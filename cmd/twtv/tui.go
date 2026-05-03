package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/user/twtv/internal/blocklist"
	"github.com/user/twtv/internal/config"
	"github.com/user/twtv/internal/history"
	"github.com/user/twtv/internal/player"
	"github.com/user/twtv/internal/twitch"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	styleLive      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleOffline   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSelected  = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleViewers   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	styleInput     = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	styleDivider   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleKey       = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleErr       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleTab       = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Padding(0, 1)
	styleTabActive = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true).Background(lipgloss.Color("236")).Padding(0, 1)
	styleHeader    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleDislike   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleGame      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// ── tabs ──────────────────────────────────────────────────────────────────────

type tab int

const (
	tabFollowed tab = iota
	tabCategories
	tabForYou
	tabTop
	tabCount // sentinel — always last
)

var tabNames = [tabCount]string{"followed", "categories", "for you", "top streams"}

// ── messages ──────────────────────────────────────────────────────────────────

type followedLoadedMsg struct {
	entries []tuiEntry
	err     error
}
type topLoadedMsg struct {
	entries []tuiEntry
	err     error
}
type forYouLoadedMsg struct {
	entries []tuiEntry
	err     error
}
type categoriesLoadedMsg struct {
	entries []tuiEntry
	err     error
}
type gameStreamsLoadedMsg struct {
	entries []tuiEntry
	err     error
}

// ── data types ────────────────────────────────────────────────────────────────

type tuiEntry struct {
	channel    string
	url        string
	game       string
	title      string
	viewers    int
	live       bool
	isCategory bool
}

type uiMode int

const (
	modeList uiMode = iota
	modeFilter
	modeChat
	modeDislike
)

// ── model ─────────────────────────────────────────────────────────────────────

type model struct {
	cfg   *config.Config
	hist  *history.History
	bl    *blocklist.Blocklist
	extra []string
	quiet bool

	tabs    [tabCount][]tuiEntry
	loaded  [tabCount]bool
	loading [tabCount]bool

	activeTab tab
	filtered  []tuiEntry
	cursor    int

	mode   uiMode
	filter string
	err    string
	status string
	width  int
	height int
	ready  bool

	// categoryID/Name track navigation into a category's streams.
	categoryID   string
	categoryName string
}

func newModel(cfg *config.Config, hist *history.History, bl *blocklist.Blocklist, extra []string, quiet bool) model {
	m := model{cfg: cfg, hist: hist, bl: bl, extra: extra, quiet: quiet}
	m.loading[tabFollowed] = true
	return m
}

// ── bubbletea interface ───────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return m.loadTab(tabFollowed)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true

	case followedLoadedMsg:
		return m.handleLoad(tabFollowed, msg.entries, msg.err), nil

	case topLoadedMsg:
		return m.handleLoad(tabTop, msg.entries, msg.err), nil

	case forYouLoadedMsg:
		return m.handleLoad(tabForYou, msg.entries, msg.err), nil

	case categoriesLoadedMsg:
		m = m.handleLoad(tabCategories, msg.entries, msg.err)
		m.categoryID, m.categoryName = "", "" // reset drill-down state
		return m, nil

	case gameStreamsLoadedMsg:
		return m.handleLoad(tabCategories, msg.entries, msg.err), nil

	case tea.KeyMsg:
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modeChat:
			return m.updateChat(msg)
		case modeDislike:
			return m.updateDislike(msg)
		default:
			return m.updateList(msg)
		}
	}

	return m, nil
}

// handleLoad is the common path for all tab-load messages.
func (m model) handleLoad(t tab, entries []tuiEntry, err error) model {
	m.loading[t] = false
	m.loaded[t] = true
	if err != nil {
		m.err = err.Error()
	} else {
		m.tabs[t] = entries
	}
	if m.activeTab == t {
		m.filtered = m.applyFilter()
	}
	return m
}

