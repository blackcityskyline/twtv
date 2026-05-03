package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/user/twtv/internal/config"
	"github.com/user/twtv/internal/history"
	"github.com/user/twtv/internal/player"
	"github.com/user/twtv/internal/terminal"
	"github.com/user/twtv/internal/twitch"
)

const twitchBase = "https://www.twitch.tv/"

func main() {
	var (
		chat    = flag.Bool("c", false, "open chat for the channel")
		quiet   = flag.Bool("q", false, "suppress mpv log output")
		histN   = flag.Int("H", 0, "pick from last N history entries (numeric list)")
	)
	flag.Usage = usage
	flag.Parse()

	cfg, err := config.Load()
	die(err)

	hist := history.New(config.ConfigDir, cfg.History.Limit)

	extra := flag.Args() // remaining args passed straight to mpv

	switch {
	case *histN > 0:
		// numbered history picker
		url, err := pickNumbered(hist, *histN)
		die(err)
		maybeChat(cfg, *chat, channelFromURL(url))
		die(hist.Append(channelFromURL(url), url))
		die(player.Launch(url, extra, *quiet))

	case flag.NArg() == 0:
		// no args — fzf with live data
		url, err := pickFzf(cfg, hist)
		die(err)
		ch := channelFromURL(url)
		if askYN(fmt.Sprintf("Open chat for %s?", ch)) {
			launchChat(cfg, ch)
		}
		die(hist.Append(ch, url))
		die(player.Launch(url, extra, *quiet))

	default:
		// direct: twtv <channel> [mpv args...]
		ch := flag.Arg(0)
		extra = flag.Args()[1:]
		url := toURL(ch)
		maybeChat(cfg, *chat, ch)
		die(hist.Append(ch, url))
		die(player.Launch(url, extra, *quiet))
	}
}

// ── fzf picker ───────────────────────────────────────────────────────────────

type fzfEntry struct {
	channel  string
	url      string
	live     bool
	viewers  int
	game     string
	title    string
}

func pickFzf(cfg *config.Config, hist *history.History) (string, error) {
	entries, err := buildFzfEntries(cfg, hist)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("nothing to show — watch something first or check your auth config")
	}

	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = formatFzfLine(e)
	}

	fzfArgs := []string{"--ansi", "--prompt=channel> ", "--reverse", "--no-sort"}
	if cfg.Fzf.ExtraArgs != "" {
		fzfArgs = append(fzfArgs, strings.Fields(cfg.Fzf.ExtraArgs)...)
	}

	cmd := exec.Command("fzf", fzfArgs...)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("cancelled")
	}

	selected := strings.TrimSpace(string(out))
	// channel name is the first word after the status icon
	parts := strings.Fields(selected)
	if len(parts) < 2 {
		return "", fmt.Errorf("could not parse selection")
	}
	ch := parts[1] // parts[0] is the ● or ○ icon

	// find url from entries
	for _, e := range entries {
		if e.channel == ch {
			return e.url, nil
		}
	}
	return toURL(ch), nil
}

