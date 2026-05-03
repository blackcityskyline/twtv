package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/user/twtv/internal/config"
	"github.com/user/twtv/internal/history"
	"github.com/user/twtv/internal/player"
	"github.com/user/twtv/internal/twitch"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	styleLive     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleOffline  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSelected = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleviewers  = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	styleInput    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	styleDivider  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleKey      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// ── messages ──────────────────────────────────────────────────────────────────

type entriesLoadedMsg struct {
	entries []tuiEntry
	err     error
}

type launchedMsg struct{}

// ── types ─────────────────────────────────────────────────────────────────────

type tuiEntry struct {
	channel string
	url     string
	live    bool
	viewers int
	game    string
	title   string
}

type mode int

const (
	modeList mode = iota
	modeFilter
	modeChat
)

// ── model ─────────────────────────────────────────────────────────────────────

type model struct {
	cfg   *config.Config
	hist  *history.History
	extra []string
	quiet bool

	entries  []tuiEntry
	filtered []tuiEntry
	cursor   int

	mode    mode
	filter  string
	loading bool
	err     string
	status  string
}

func newModel(cfg *config.Config, hist *history.History, extra []string, quiet bool) model {
	return model{
		cfg:     cfg,
		hist:    hist,
		extra:   extra,
		quiet:   quiet,
		loading: true,
	}
}

func (m model) Init() tea.Cmd {
	return m.fetchEntries()
}

// ── update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case entriesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.entries = msg.entries
		m.applyFilter()
		return m, nil

	case launchedMsg:
		return m, tea.Quit

	case tea.KeyMsg:
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modeChat:
			return m.updateChat(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.filtered) == 0 {
			break
		}
		return m, m.launch(m.filtered[m.cursor])
	case "c":
		if len(m.filtered) > 0 {
			m.mode = modeChat
		}
	case "/":
		m.mode = modeFilter
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
			m.cursor = 0
		}
	}
	return m, nil
}

func (m model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.mode = modeList
		m.cursor = 0
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.applyFilter()
		}
	case "ctrl+c":
		return m, tea.Quit
	default:
		if len(msg.Runes) > 0 {
			m.filter += string(msg.Runes)
			m.applyFilter()
		}
	}
	return m, nil
}

func (m model) updateChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.filtered[m.cursor]
	ch := channelFromURL(e.url)
	switch msg.String() {
	case "y", "Y":
		m.mode = modeList
		launchChat(m.cfg, ch)
		m.status = fmt.Sprintf("chat opened for %s", ch)
	case "n", "N", "esc", "enter":
		m.mode = modeList
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) applyFilter() {
	if m.filter == "" {
		m.filtered = m.entries
		return
	}
	f := strings.ToLower(m.filter)
	filtered := m.filtered[:0]
	for _, e := range m.entries {
		if strings.Contains(strings.ToLower(e.channel), f) ||
			strings.Contains(strings.ToLower(e.game), f) ||
			strings.Contains(strings.ToLower(e.title), f) {
			filtered = append(filtered, e)
		}
	}
	m.filtered = filtered
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.loading {
		return "\n  fetching streams…\n"
	}
	if m.err != "" {
		return styleErr.Render("\n  error: "+m.err) + "\n\n  " +
			styleDim.Render("press q to quit") + "\n"
	}

	var b strings.Builder

	// filter bar
	switch m.mode {
	case modeFilter:
		b.WriteString(styleInput.Render("  / "+m.filter+"█") + "\n")
	default:
		if m.filter != "" {
			b.WriteString(styleDim.Render("  / "+m.filter+"  [esc clear]") + "\n")
		}
	}

	// list
	prevLive := true
	for i, e := range m.filtered {
		// separator between live and offline sections
		if i > 0 && prevLive && !e.live {
			b.WriteString(styleDivider.Render("  "+strings.Repeat("─", 54)) + "\n")
		}
		prevLive = e.live

		row := renderRow(e)
		if i == m.cursor {
			b.WriteString(styleSelected.Render("▸ "+row) + "\n")
		} else {
			b.WriteString("  " + row + "\n")
		}
	}

	if len(m.filtered) == 0 {
		b.WriteString(styleDim.Render("  no results") + "\n")
	}

	b.WriteString("\n")

	// bottom bar
	if m.mode == modeChat && len(m.filtered) > 0 {
		ch := channelFromURL(m.filtered[m.cursor].url)
		b.WriteString(styleInput.Render(fmt.Sprintf("  open chat for %s? [y/n] ", ch)) + "\n")
	} else {
		b.WriteString(renderKeys() + "\n")
	}

	if m.status != "" {
		b.WriteString(styleDim.Render("  "+m.status) + "\n")
	}

	return b.String()
}