// ── key handlers ──────────────────────────────────────────────────────────────

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

	case "1":
		return m.switchTab(tabFollowed)
	case "2":
		return m.switchTab(tabCategories)
	case "3":
		return m.switchTab(tabForYou)
	case "4":
		return m.switchTab(tabTop)
	case "tab":
		return m.switchTab((m.activeTab + 1) % tabCount)

	case "enter":
		if len(m.filtered) == 0 {
			break
		}
		e := m.filtered[m.cursor]
		if e.isCategory {
			return m.drillIntoCategory(e)
		}
		return m, m.launch(e)

	case "c":
		if len(m.filtered) > 0 {
			m.mode = modeChat
		}
	case "d":
		if m.activeTab == tabForYou && len(m.filtered) > 0 {
			m.mode = modeDislike
		}
	case "/":
		m.mode = modeFilter

	case "esc", "backspace":
		// Navigate back from category drill-down.
		if m.activeTab == tabCategories && m.categoryID != "" {
			return m.backToCategories()
		}
		// Clear active filter.
		if m.filter != "" {
			m.filter = ""
			m.filtered = m.applyFilter()
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
			m.filtered = m.applyFilter()
		}
	case "ctrl+c":
		return m, tea.Quit
	default:
		if len(msg.Runes) > 0 {
			m.filter += string(msg.Runes)
			m.filtered = m.applyFilter()
		}
	}
	return m, nil
}