func buildFzfEntries(cfg *config.Config, hist *history.History) ([]fzfEntry, error) {
	entries := []fzfEntry{}
	liveMap := map[string]twitch.Stream{}

	// fetch live streams if auth is configured
	if cfg.Auth.ClientID != "" && cfg.Auth.AccessToken != "" {
		tc := twitch.New(cfg.Auth.ClientID, cfg.Auth.AccessToken)
		userID, err := tc.UserID()
		if err == nil {
			follows, err := tc.Follows(userID)
			if err == nil {
				logins := make([]string, len(follows))
				for i, f := range follows {
					logins[i] = f.BroadcasterLogin
				}
				streams, err := tc.LiveStreams(logins)
				if err == nil {
					for _, s := range streams {
						liveMap[strings.ToLower(s.UserLogin)] = s
					}
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "twitch auth error: %v\n", err)
		}
	}

	// live streams first (sorted by viewers)
	live := make([]fzfEntry, 0, len(liveMap))
	for _, s := range liveMap {
		live = append(live, fzfEntry{
			channel: s.UserLogin,
			url:     toURL(s.UserLogin),
			live:    true,
			viewers: s.ViewerCount,
			game:    s.GameName,
			title:   s.Title,
		})
	}
	sort.Slice(live, func(i, j int) bool {
		return live[i].viewers > live[j].viewers
	})
	entries = append(entries, live...)

	// history entries that aren't already shown as live
	histEntries, err := hist.Read()
	if err != nil {
		return entries, nil
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.channel] = true
	}
	// walk history in reverse (most recent first), deduplicate
	for i := len(histEntries) - 1; i >= 0; i-- {
		e := histEntries[i]
		ch := strings.ToLower(e.Channel)
		if seen[ch] {
			continue
		}
		if !cfg.Fzf.ShowOffline {
			continue
		}
		seen[ch] = true
		entries = append(entries, fzfEntry{
			channel: e.Channel,
			url:     e.URL,
			live:    false,
		})
	}

	return entries, nil
}

func formatFzfLine(e fzfEntry) string {
	if e.live {
		return fmt.Sprintf("\033[32m●\033[0m %-20s  %6d viewers  %s — %s",
			e.channel, e.viewers, e.game, truncate(e.title, 50))
	}
	return fmt.Sprintf("\033[90m○\033[0m %-20s  offline", e.channel)
}

// ── numeric history picker ────────────────────────────────────────────────────

func pickNumbered(hist *history.History, n int) (string, error) {
	entries, err := hist.Last(n)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("history is empty")
	}
	for i, e := range entries {
		fmt.Printf("%2d. %-30s  %s\n", i+1, e.Channel, e.Timestamp)
	}
	var choice int
	fmt.Printf("\nPick a number (1–%d): ", len(entries))
	if _, err := fmt.Scan(&choice); err != nil || choice < 1 || choice > len(entries) {
		return "", fmt.Errorf("cancelled")
	}
	return entries[choice-1].URL, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toURL(ch string) string {
	if strings.HasPrefix(ch, "http://") || strings.HasPrefix(ch, "https://") {
		return ch
	}
	return twitchBase + strings.TrimPrefix(ch, "/")
}

func channelFromURL(url string) string {
	return strings.TrimPrefix(strings.TrimRight(url, "/"), twitchBase)
}

func maybeChat(cfg *config.Config, open bool, ch string) {
	if open {
		launchChat(cfg, ch)
	}
}

func launchChat(cfg *config.Config, ch string) {
	term := cfg.Chat.Terminal
	if term == "" {
		var err error
		term, err = terminal.Find()
		if err != nil {
			fmt.Fprintf(os.Stderr, "chat: %v\n", err)
			return
		}
	}
	// split chat command (e.g. "twt -c" → ["twt", "-c"])
	parts := strings.Fields(cfg.Chat.Command)
	if len(parts) == 0 {
		parts = []string{"twt", "-c"}
	}
	chatBin := parts[0]
	chatArgs := append(parts[1:], ch)
	if err := player.LaunchChat(term, chatBin, strings.Join(chatArgs, " ")); err != nil {
		fmt.Fprintf(os.Stderr, "chat: %v\n", err)
	}
}

func askYN(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	var ans string
	fmt.Scan(&ans)
	return strings.ToLower(ans) == "y" || strings.ToLower(ans) == "yes"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: twtv [flags] [channel] [mpv args...]

  twtv                  open fzf with live followed streams + history
  twtv <channel>        play channel directly
  twtv -H 10            pick from last 10 history entries

flags:
  -c    open chat in a new terminal window
  -q    suppress mpv log output
  -H N  show last N history entries (numeric picker)

extra mpv args go after the channel name.`)
}
