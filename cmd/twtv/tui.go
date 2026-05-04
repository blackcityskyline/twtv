package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/user/twtv/internal/config"
	"github.com/user/twtv/internal/history"
	"github.com/user/twtv/internal/muted"
	"github.com/user/twtv/internal/player"
	"github.com/user/twtv/internal/preview"
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
	styleErr       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleTab       = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Padding(0, 1)
	styleTabActive = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true).Background(lipgloss.Color("236")).Padding(0, 1)
	styleHeader    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleGame      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleMuted     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleConfirm   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	// Key hints: bright white key + normal white description — clearly visible.
	styleHintKey = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	styleHintVal = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	styleHintSep = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// ── tabs ──────────────────────────────────────────────────────────────────────

type tab int

const (
	tabFollowed tab = iota
	tabCategories
	tabForYou
	tabTop
	tabMuted
	tabCount // sentinel — always last
)

var tabLabels = [tabCount]string{
	tabFollowed:   "followed",
	tabCategories: "categories",
	tabForYou:     "for you",
	tabTop:        "top",
	tabMuted:      "muted",
}

// ── messages ──────────────────────────────────────────────────────────────────

type tabLoadedMsg struct {
	t       tab
	entries []entry
	err     error
}

type gameStreamsLoadedMsg struct {
	entries []entry
	err     error
}

type previewDoneMsg struct{ err error }

// ── data types ────────────────────────────────────────────────────────────────

// entry is a single row in any tab.
type entry struct {
	channel      string
	url          string
	game         string
	title        string
	thumbnailURL string
	viewers      int
	live         bool
	isCategory   bool
	isMuted      bool // true when shown in the muted tab
}

type mode int

const (
	modeList mode = iota
	modeFilter
	modeChat
	modeMuteConfirm   // "mute this channel? [y/n]"
	modeUnmuteConfirm // "unmute this channel? [y/n]"
	modePreview       // waiting for preview to render
)

// ── model ─────────────────────────────────────────────────────────────────────

type model struct {
	cfg   *config.Config
	hist  *history.History
	ml    *muted.List // ml = mute list
	extra []string
	quiet bool

	tabs    [tabCount][]entry
	loaded  [tabCount]bool
	loading [tabCount]bool

	activeTab tab
	filtered  []entry
	cursor    int

	mode   mode
	filter string
	err    string
	status string
	width  int
	height int
	ready  bool

	// category drill-down state
	categoryID   string
	categoryName string
}

func newModel(cfg *config.Config, hist *history.History, ml *muted.List, extra []string, quiet bool) model {
	m := model{cfg: cfg, hist: hist, ml: ml, extra: extra, quiet: quiet}
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

	case tabLoadedMsg:
		m.loading[msg.t] = false
		m.loaded[msg.t] = true
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.tabs[msg.t] = msg.entries
		}
		if m.activeTab == msg.t {
			m.filtered = m.applyFilter()
		}

	case gameStreamsLoadedMsg:
		m.loading[tabCategories] = false
		m.loaded[tabCategories] = true
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.tabs[tabCategories] = msg.entries
		}
		if m.activeTab == tabCategories {
			m.filtered = m.applyFilter()
		}

	case previewDoneMsg:
		m.mode = modeList
		if msg.err != nil {
			m.status = "preview: " + msg.err.Error()
		}

	case tea.KeyMsg:
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modeChat:
			return m.updateChat(msg)
		case modeMuteConfirm:
			return m.updateMuteConfirm(msg)
		case modeUnmuteConfirm:
			return m.updateUnmuteConfirm(msg)
		case modePreview:
			// block all keys while preview is rendering
		default:
			return m.updateList(msg)
		}
	}

	return m, nil
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
	case "5":
		return m.switchTab(tabMuted)
	case "tab":
		return m.switchTab((m.activeTab + 1) % tabCount)

	case "enter":
		if len(m.filtered) == 0 {
			break
		}
		e := m.filtered[m.cursor]
		if e.isCategory {
			return m.drillCategory(e)
		}
		return m, m.launch(e)

	case "p":
		// Preview — only for live streams.
		if len(m.filtered) > 0 && m.filtered[m.cursor].live {
			return m.showPreview(m.filtered[m.cursor])
		}

	case "c":
		if len(m.filtered) > 0 && !m.filtered[m.cursor].isCategory {
			m.mode = modeChat
		}

	case "m":
		// m = mute (from any tab except muted itself)
		if len(m.filtered) > 0 && m.activeTab != tabMuted && !m.filtered[m.cursor].isCategory {
			m.mode = modeMuteConfirm
		}

	case "u":
		// u = unmute (only in muted tab)
		if len(m.filtered) > 0 && m.activeTab == tabMuted {
			m.mode = modeUnmuteConfirm
		}

	case "/":
		m.mode = modeFilter

	case "esc", "backspace":
		if m.activeTab == tabCategories && m.categoryID != "" {
			return m.backToCategories()
		}
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