func (m model) updateChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ch := channelFromURL(m.filtered[m.cursor].url)
	switch msg.String() {
	case "y", "Y":
		m.mode = modeList
		openChat(m.cfg, ch)
		m.status = fmt.Sprintf("chat opened for %s", ch)
	case "n", "N", "esc", "enter":
		m.mode = modeList
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) updateDislike(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.filtered[m.cursor]
	switch msg.String() {
	case "y", "Y":
		m.mode = modeList
		_ = m.bl.Add(e.channel)

		// Remove the blocked channel from the in-memory list immediately.
		fresh := make([]tuiEntry, 0, len(m.tabs[tabForYou]))
		for _, fe := range m.tabs[tabForYou] {
			if !strings.EqualFold(fe.channel, e.channel) {
				fresh = append(fresh, fe)
			}
		}
		m.tabs[tabForYou] = fresh
		m.filtered = m.applyFilter()
		if m.cursor >= len(m.filtered) && m.cursor > 0 {
			m.cursor--
		}
		m.status = fmt.Sprintf("%s blocked from recommendations", e.channel)
	case "n", "N", "esc", "enter":
		m.mode = modeList
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// ── navigation helpers ────────────────────────────────────────────────────────

func (m model) switchTab(t tab) (tea.Model, tea.Cmd) {
	m.activeTab = t
	m.cursor, m.filter, m.err, m.status = 0, "", "", ""
	m.mode = modeList

	if !m.loaded[t] && !m.loading[t] {
		m.loading[t] = true
		return m, m.loadTab(t)
	}
	m.filtered = m.applyFilter()
	return m, nil
}

func (m model) drillIntoCategory(e tuiEntry) (tea.Model, tea.Cmd) {
	m.categoryID = e.channel
	m.categoryName = e.channel
	m.loading[tabCategories] = true
	m.err = ""
	m.cursor = 0
	m.filtered = nil
	return m, func() tea.Msg {
		entries, err := loadGameStreams(m.cfg, e.channel)
		return gameStreamsLoadedMsg{entries: entries, err: err}
	}
}

func (m model) backToCategories() (tea.Model, tea.Cmd) {
	m.categoryID, m.categoryName = "", ""
	m.cursor = 0
	m.loading[tabCategories] = true
	m.err = ""
	m.filtered = nil
	return m, func() tea.Msg {
		entries, err := loadCategories(m.cfg)
		return categoriesLoadedMsg{entries: entries, err: err}
	}
}

func (m model) applyFilter() []tuiEntry {
	src := m.tabs[m.activeTab]
	if m.filter == "" {
		return src
	}
	f := strings.ToLower(m.filter)
	out := make([]tuiEntry, 0, len(src))
	for _, e := range src {
		if strings.Contains(strings.ToLower(e.channel), f) ||
			strings.Contains(strings.ToLower(e.game), f) ||
			strings.Contains(strings.ToLower(e.title), f) {
			out = append(out, e)
		}
	}
	return out
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return ""
	}

	// Fixed chrome: header(1) + divider(1) + divider(1) + bottom(1) = 4 lines.
	// renderList must fill exactly (m.height - 4) lines so the total never
	// exceeds the terminal height and BubbleTea won't scroll the viewport.
	const fixedLines = 4
	listHeight := m.height - fixedLines
	if listHeight < 1 {
		listHeight = 1
	}
	if m.mode == modeFilter || m.filter != "" {
		listHeight--
	}
	if m.activeTab == tabCategories && m.categoryID != "" {
		listHeight--
	}

	divider := styleDivider.Render(strings.Repeat("─", max(m.width, 1)))

	// Each element here is exactly one terminal line (no trailing \n).
	// strings.Join adds the \n between them; the final \n is intentionally
	// absent so BubbleTea controls cursor placement after the last line.
	return m.renderHeader() + "\n" +
		divider + "\n" +
		m.renderList(listHeight) +
		divider + "\n" +
		m.renderBottom()
}

func (m model) renderHeader() string {
	parts := make([]string, tabCount)
	for i, name := range tabNames {
		label := fmt.Sprintf("[%d] %s", i+1, name)
		if tab(i) == m.activeTab {
			parts[i] = styleTabActive.Render(label)
		} else {
			parts[i] = styleTab.Render(label)
		}
	}

	liveCount := 0
	for _, e := range m.tabs[m.activeTab] {
		if e.live {
			liveCount++
		}
	}
	liveStr := ""
	if liveCount > 0 {
		liveStr = styleLive.Render(fmt.Sprintf("  %d live", liveCount))
	}
	return styleHeader.Render(" twtv") + "  " + strings.Join(parts, "") + liveStr
}

// renderList renders exactly `height` lines, each terminated with \n.
// The final character of the returned string is always \n.
func (m model) renderList(height int) string {
	var b strings.Builder
	linesWritten := 0

	writeLine := func(s string) {
		b.WriteString(s + "\n")
		linesWritten++
	}

	// Optional header rows (filter bar / category breadcrumb).
	// These are already accounted for in View() via listHeight adjustments,
	// so they don't consume from `height` here.
	switch {
	case m.mode == modeFilter:
		writeLine(styleInput.Render("  / " + m.filter + "█"))
	case m.filter != "":
		writeLine(styleDim.Render("  / " + m.filter + "  [esc clear]"))
	}
	if m.activeTab == tabCategories && m.categoryID != "" {
		writeLine(styleGame.Render(fmt.Sprintf("  Category: %s  [esc / backspace to go back]", m.categoryName)))
	}
	// Undo the lines we just wrote — they were pre-subtracted from height in
	// View() and must not count toward the `height` rows we still need to fill.
	linesWritten = 0

	pad := func() {
		for linesWritten < height {
			b.WriteByte('\n')
			linesWritten++
		}
	}

	switch {
	case m.loading[m.activeTab]:
		writeLine("")
		writeLine("  " + styleDim.Render("fetching…"))
		pad()
		return b.String()
	case m.err != "":
		writeLine("")
		writeLine("  " + styleErr.Render("error: "+m.err))
		pad()
		return b.String()
	case len(m.filtered) == 0:
		writeLine("")
		writeLine("  " + styleDim.Render("nothing here"))
		pad()
		return b.String()
	}

	start := 0
	if m.cursor >= height {
		start = m.cursor - height + 1
	}
	end := min(start+height, len(m.filtered))

	prevLive := true
	for i := start; i < end && linesWritten < height; i++ {
		e := m.filtered[i]

		// Separator between live and offline sections.
		// Only draw it if there is still room for both the separator AND the row.
		if !e.isCategory && prevLive && !e.live && i > 0 && linesWritten+2 <= height {
			writeLine(styleDivider.Render("  " + strings.Repeat("─", max(m.width-2, 1))))
		}
		prevLive = e.live

		if linesWritten >= height {
			break
		}
		row := m.renderRow(e)
		if i == m.cursor {
			writeLine(styleSelected.Render("▸ " + row))
		} else {
			writeLine("  " + row)
		}
	}
	pad()

	return b.String()
}

func (m model) renderRow(e tuiEntry) string {
	if e.isCategory {
		return fmt.Sprintf("%s %s",
			styleGame.Render("▸"),
			styleGame.Render(fmt.Sprintf("%-22s", truncate(e.channel, 22))),
		)
	}

	w := m.width - 4
	ch := fmt.Sprintf("%-22s", truncate(e.channel, 22))
	if e.live {
		metaW := max(w-22-7-2, 10)
		return fmt.Sprintf("%s %s %s  %s",
			styleLive.Render("●"),
			styleLive.Render(ch),
			styleViewers.Render(fmt.Sprintf("%6d", e.viewers)),
			styleDim.Render(truncate(e.game+" — "+e.title, metaW)),
		)
	}
	return fmt.Sprintf("%s %s  %s",
		styleOffline.Render("○"),
		styleOffline.Render(ch),
		styleDim.Render("offline"),
	)
}

// renderBottom returns a single line of text without a trailing newline.
func (m model) renderBottom() string {
	switch m.mode {
	case modeChat:
		if len(m.filtered) > 0 {
			ch := channelFromURL(m.filtered[m.cursor].url)
			return styleInput.Render(fmt.Sprintf("  open chat for %s? [y/n] ", ch))
		}
	case modeDislike:
		if len(m.filtered) > 0 {
			return styleDislike.Render(fmt.Sprintf("  block %s from recommendations? [y/n] ", m.filtered[m.cursor].channel))
		}
	}
	if m.status != "" {
		return styleDim.Render("  " + m.status)
	}
	return "  " + m.renderKeyHints()
}

func (m model) renderKeyHints() string {
	type kv struct{ k, v string }
	hints := []kv{
		{"↑↓ jk", "move"}, {"enter", "play"}, {"c", "chat"},
		{"1-4", "tabs"}, {"/", "filter"}, {"esc", "clear/back"},
	}
	if m.activeTab == tabForYou {
		hints = append(hints, kv{"d", "dislike"})
	}
	hints = append(hints, kv{"q", "quit"})

	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = styleKey.Render(h.k) + styleDim.Render(" "+h.v)
	}
	return strings.Join(parts, styleDim.Render("  ·  "))
}

