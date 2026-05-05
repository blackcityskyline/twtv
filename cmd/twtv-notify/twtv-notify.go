package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
)

const (
	configFileName  = "config.json"
	apiUsers        = "https://api.twitch.tv/helix/users"
	apiStreamsFmt   = "https://api.twitch.tv/helix/streams/followed?user_id=%s&first=100"
	defaultInterval = 120
	maxResponse     = 16384 // max API response size to read
)

// ── config and API response types ─────────────────────────────────────
type Config struct {
	Auth   AuthConfig   `json:"auth"`
	Notify NotifyConfig `json:"notify"`
}

type AuthConfig struct {
	ClientID    string `json:"client_id"`
	AccessToken string `json:"access_token"`
}

type NotifyConfig struct {
	Command     string `json:"command"`
	IntervalSec int    `json:"interval_sec"`
}

type usersResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type streamsResponse struct {
	Data []struct {
		UserLogin string `json:"user_login"`
	} `json:"data"`
}

// ── helpers ───────────────────────────────────────────────────────────
func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot find home directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "twtv", configFileName), nil
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if cfg.Notify.Command == "" {
		cfg.Notify.Command = "notify-send"
	}
	if cfg.Notify.IntervalSec <= 0 {
		cfg.Notify.IntervalSec = defaultInterval
	}
	cfg.Auth.AccessToken = strings.TrimPrefix(cfg.Auth.AccessToken, "oauth:")
	if cfg.Auth.ClientID == "" || cfg.Auth.AccessToken == "" {
		return nil, fmt.Errorf("config missing auth.client_id or auth.access_token")
	}
	return &cfg, nil
}

// doRequest performs an authenticated GET request with Twitch headers.
// Reads up to maxResponse bytes to avoid uncontrolled allocations.
func doRequest(ctx context.Context, client *http.Client, url, clientID, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "twtv-notify/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	// Use a pre-sized buffer to limit memory growth
	var buf bytes.Buffer
	buf.Grow(maxResponse)
	limited := io.LimitReader(resp.Body, int64(maxResponse))
	if _, err := buf.ReadFrom(limited); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return buf.Bytes(), nil
}

// getUserId fetches the authenticated user's Twitch ID.
func getUserId(ctx context.Context, client *http.Client, cfg *Config) (string, error) {
	body, err := doRequest(ctx, client, apiUsers, cfg.Auth.ClientID, cfg.Auth.AccessToken)
	if err != nil {
		return "", err
	}
	var resp usersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse users: %w", err)
	}
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("empty user data, check credentials")
	}
	return resp.Data[0].ID, nil
}

// pollLive fills the provided buffer with currently live user logins.
func pollLive(ctx context.Context, client *http.Client, cfg *Config, userID string, buf *[]string) error {
	url := fmt.Sprintf(apiStreamsFmt, userID)
	body, err := doRequest(ctx, client, url, cfg.Auth.ClientID, cfg.Auth.AccessToken)
	if err != nil {
		return err
	}
	var resp streamsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse streams: %w", err)
	}
	*buf = (*buf)[:0]
	for _, s := range resp.Data {
		if s.UserLogin != "" {
			*buf = append(*buf, s.UserLogin)
		}
	}
	return nil
}

// notify runs the configured notification command asynchronously.
func notify(cmd, channel string) {
	go func() {
		c := exec.Command("sh", "-c", fmt.Sprintf(`%s "twtv" "%s is live"`, cmd, channel))
		c.Stdout = nil
		c.Stderr = nil
		if err := c.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "notify error: %v\n", err)
		}
	}()
}

// diff returns channels in current that are not in prev.
func diff(prev map[string]bool, current []string) []string {
	var d []string
	for _, name := range current {
		if !prev[strings.ToLower(name)] {
			d = append(d, name)
		}
	}
	return d
}

func main() {
	// ── memory tuning ─────────────────────────────────────────────────
	debug.SetMemoryLimit(3 * 1024 * 1024) // soft heap limit 3 MiB
	debug.SetGCPercent(1)                 // aggressive GC

	// ── graceful shutdown ─────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── load configuration ────────────────────────────────────────────
	cfgPath, err := configPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "twtv-notify: %v\n", err)
		os.Exit(1)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "twtv-notify: config error: %v\n", err)
		os.Exit(1)
	}

	// ── minimal HTTP client ──────────────────────────────────────────
	transport := &http.Transport{
		MaxIdleConns:      0,
		IdleConnTimeout:   0,
		DisableKeepAlives: true,
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		ForceAttemptHTTP2: false,
	}
	httpClient := &http.Client{Transport: transport, Timeout: 10 * time.Second}

	// ── obtain user ID ────────────────────────────────────────────────
	userID, err := getUserId(ctx, httpClient, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "twtv-notify: failed to get user ID: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "twtv-notify: user_id=%s  interval=%ds\n", userID, cfg.Notify.IntervalSec)

	// ── reusable buffers to avoid per-cycle allocations ───────────────
	liveBuf := make([]string, 0, 32)
	prevSet := make(map[string]bool, 32)

	// Initial poll builds baseline without notifications
	if err := pollLive(ctx, httpClient, cfg, userID, &liveBuf); err != nil {
		fmt.Fprintf(os.Stderr, "twtv-notify: initial poll error: %v\n", err)
	} else {
		for _, ch := range liveBuf {
			prevSet[strings.ToLower(ch)] = true
		}
	}
	firstPoll := true

	ticker := time.NewTicker(time.Duration(cfg.Notify.IntervalSec) * time.Second)
	defer ticker.Stop()

	// ── main loop ─────────────────────────────────────────────────────
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "twtv-notify: shutting down\n")
			return
		case <-ticker.C:
			if err := pollLive(ctx, httpClient, cfg, userID, &liveBuf); err != nil {
				fmt.Fprintf(os.Stderr, "twtv-notify: poll error: %v\n", err)
				continue
			}

			if !firstPoll {
				newCh := diff(prevSet, liveBuf)
				for _, ch := range newCh {
					fmt.Fprintf(os.Stderr, "twtv-notify: %s is live\n", ch)
					notify(cfg.Notify.Command, ch)
				}
			}
			firstPoll = false

			// Update the set of previously live channels
			for k := range prevSet {
				delete(prevSet, k)
			}
			for _, ch := range liveBuf {
				prevSet[strings.ToLower(ch)] = true
			}

			// Explicit GC to return memory to the OS promptly
			runtime.GC()
		}
	}
}