func (m model) updateMuteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.filtered[m.cursor]
	switch msg.String() {
	case "y", "Y":
		m.mode = modeList
		_ = m.ml.Mute(e.channel)

		// Remove from current tab immediately.
		fresh := make([]entry, 0, len(m.tabs[m.activeTab]))
		for _, fe := range m.tabs[m.activeTab] {
			if !strings.EqualFold(fe.channel, e.channel) {
				fresh = append(fresh, fe)
			}
		}
		m.tabs[m.activeTab] = fresh
		m.filtered = m.applyFilter()
		if m.cursor >= len(m.filtered) && m.cursor > 0 {
			m.cursor--
		}

		// Invalidate muted tab so it reloads next time.
		m.loaded[tabMuted] = false

		m.status = fmt.Sprintf("%s muted", e.channel)
	case "n", "N", "esc", "enter":
		m.mode = modeList
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) updateUnmuteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.filtered[m.cursor]
	switch msg.String() {
	case "y", "Y":
		m.mode = modeList
		_ = m.ml.Unmute(e.channel)

		fresh := make([]entry, 0, len(m.tabs[tabMuted]))
		for _, fe := range m.tabs[tabMuted] {
			if !strings.EqualFold(fe.channel, e.channel) {
				fresh = append(fresh, fe)
			}
		}
		m.tabs[tabMuted] = fresh
		m.filtered = m.applyFilter()
		if m.cursor >= len(m.filtered) && m.cursor > 0 {
			m.cursor--
		}
		m.status = fmt.Sprintf("%s unmuted", e.channel)
	case "n", "N", "esc", "enter":
		m.mode = modeList
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// ── navigation ────────────────────────────────────────────────────────────────

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

func (m model) drillCategory(e entry) (tea.Model, tea.Cmd) {
	m.categoryID = e.channel
	m.categoryName = e.channel
	m.loading[tabCategories] = true
	m.err, m.cursor = "", 0
	m.filtered = nil
	return m, func() tea.Msg {
		entries, err := loadGameStreams(m.cfg, e.channel)
		return gameStreamsLoadedMsg{entries: entries, err: err}
	}
}

func (m model) backToCategories() (tea.Model, tea.Cmd) {
	m.categoryID, m.categoryName = "", ""
	m.loading[tabCategories] = true
	m.err, m.cursor = "", 0
	m.filtered = nil
	return m, func() tea.Msg {
		entries, err := loadCategories(m.cfg)
		return tabLoadedMsg{t: tabCategories, entries: entries, err: err}
	}
}

func (m model) applyFilter() []entry {
	src := m.tabs[m.activeTab]
	if m.filter == "" {
		return src
	}
	f := strings.ToLower(m.filter)
	out := make([]entry, 0, len(src))
	for _, e := range src {
		if strings.Contains(strings.ToLower(e.channel), f) ||
			strings.Contains(strings.ToLower(e.game), f) ||
			strings.Contains(strings.ToLower(e.title), f) {
			out = append(out, e)
		}
	}
	return out
}

// ── preview ───────────────────────────────────────────────────────────────────

func (m model) showPreview(e entry) (tea.Model, tea.Cmd) {
	m.mode = modePreview
	m.status = fmt.Sprintf("loading preview for %s…", e.channel)
	cols := m.width
	rows := (m.height * 2) / 3
	return m, func() tea.Msg {
		err := preview.Show(e.channel, cols, rows)
		return previewDoneMsg{err: err}
	}
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return ""
	}

	// Fixed chrome: header(1) + divider(1) + divider(1) + bottom(1) = 4 lines.
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

	return m.renderHeader() + "\n" +
		divider + "\n" +
		m.renderList(listHeight) +
		divider + "\n" +
		m.renderBottom()
}

func (m model) renderHeader() string {
	parts := make([]string, tabCount)
	for i, label := range tabLabels {
		text := fmt.Sprintf("[%d] %s", i+1, label)
		if tab(i) == m.activeTab {
			parts[i] = styleTabActive.Render(text)
		} else {
			parts[i] = styleTab.Render(text)
		}
	}

	liveCount := 0
	for _, e := range m.tabs[m.activeTab] {
		if e.live {
			liveCount++
		}
	}
	live := ""
	if liveCount > 0 {
		live = styleLive.Render(fmt.Sprintf("  %d live", liveCount))
	}
	return styleHeader.Render(" twtv") + "  " + strings.Join(parts, "") + live
}