// ── commands ──────────────────────────────────────────────────────────────────

func (m model) loadTab(t tab) tea.Cmd {
	switch t {
	case tabFollowed:
		return func() tea.Msg {
			entries, err := loadFollowed(m.cfg, m.hist)
			return followedLoadedMsg{entries: entries, err: err}
		}
	case tabTop:
		return func() tea.Msg {
			entries, err := loadTop(m.cfg, m.bl)
			return topLoadedMsg{entries: entries, err: err}
		}
	case tabForYou:
		return func() tea.Msg {
			entries, err := loadForYou(m.cfg, m.hist, m.bl)
			return forYouLoadedMsg{entries: entries, err: err}
		}
	case tabCategories:
		return func() tea.Msg {
			entries, err := loadCategories(m.cfg)
			return categoriesLoadedMsg{entries: entries, err: err}
		}
	}
	return nil
}

func (m model) launch(e tuiEntry) tea.Cmd {
	return func() tea.Msg {
		_ = m.hist.Append(e.channel, e.url, e.game)
		if err := player.Launch(e.url, m.extra, m.quiet); err != nil {
			// Surface error back to the model via a status message.
			// For now we accept the limitation that tea.Cmd cannot
			// directly mutate model state; a future improvement would
			// return a typed error message.
			_ = err
		}
		return nil
	}
}

// ── data loaders ──────────────────────────────────────────────────────────────

func newClient(cfg *config.Config) *twitch.Client {
	return twitch.New(cfg.Auth.ClientID, cfg.Auth.AccessToken)
}

