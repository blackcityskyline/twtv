package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/twtv/internal/config"
	"github.com/user/twtv/internal/history"
	"github.com/user/twtv/internal/muted"
	"github.com/user/twtv/internal/player"
	"github.com/user/twtv/internal/terminal"
)

func main() {
	histFlag := flag.String("H", "", "show history (last N entries)")
	chatFlag := flag.Bool("c", false, "open chat alongside stream")
	quietFlag := flag.Bool("q", false, "suppress mpv log output")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fatalf("config error: %v", err)
	}

	hist, err := history.New(cfg.History.File, cfg.History.Limit)
	if err != nil {
		fatalf("history error: %v", err)
	}

	cfgDir := filepath.Dir(cfg.History.File)
	ml := muted.Load(filepath.Join(cfgDir, "muted.txt"))

	extra := flag.Args()

	if *histFlag != "" {
		runHistoryPicker(cfg, hist, extra, *chatFlag, *quietFlag, *histFlag)
		return
	}

	if len(extra) > 0 {
		url := toURL(extra[0])
		ch := channelFromURL(url)
		_ = hist.Append(ch, url, "")
		if *chatFlag {
			openChat(cfg, ch)
		}
		if err := player.Launch(url, extra[1:], *quietFlag); err != nil {
			fatalf("player error: %v", err)
		}
		return
	}

	// TUI mode — always quiet so mpv output doesn't corrupt the interface.
	m := newModel(cfg, hist, ml, extra, true)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fatalf("tui error: %v", err)
	}
}

func runHistoryPicker(cfg *config.Config, hist *history.History, extra []string, chat, quiet bool, rawN string) {
	n, err := strconv.Atoi(rawN)
	if err != nil || n <= 0 {
		n = 10
	}

	entries, err := hist.Read()
	if err != nil {
		fatalf("history read error: %v", err)
	}
	if len(entries) == 0 {
		fmt.Println("no history yet")
		return
	}
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}

	for i, e := range entries {
		fmt.Printf("%2d. %s\n    %s\n", i+1, e.Channel, e.URL)
	}
	fmt.Printf("Pick a number (1–%d): ", len(entries))

	var pick int
	if _, err := fmt.Scanf("%d", &pick); err != nil || pick < 1 || pick > len(entries) {
		fmt.Println("cancelled")
		return
	}

	e := entries[pick-1]
	_ = hist.Append(e.Channel, e.URL, e.Game)
	if chat {
		openChat(cfg, e.Channel)
	}
	if err := player.Launch(e.URL, extra, quiet); err != nil {
		fatalf("player error: %v", err)
	}
}

// toURL normalises a channel name or Twitch URL to a canonical https URL.
func toURL(channel string) string {
	ch := channel
	for _, prefix := range []string{
		"https://www.twitch.tv/",
		"https://twitch.tv/",
		"www.twitch.tv/",
		"twitch.tv/",
	} {
		ch = strings.TrimPrefix(ch, prefix)
	}
	return "https://www.twitch.tv/" + ch
}

// channelFromURL extracts the login name from a canonical Twitch URL.
func channelFromURL(u string) string {
	u = strings.TrimPrefix(u, "https://www.twitch.tv/")
	return strings.TrimPrefix(u, "https://twitch.tv/")
}

// openChat opens a Twitch chat window for channel in the configured terminal.
func openChat(cfg *config.Config, channel string) {
	term := cfg.Chat.Terminal
	if term == "" {
		term = terminal.Detect()
	}
	if term == "" {
		return
	}
	cmd := cfg.Chat.Command
	if strings.Contains(cmd, "<channel>") {
		cmd = strings.ReplaceAll(cmd, "<channel>", channel)
	} else {
		cmd += " " + channel
	}
	_ = terminal.Launch(term, strings.Fields(cmd)...)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