// renderList renders exactly `height` lines each ending with \n.
func (m model) renderList(height int) string {
	var b strings.Builder
	written := 0

	line := func(s string) {
		b.WriteString(s + "\n")
		written++
	}
	pad := func() {
		for written < height {
			b.WriteByte('\n')
			written++
		}
	}

	// Optional header rows — pre-subtracted from height in View().
	switch {
	case m.mode == modeFilter:
		line(styleInput.Render("  / " + m.filter + "█"))
	case m.filter != "":
		line(styleDim.Render("  / " + m.filter + "  [esc clear]"))
	}
	if m.activeTab == tabCategories && m.categoryID != "" {
		line(styleGame.Render(fmt.Sprintf("  Category: %s  [esc to go back]", m.categoryName)))
	}
	written = 0 // reset: above lines don't count against height budget

	switch {
	case m.loading[m.activeTab]:
		line("")
		line("  " + styleDim.Render("fetching…"))
		pad()
		return b.String()
	case m.err != "":
		line("")
		line("  " + styleErr.Render("error: "+m.err))
		pad()
		return b.String()
	case len(m.filtered) == 0:
		line("")
		if m.activeTab == tabMuted {
			line("  " + styleDim.Render("no muted channels"))
		} else {
			line("  " + styleDim.Render("nothing here"))
		}
		pad()
		return b.String()
	}

	start := 0
	if m.cursor >= height {
		start = m.cursor - height + 1
	}
	end := min(start+height, len(m.filtered))

	prevLive := true
	for i := start; i < end && written < height; i++ {
		e := m.filtered[i]

		// Separator between live and offline sections.
		if !e.isCategory && prevLive && !e.live && i > 0 && written+2 <= height {
			line(styleDivider.Render("  " + strings.Repeat("─", max(m.width-2, 1))))
		}
		prevLive = e.live

		if written >= height {
			break
		}
		row := m.renderRow(e)
		if i == m.cursor {
			line(styleSelected.Render("▸ " + row))
		} else {
			line("  " + row)
		}
	}
	pad()
	return b.String()
}

