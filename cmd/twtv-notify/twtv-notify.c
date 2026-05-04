/*
 * twtv-notify — stream-start notification daemon for twtv.
 *
 * Polls /streams/followed every <interval_sec> seconds (default 120).
 * On the first poll it silently builds a baseline so that restarting
 * the daemon does not spam notifications for already-live streams.
 *
 * Dependencies: libcurl, libjansson
 *   Arch:   pacman -S curl jansson
 *   Debian: apt install libcurl4-openssl-dev libjansson-dev
 *
 * Build:
 *   gcc -O2 -o twtv-notify twtv-notify.c -lcurl -ljansson
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <signal.h>
#include <time.h>

#include <curl/curl.h>
#include <jansson.h>

/* ── constants ──────────────────────────────────────────────────────────────── */

#define CONFIG_FILENAME   "config.json"
#define API_USER          "https://api.twitch.tv/helix/users"
#define API_STREAMS_FMT   "https://api.twitch.tv/helix/streams/followed?user_id=%s&first=100"
#define MAX_CHANNELS      512
#define CHANNEL_LEN       64
#define DEFAULT_INTERVAL  120
#define UA                "twtv-notify/1.0"

/* ── types ──────────────────────────────────────────────────────────────────── */

typedef struct {
    char   client_id[128];
    char   access_token[256];
    char   notify_cmd[128];   /* e.g. "notify-send" */
    int    interval_sec;
} Config;

/* Growable buffer for curl response body. */
typedef struct {
    char  *data;
    size_t len;
} Buf;

/* Set of currently-live channel logins. */
typedef struct {
    char names[MAX_CHANNELS][CHANNEL_LEN];
    int  count;
} LiveSet;

/* ── globals ─────────────────────────────────────────────────────────────────── */

static volatile sig_atomic_t g_stop = 0;

static void on_signal(int sig) { (void)sig; g_stop = 1; }

/* ── curl helpers ────────────────────────────────────────────────────────────── */

static size_t write_cb(void *ptr, size_t size, size_t nmemb, void *userdata) {
    size_t bytes = size * nmemb;
    Buf *b = userdata;
    char *tmp = realloc(b->data, b->len + bytes + 1);
    if (!tmp) return 0;
    b->data = tmp;
    memcpy(b->data + b->len, ptr, bytes);
    b->len += bytes;
    b->data[b->len] = '\0';
    return bytes;
}

/*
 * fetch_json performs an authenticated GET and returns a jansson object.
 * Caller must json_decref() the result. Returns NULL on error.
 */
static json_t *fetch_json(CURL *curl, const Config *cfg, const char *url) {
    Buf buf = {NULL, 0};

    char auth[320];
    snprintf(auth, sizeof(auth), "Bearer %s", cfg->access_token);

    struct curl_slist *hdrs = NULL;
    char cid_hdr[160];
    snprintf(cid_hdr, sizeof(cid_hdr), "Client-Id: %s", cfg->client_id);
    hdrs = curl_slist_append(hdrs, cid_hdr);
    hdrs = curl_slist_append(hdrs, "Accept: application/json");

    curl_easy_setopt(curl, CURLOPT_URL,            url);
    curl_easy_setopt(curl, CURLOPT_HTTPHEADER,     hdrs);
    curl_easy_setopt(curl, CURLOPT_XOAUTH2_BEARER, auth);
    curl_easy_setopt(curl, CURLOPT_HTTPAUTH,       CURLAUTH_BEARER);
    curl_easy_setopt(curl, CURLOPT_USERAGENT,      UA);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION,  write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA,      &buf);
    curl_easy_setopt(curl, CURLOPT_TIMEOUT,        10L);

    CURLcode res = curl_easy_perform(curl);
    curl_slist_free_all(hdrs);
    curl_easy_reset(curl);

    if (res != CURLE_OK) {
        fprintf(stderr, "curl: %s\n", curl_easy_strerror(res));
        free(buf.data);
        return NULL;
    }

    json_error_t err;
    json_t *root = json_loads(buf.data ? buf.data : "", 0, &err);
    free(buf.data);
    if (!root)
        fprintf(stderr, "json parse error: %s\n", err.text);
    return root;
}

/* ── config ──────────────────────────────────────────────────────────────────── */

/*
 * config_path writes the path to config.json into out (size outlen).
 * Uses $XDG_CONFIG_HOME/twtv or $HOME/.config/twtv as fallback.
 */
static void config_path(char *out, size_t outlen) {
    const char *base = getenv("XDG_CONFIG_HOME");
    if (base && base[0]) {
        snprintf(out, outlen, "%s/twtv/%s", base, CONFIG_FILENAME);
    } else {
        const char *home = getenv("HOME");
        snprintf(out, outlen, "%s/.config/twtv/%s", home ? home : "", CONFIG_FILENAME);
    }
}