func loadFollowed(cfg *config.Config, hist *history.History) ([]tuiEntry, error) {
	tc := newClient(cfg)

	userID, err := tc.UserID()
	if err != nil {
		return nil, err
	}
	follows, err := tc.Follows(userID)
	if err != nil {
		return nil, err
	}

	logins := make([]string, len(follows))
	for i, f := range follows {
		logins[i] = f.BroadcasterLogin
	}

	streams, err := tc.LiveStreams(logins)
	if err != nil {
		return nil, err
	}

	entries := make([]tuiEntry, 0, len(streams))
	for _, s := range streams {
		entries = append(entries, streamToEntry(s))
	}
	sortByViewers(entries)

	// Append offline followed channels from history (deduped).
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[strings.ToLower(e.channel)] = true
	}
	histEntries, _ := hist.Read()
	for i := len(histEntries) - 1; i >= 0; i-- {
		he := histEntries[i]
		ch := strings.ToLower(he.Channel)
		if seen[ch] {
			continue
		}
		seen[ch] = true
		entries = append(entries, tuiEntry{channel: he.Channel, url: he.URL, game: he.Game})
	}

	return entries, nil
}

func loadTop(cfg *config.Config, bl *blocklist.Blocklist) ([]tuiEntry, error) {
	tc := newClient(cfg)
	streams, err := tc.TopStreams(50)
	if err != nil {
		return nil, err
	}
	entries := make([]tuiEntry, 0, len(streams))
	for _, s := range streams {
		if !bl.Has(s.UserLogin) {
			entries = append(entries, streamToEntry(s))
		}
	}
	return entries, nil
}

func loadForYou(cfg *config.Config, hist *history.History, bl *blocklist.Blocklist) ([]tuiEntry, error) {
	tc := newClient(cfg)

	topGames := hist.TopGames(5)
	if len(topGames) == 0 {
		return nil, fmt.Errorf("watch some streams first — recommendations are based on your history")
	}

	games, err := tc.GamesByName(topGames)
	if err != nil {
		return nil, err
	}

	watched := hist.WatchedChannels()
	seen := make(map[string]bool)
	var entries []tuiEntry

	for _, g := range games {
		streams, err := tc.StreamsByGame(g.ID, 20)
		if err != nil {
			continue // partial failure — skip this game, keep others
		}
		for _, s := range streams {
			ch := strings.ToLower(s.UserLogin)
			if seen[ch] || bl.Has(s.UserLogin) {
				continue
			}
			seen[ch] = true
			e := streamToEntry(s)
			e.viewers += watched[ch] * 100 // boost previously watched channels
			entries = append(entries, e)
		}
	}

	sortByViewers(entries)
	return entries, nil
}

func loadCategories(cfg *config.Config) ([]tuiEntry, error) {
	tc := newClient(cfg)
	games, err := tc.TopGames(50)
	if err != nil {
		return nil, err
	}
	entries := make([]tuiEntry, len(games))
	for i, g := range games {
		entries[i] = tuiEntry{channel: g.Name, isCategory: true}
	}
	return entries, nil
}

func loadGameStreams(cfg *config.Config, gameName string) ([]tuiEntry, error) {
	tc := newClient(cfg)
	games, err := tc.GamesByName([]string{gameName})
	if err != nil || len(games) == 0 {
		return nil, fmt.Errorf("game not found: %s", gameName)
	}
	streams, err := tc.StreamsByGame(games[0].ID, 40)
	if err != nil {
		return nil, err
	}
	entries := make([]tuiEntry, 0, len(streams))
	for _, s := range streams {
		entries = append(entries, streamToEntry(s))
	}
	sortByViewers(entries)
	return entries, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func streamToEntry(s twitch.Stream) tuiEntry {
	return tuiEntry{
		channel: s.UserLogin,
		url:     toURL(s.UserLogin),
		game:    s.GameName,
		title:   s.Title,
		viewers: s.ViewerCount,
		live:    true,
	}
}

func sortByViewers(e []tuiEntry) {
	for i := 1; i < len(e); i++ {
		for j := i; j > 0 && e[j].viewers > e[j-1].viewers; j-- {
			e[j], e[j-1] = e[j-1], e[j]
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}


func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