func (m model) renderRow(e entry) string {
	if e.isCategory {
		return fmt.Sprintf("%s %s",
			styleGame.Render("▸"),
			styleGame.Render(fmt.Sprintf("%-22s", truncate(e.channel, 22))),
		)
	}
	if e.isMuted {
		return fmt.Sprintf("%s %s",
			styleMuted.Render("✕"),
			styleMuted.Render(fmt.Sprintf("%-22s", truncate(e.channel, 22))),
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

func (m model) renderBottom() string {
	switch m.mode {
	case modeChat:
		if len(m.filtered) > 0 {
			ch := channelFromURL(m.filtered[m.cursor].url)
			return styleConfirm.Render(fmt.Sprintf("  open chat for %s? [y/n] ", ch))
		}
	case modeMuteConfirm:
		if len(m.filtered) > 0 {
			return styleConfirm.Render(fmt.Sprintf("  mute %s? [y/n] ", m.filtered[m.cursor].channel))
		}
	case modeUnmuteConfirm:
		if len(m.filtered) > 0 {
			return styleConfirm.Render(fmt.Sprintf("  unmute %s? [y/n] ", m.filtered[m.cursor].channel))
		}
	case modePreview:
		return styleDim.Render("  " + m.status)
	}
	if m.status != "" {
		return styleDim.Render("  " + m.status)
	}
	return "  " + m.renderHints()
}

func (m model) renderHints() string {
	type hint struct{ key, val string }

	hints := []hint{
		{"↑↓/jk", "move"},
		{"enter", "play"},
		{"p", "preview"},
		{"c", "chat"},
		{"1-5", "tabs"},
		{"/", "filter"},
		{"esc", "back"},
	}
	if m.activeTab == tabMuted {
		hints = append(hints, hint{"u", "unmute"})
	} else {
		hints = append(hints, hint{"m", "mute"})
	}
	hints = append(hints, hint{"q", "quit"})

	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = styleHintKey.Render(h.key) + styleHintVal.Render(" "+h.val)
	}
	return strings.Join(parts, styleHintSep.Render("  ·  "))
}

// ── commands ──────────────────────────────────────────────────────────────────

func (m model) loadTab(t tab) tea.Cmd {
	switch t {
	case tabFollowed:
		return func() tea.Msg {
			entries, err := loadFollowed(m.cfg, m.hist, m.ml)
			return tabLoadedMsg{t: tabFollowed, entries: entries, err: err}
		}
	case tabTop:
		return func() tea.Msg {
			entries, err := loadTop(m.cfg, m.ml)
			return tabLoadedMsg{t: tabTop, entries: entries, err: err}
		}
	case tabForYou:
		return func() tea.Msg {
			entries, err := loadForYou(m.cfg, m.hist, m.ml)
			return tabLoadedMsg{t: tabForYou, entries: entries, err: err}
		}
	case tabCategories:
		return func() tea.Msg {
			entries, err := loadCategories(m.cfg)
			return tabLoadedMsg{t: tabCategories, entries: entries, err: err}
		}
	case tabMuted:
		return func() tea.Msg {
			entries := loadMuted(m.ml)
			return tabLoadedMsg{t: tabMuted, entries: entries, err: nil}
		}
	}
	return nil
}

func (m model) launch(e entry) tea.Cmd {
	return func() tea.Msg {
		_ = m.hist.Append(e.channel, e.url, e.game)
		if err := player.Launch(e.url, m.extra, m.quiet); err != nil {
			_ = err // future: surface via typed msg
		}
		return nil
	}
}

// ── data loaders ──────────────────────────────────────────────────────────────

func newClient(cfg *config.Config) *twitch.Client {
	return twitch.New(cfg.Auth.ClientID, cfg.Auth.AccessToken)
}

func loadFollowed(cfg *config.Config, hist *history.History, ml *muted.List) ([]entry, error) {
	tc := newClient(cfg)

	userID, err := tc.UserID()
	if err != nil {
		return nil, err
	}
	follows, err := tc.Follows(userID)
	if err != nil {
		return nil, err
	}

	logins := make([]string, 0, len(follows))
	for _, f := range follows {
		if !ml.Has(f.BroadcasterLogin) {
			logins = append(logins, f.BroadcasterLogin)
		}
	}

	streams, err := tc.LiveStreams(logins)
	if err != nil {
		return nil, err
	}

	entries := make([]entry, 0, len(streams))
	for _, s := range streams {
		entries = append(entries, streamToEntry(s))
	}
	sortByViewers(entries)

	// Append offline followed channels from history (deduped, mute-filtered).
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[strings.ToLower(e.channel)] = true
	}
	histEntries, _ := hist.Read()
	for i := len(histEntries) - 1; i >= 0; i-- {
		he := histEntries[i]
		ch := strings.ToLower(he.Channel)
		if seen[ch] || ml.Has(he.Channel) {
			continue
		}
		seen[ch] = true
		entries = append(entries, entry{channel: he.Channel, url: he.URL, game: he.Game})
	}
	return entries, nil
}

func loadTop(cfg *config.Config, ml *muted.List) ([]entry, error) {
	tc := newClient(cfg)
	streams, err := tc.TopStreams(50)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(streams))
	for _, s := range streams {
		if !ml.Has(s.UserLogin) {
			entries = append(entries, streamToEntry(s))
		}
	}
	return entries, nil
}

func loadForYou(cfg *config.Config, hist *history.History, ml *muted.List) ([]entry, error) {
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
	var entries []entry

	for _, g := range games {
		streams, err := tc.StreamsByGame(g.ID, 20)
		if err != nil {
			continue // partial failure: skip this game, keep others
		}
		for _, s := range streams {
			ch := strings.ToLower(s.UserLogin)
			if seen[ch] || ml.Has(s.UserLogin) {
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

func loadCategories(cfg *config.Config) ([]entry, error) {
	tc := newClient(cfg)
	games, err := tc.TopGames(50)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, len(games))
	for i, g := range games {
		entries[i] = entry{channel: g.Name, isCategory: true}
	}
	return entries, nil
}

func loadGameStreams(cfg *config.Config, gameName string) ([]entry, error) {
	tc := newClient(cfg)
	games, err := tc.GamesByName([]string{gameName})
	if err != nil || len(games) == 0 {
		return nil, fmt.Errorf("game not found: %s", gameName)
	}
	streams, err := tc.StreamsByGame(games[0].ID, 40)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(streams))
	for _, s := range streams {
		entries = append(entries, streamToEntry(s))
	}
	sortByViewers(entries)
	return entries, nil
}

// loadMuted builds the muted tab entries from the mute list (no API call needed).
func loadMuted(ml *muted.List) []entry {
	channels := ml.Channels()
	entries := make([]entry, len(channels))
	for i, ch := range channels {
		entries[i] = entry{
			channel: ch,
			url:     toURL(ch),
			isMuted: true,
		}
	}
	return entries
}

// ── helpers ───────────────────────────────────────────────────────────────────

func streamToEntry(s twitch.Stream) entry {
	return entry{
		channel:      s.UserLogin,
		url:          toURL(s.UserLogin),
		game:         s.GameName,
		title:        s.Title,
		thumbnailURL: s.ThumbnailURL,
		viewers:      s.ViewerCount,
		live:         true,
	}
}

func sortByViewers(e []entry) {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
