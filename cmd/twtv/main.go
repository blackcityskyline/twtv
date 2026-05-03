package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/twtv/internal/config"
	"github.com/user/twtv/internal/history"
	"github.com/user/twtv/internal/player"
	"github.com/user/twtv/internal/terminal"
)

const twitchBase = "https://www.twitch.tv/"

func main() {
	var (
		chat  = flag.Bool("c", false, "open chat for the channel")
		quiet = flag.Bool("q", false, "suppress mpv log output")
		histN = flag.Int("H", 0, "pick from last N history entries (numeric list)")
	)
	flag.Usage = usage
	flag.Parse()

	cfg, err := config.Load()
	die(err)

	hist := history.New(config.ConfigDir, cfg.History.Limit)
	extra := flag.Args()

	switch {
	case *histN > 0:
		url, err := pickNumbered(hist, *histN)
		die(err)
		ch := channelFromURL(url)
		maybeChat(cfg, *chat, ch)
		die(hist.Append(ch, url))
		die(player.Launch(url, extra, *quiet))

	case flag.NArg() == 0:
		// no args — open TUI
		p := tea.NewProgram(newModel(cfg, hist, extra, *quiet))
		if _, err := p.Run(); err != nil {
			die(err)
		}

	default:
		ch := flag.Arg(0)
		extra = flag.Args()[1:]
		url := toURL(ch)
		maybeChat(cfg, *chat, ch)
		die(hist.Append(ch, url))
		die(player.Launch(url, extra, *quiet))
	}
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

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: twtv [flags] [channel] [mpv args...]

  twtv                  open TUI with live streams + history
  twtv <channel>        play channel directly
  twtv -H 10            pick from last 10 history entries

flags:
  -c    open chat in a new terminal window
  -q    suppress mpv log output
  -H N  show last N history entries (numeric picker)`)
}