static int load_config(Config *cfg) {
    char path[512];
    config_path(path, sizeof(path));

    json_error_t err;
    json_t *root = json_load_file(path, 0, &err);
    if (!root) {
        fprintf(stderr, "config: %s: %s\n", path, err.text);
        return -1;
    }

    /* auth.client_id */
    json_t *auth = json_object_get(root, "auth");
    if (auth) {
        const char *v;
        if ((v = json_string_value(json_object_get(auth, "client_id"))))
            snprintf(cfg->client_id, sizeof(cfg->client_id), "%s", v);
        if ((v = json_string_value(json_object_get(auth, "access_token")))) {
            /* strip leading "oauth:" if present */
            if (strncmp(v, "oauth:", 6) == 0) v += 6;
            snprintf(cfg->access_token, sizeof(cfg->access_token), "%s", v);
        }
    }

    /* notify.command and notify.interval_sec */
    json_t *notify = json_object_get(root, "notify");
    if (notify) {
        const char *cmd = json_string_value(json_object_get(notify, "command"));
        if (cmd) snprintf(cfg->notify_cmd, sizeof(cfg->notify_cmd), "%s", cmd);

        json_t *iv = json_object_get(notify, "interval_sec");
        if (json_is_integer(iv))
            cfg->interval_sec = (int)json_integer_value(iv);
    }

    json_decref(root);
    return 0;
}

/* ── Twitch helpers ──────────────────────────────────────────────────────────── */

static int get_user_id(CURL *curl, const Config *cfg, char *out, size_t outlen) {
    json_t *root = fetch_json(curl, cfg, API_USER);
    if (!root) return -1;

    json_t *data = json_object_get(root, "data");
    int ok = -1;
    if (json_is_array(data) && json_array_size(data) > 0) {
        json_t *user = json_array_get(data, 0);
        const char *id = json_string_value(json_object_get(user, "id"));
        if (id) { snprintf(out, outlen, "%s", id); ok = 0; }
    }
    json_decref(root);
    return ok;
}

/*
 * poll_live fills `set` with currently-live followed channels.
 * Returns 0 on success, -1 on error.
 */
static int poll_live(CURL *curl, const Config *cfg, const char *user_id, LiveSet *set) {
    char url[512];
    snprintf(url, sizeof(url), API_STREAMS_FMT, user_id);

    json_t *root = fetch_json(curl, cfg, url);
    if (!root) return -1;

    json_t *data = json_object_get(root, "data");
    set->count = 0;

    if (json_is_array(data)) {
        size_t i;
        json_t *stream;
        json_array_foreach(data, i, stream) {
            if (set->count >= MAX_CHANNELS) break;
            const char *login = json_string_value(json_object_get(stream, "user_login"));
            if (login)
                snprintf(set->names[set->count++], CHANNEL_LEN, "%s", login);
        }
    }
    json_decref(root);
    return 0;
}

/* ── live-set helpers ────────────────────────────────────────────────────────── */

static int set_has(const LiveSet *s, const char *name) {
    for (int i = 0; i < s->count; i++)
        if (strcasecmp(s->names[i], name) == 0) return 1;
    return 0;
}

/* ── notification ────────────────────────────────────────────────────────────── */

static void notify(const Config *cfg, const char *channel) {
    char cmd[512];
    /* notify-send accepts: notify-send "title" "body" */
    snprintf(cmd, sizeof(cmd), "%s \"twtv\" \"%s is live\" &", cfg->notify_cmd, channel);
    system(cmd); /* fire-and-forget */
}

/* ── main loop ───────────────────────────────────────────────────────────────── */

int main(void) {
    signal(SIGTERM, on_signal);
    signal(SIGINT,  on_signal);

    /* Default config values. */
    Config cfg = {
        .notify_cmd   = "notify-send",
        .interval_sec = DEFAULT_INTERVAL,
    };

    if (load_config(&cfg) != 0)
        return 1;

    if (!cfg.client_id[0] || !cfg.access_token[0]) {
        fprintf(stderr, "twtv-notify: auth.client_id and auth.access_token must be set in config.json\n");
        return 1;
    }

    curl_global_init(CURL_GLOBAL_DEFAULT);
    CURL *curl = curl_easy_init();
    if (!curl) { fprintf(stderr, "curl init failed\n"); return 1; }

    char user_id[64] = {0};
    if (get_user_id(curl, &cfg, user_id, sizeof(user_id)) != 0) {
        fprintf(stderr, "twtv-notify: failed to resolve user ID — check credentials\n");
        curl_easy_cleanup(curl);
        curl_global_cleanup();
        return 1;
    }
    fprintf(stderr, "twtv-notify: user_id=%s  interval=%ds\n", user_id, cfg.interval_sec);

    LiveSet prev = {.count = 0};
    LiveSet curr = {.count = 0};
    int first = 1; /* suppress notifications on first poll */

    while (!g_stop) {
        if (poll_live(curl, &cfg, user_id, &curr) == 0) {
            if (!first) {
                /* Notify for channels that just went live (in curr but not prev). */
                for (int i = 0; i < curr.count; i++) {
                    if (!set_has(&prev, curr.names[i])) {
                        fprintf(stderr, "twtv-notify: %s is live\n", curr.names[i]);
                        notify(&cfg, curr.names[i]);
                    }
                }
            }
            first = 0;
            prev = curr;
        }

        /* Sleep in 1s increments so SIGTERM is handled promptly. */
        for (int i = 0; i < cfg.interval_sec && !g_stop; i++)
            sleep(1);
    }

    fprintf(stderr, "twtv-notify: shutting down\n");
    curl_easy_cleanup(curl);
    curl_global_cleanup();
    return 0;
}