func renderRow(e tuiEntry) string {
	if e.live {
		icon := styleLive.Render("●")
		ch := styleLive.Render(fmt.Sprintf("%-22s", e.channel))
		viewers := styleViewers(e.viewers)
		meta := styleDim.Render(truncate(e.game+" — "+e.title, 44))
		return fmt.Sprintf("%s %s %s  %s", icon, ch, viewers, meta)
	}
	icon := styleOffline.Render("○")
	ch := styleOffline.Render(fmt.Sprintf("%-22s", e.channel))
	return fmt.Sprintf("%s %s  %s", icon, ch, styleDim.Render("offline"))
}

func styleViewers(n int) string {
	return styleviewers.Render(fmt.Sprintf("%6d", n))
}


func renderKeys() string {
	type kv struct{ k, v string }
	keys := []kv{
		{"↑↓ jk", "move"},
		{"enter", "play"},
		{"c", "chat"},
		{"/", "filter"},
		{"esc", "clear"},
		{"q", "quit"},
	}
	parts := make([]string, len(keys))
	for i, kv := range keys {
		parts[i] = styleKey.Render(kv.k) + styleDim.Render(" "+kv.v)
	}
	return "  " + strings.Join(parts, styleDim.Render("  ·  "))
}

// ── commands ──────────────────────────────────────────────────────────────────

func (m model) fetchEntries() tea.Cmd {
	return func() tea.Msg {
		entries, err := loadEntries(m.cfg, m.hist)
		return entriesLoadedMsg{entries: entries, err: err}
	}
}

func (m model) launch(e tuiEntry) tea.Cmd {
	return func() tea.Msg {
		_ = m.hist.Append(e.channel, e.url)
		_ = launchMpv(e.url, m.extra, m.quiet)
		return launchedMsg{}
	}
}

func launchMpv(url string, extra []string, quiet bool) error {
	return player.Launch(url, extra, quiet)
}

// ── data ──────────────────────────────────────────────────────────────────────

func loadEntries(cfg *config.Config, hist *history.History) ([]tuiEntry, error) {
	liveMap := map[string]twitch.Stream{}

	if cfg.Auth.ClientID != "" && cfg.Auth.AccessToken != "" {
		tc := twitch.New(cfg.Auth.ClientID, cfg.Auth.AccessToken)
		if userID, err := tc.UserID(); err == nil {
			if follows, err := tc.Follows(userID); err == nil {
				logins := make([]string, len(follows))
				for i, f := range follows {
					logins[i] = f.BroadcasterLogin
				}
				if streams, err := tc.LiveStreams(logins); err == nil {
					for _, s := range streams {
						liveMap[strings.ToLower(s.UserLogin)] = s
					}
				}
			}
		}
	}

	// live first, sorted by viewers
	entries := make([]tuiEntry, 0, len(liveMap))
	for _, s := range liveMap {
		entries = append(entries, tuiEntry{
			channel: s.UserLogin,
			url:     toURL(s.UserLogin),
			live:    true,
			viewers: s.ViewerCount,
			game:    s.GameName,
			title:   s.Title,
		})
	}
	sortByViewers(entries)

	// offline history, deduped
	seen := map[string]bool{}
	for _, e := range entries {
		seen[strings.ToLower(e.channel)] = true
	}

	if cfg.Fzf.ShowOffline {
		histEntries, _ := hist.Read()
		for i := len(histEntries) - 1; i >= 0; i-- {
			e := histEntries[i]
			ch := strings.ToLower(e.Channel)
			if seen[ch] {
				continue
			}
			seen[ch] = true
			entries = append(entries, tuiEntry{
				channel: e.Channel,
				url:     e.URL,
				live:    false,
			})
		}
	}

	return entries, nil
}

func sortByViewers(e []tuiEntry) {
	for i := 1; i < len(e); i++ {
		for j := i; j > 0 && e[j].viewers > e[j-1].viewers; j-- {
			e[j], e[j-1] = e[j-1], e[j]
		}
	}
}
