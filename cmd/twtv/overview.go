package main

// overview.go — channel overview page (key: i)
//
// Layout (conceptual):
//
//   ┌──────────────────────────────────────────────────────┐
//   │  twtv  ‹ channelname                                 │  header
//   ├──────────────────────────────────────────────────────┤
//   │  DisplayName  ● LIVE  1.2K viewers  ✓ following      │
//   │  Followers: 345.6K  •  Playing: Some Game            │
//   │  Description text wrapped to terminal width…         │
//   ├──────────────────────────────────────────────────────┤
//   │  [videos]  [clips]          [ prev  ] next           │  sub-tabs
//   ├──────────────────────────────────────────────────────┤
//   │  ▸ VOD  Title of stream            2h34m  1234 views │
//   │    …                                                 │
//   ├──────────────────────────────────────────────────────┤
//   │  ↑↓/jk move · enter play · p preview · [ ] tabs …   │  hints
//   └──────────────────────────────────────────────────────┘
//
// Sub-tab navigation uses [ and ] so it never conflicts with
// the global 1-5 / Tab keys of the main list.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/user/twtv/internal/config"
	"github.com/user/twtv/internal/preview"
	"github.com/user/twtv/internal/twitch"
)

// ── sub-tabs ──────────────────────────────────────────────────────────────────

type overviewSubTab int

const (
	ovTabVideos overviewSubTab = iota
	ovTabClips
	ovTabCount
)

var ovTabLabels = [ovTabCount]string{
	ovTabVideos: "videos",
	ovTabClips:  "clips",
}

// ── styles ────────────────────────────────────────────────────────────────────

var (
	styleOvHeader       = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleOvBack         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOvName         = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	styleOvLive         = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleOvOffline      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOvFollowing    = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleOvMeta         = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	styleOvGame         = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleOvDesc         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOvViewers      = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	styleOvDuration     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleOvDim          = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOvErr          = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleOvSelected     = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	styleOvDivider      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOvSubTab       = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Padding(0, 1)
	styleOvSubTabActive = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true).
				Background(lipgloss.Color("236")).Padding(0, 1)
	styleOvHintKey = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	styleOvHintVal = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	styleOvHintSep = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// ── messages ──────────────────────────────────────────────────────────────────

type overviewInfoLoadedMsg struct {
	info *twitch.ChannelInfo
	err  error
}

type overviewVideosLoadedMsg struct {
	videos []twitch.Video
	err    error
}

type overviewClipsLoadedMsg struct {
	clips []twitch.Clip
	err   error
}

// ── data model ────────────────────────────────────────────────────────────────

type overviewModel struct {
	login  string
	userID string // authenticated user id (for follow check)

	info       *twitch.ChannelInfo
	infoErr    string
	infoLoaded bool

	videos       []twitch.Video
	videosErr    string
	videosLoaded bool

	clips       []twitch.Clip
	clipsErr    string
	clipsLoaded bool

	activeSubTab overviewSubTab
	cursor       int
	status       string
}

func newOverviewModel(login, authUserID string) overviewModel {
	return overviewModel{login: login, userID: authUserID}
}

// currentLen returns the row count in the active sub-tab.
func (ov *overviewModel) currentLen() int {
	switch ov.activeSubTab {
	case ovTabVideos:
		return len(ov.videos)
	case ovTabClips:
		return len(ov.clips)
	}
	return 0
}

// selectedURL returns the playable URL of the highlighted row.
func (ov *overviewModel) selectedURL() string {
	switch ov.activeSubTab {
	case ovTabVideos:
		if ov.cursor < len(ov.videos) {
			return ov.videos[ov.cursor].URL
		}
	case ovTabClips:
		if ov.cursor < len(ov.clips) {
			return ov.clips[ov.cursor].URL
		}
	}
	return ""
}

// selectedThumb returns the thumbnail URL of the highlighted row.
func (ov *overviewModel) selectedThumb() string {
	switch ov.activeSubTab {
	case ovTabVideos:
		if ov.cursor < len(ov.videos) {
			return preview.ExpandThumbURL(ov.videos[ov.cursor].ThumbnailURL, 1280, 720)
		}
	case ovTabClips:
		if ov.cursor < len(ov.clips) {
			return ov.clips[ov.cursor].ThumbnailURL
		}
	}
	return ""
}

// ── init commands ─────────────────────────────────────────────────────────────

func cmdLoadOverviewInfo(cfg *config.Config, login, authUserID string) tea.Cmd {
	return func() tea.Msg {
		tc := twitch.New(cfg.Auth.ClientID, cfg.Auth.AccessToken)
		info, err := tc.ChannelInfoByLogin(login, authUserID)
		return overviewInfoLoadedMsg{info: info, err: err}
	}
}

func cmdLoadOverviewVideos(cfg *config.Config, broadcasterID string) tea.Cmd {
	return func() tea.Msg {
		tc := twitch.New(cfg.Auth.ClientID, cfg.Auth.AccessToken)
		videos, err := tc.Videos(broadcasterID, 40, "")
		return overviewVideosLoadedMsg{videos: videos, err: err}
	}
}

func cmdLoadOverviewClips(cfg *config.Config, broadcasterID string) tea.Cmd {
	return func() tea.Msg {
		tc := twitch.New(cfg.Auth.ClientID, cfg.Auth.AccessToken)
		clips, err := tc.Clips(broadcasterID, 40)
		return overviewClipsLoadedMsg{clips: clips, err: err}
	}
}

// ── key handler ───────────────────────────────────────────────────────────────

// updateOverview handles key input in modeOverview.
// Returns (model, cmd, exitOverview).
func (m model) updateOverview(msg tea.KeyMsg) (model, tea.Cmd, bool) {
	ov := &m.overview

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit, false

	case "i", "esc":
		return m, nil, true // exit overview, back to main list

	case "up", "k":
		if ov.cursor > 0 {
			ov.cursor--
		}
	case "down", "j":
		if ov.cursor < ov.currentLen()-1 {
			ov.cursor++
		}

	case "[":
		if ov.activeSubTab > 0 {
			ov.activeSubTab--
			ov.cursor = 0
		}
	case "]":
		if ov.activeSubTab < ovTabCount-1 {
			ov.activeSubTab++
			ov.cursor = 0
			// Lazy-load clips on first visit.
			if ov.activeSubTab == ovTabClips && !ov.clipsLoaded && ov.info != nil {
				m.overview = *ov
				return m, cmdLoadOverviewClips(m.cfg, ov.info.ID), false
			}
		}

	case "enter":
		url := ov.selectedURL()
		if url == "" {
			break
		}
		ch := ov.login
		game := ""
		if ov.info != nil {
			game = ov.info.GameName
		}
		_ = m.hist.Append(ch, url, game)
		if err := playerLaunchURL(m, url); err != nil {
			ov.status = "player error: " + err.Error()
		}

	case "p":
		thumb := ov.selectedThumb()
		if thumb == "" {
			ov.status = "no thumbnail available"
			break
		}
		cols := m.width
		rows := (m.height * 2) / 3
		cmd, err := preview.CommandFromURL(thumb, cols, rows)
		if err != nil {
			ov.status = "preview: " + err.Error()
			break
		}
		m.overview = *ov
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return previewDoneMsg{err: err}
		}), false
	}

	m.overview = *ov
	return m, nil, false
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) renderOverview() string {
	divider := styleOvDivider.Render(strings.Repeat("─", max(m.width, 1)))
	var b strings.Builder

	// Header
	b.WriteString(styleOvHeader.Render(" twtv") + "  " +
		styleOvBack.Render("‹ ") + styleOvName.Render(m.overview.login) + "\n")
	b.WriteString(divider + "\n")

	// Channel info block
	infoLines := m.renderOvInfo()
	for _, l := range infoLines {
		b.WriteString(l + "\n")
	}
	b.WriteString(divider + "\n")

	// Sub-tab bar
	b.WriteString(m.renderOvSubTabBar() + "\n")
	b.WriteString(divider + "\n")

	// Fixed chrome: header+div + len(infoLines)+div + subtab+div + bottom = 4 + infoLines
	fixedLines := 1 + 1 + len(infoLines) + 1 + 1 + 1 + 1
	listHeight := m.height - fixedLines
	if listHeight < 1 {
		listHeight = 1
	}

	b.WriteString(m.renderOvList(listHeight))
	b.WriteString(divider + "\n")
	b.WriteString(m.renderOvBottom())

	return b.String()
}

func (m model) renderOvInfo() []string {
	ov := m.overview
	if !ov.infoLoaded {
		return []string{"", "  " + styleOvDim.Render("fetching channel info…")}
	}
	if ov.infoErr != "" {
		return []string{"", "  " + styleOvErr.Render("error: "+ov.infoErr)}
	}
	info := ov.info
	if info == nil {
		return []string{"  " + styleOvDim.Render("no info")}
	}

	var lines []string

	// Line 1: DisplayName · live/offline · following badge
	liveStr := " " + styleOvOffline.Render("○ offline")
	if info.IsLive {
		liveStr = " " + styleOvLive.Render("● LIVE") +
			" " + styleOvViewers.Render(formatViewers(info.ViewerCount)+" viewers")
		if info.StartedAt != "" {
			liveStr += " " + styleOvDim.Render("since "+formatStartedAt(info.StartedAt))
		}
	}
	followBadge := ""
	if info.IsFollowing {
		followBadge = "  " + styleOvFollowing.Render("✓ following")
	}
	lines = append(lines, "  "+styleOvName.Render(info.DisplayName)+liveStr+followBadge)

	// Line 2: followers · game
	meta := "Followers: " + formatFollowers(info.FollowerCount)
	if info.GameName != "" {
		meta += "  •  "
	}
	line2 := "  " + styleOvMeta.Render(meta)
	if info.GameName != "" {
		line2 += styleOvGame.Render("Playing: "+info.GameName)
	}
	lines = append(lines, line2)

	// Lines 3+: description (wrapped, max 3 lines)
	if desc := strings.TrimSpace(info.Description); desc != "" {
		for _, l := range wrapText(desc, m.width-4, 3) {
			lines = append(lines, "  "+styleOvDesc.Render(l))
		}
	}

	return lines
}

func (m model) renderOvSubTabBar() string {
	parts := make([]string, ovTabCount)
	for i, label := range ovTabLabels {
		txt := "[" + label + "]"
		if overviewSubTab(i) == m.overview.activeSubTab {
			parts[i] = styleOvSubTabActive.Render(txt)
		} else {
			parts[i] = styleOvSubTab.Render(txt)
		}
	}
	nav := styleOvDim.Render("  [ prev  ] next")
	return "  " + strings.Join(parts, "  ") + nav
}

func (m model) renderOvList(height int) string {
	ov := m.overview
	var b strings.Builder
	written := 0

	writeLine := func(s string) {
		b.WriteString(s + "\n")
		written++
	}
	pad := func() {
		for written < height {
			b.WriteByte('\n')
			written++
		}
	}

	switch ov.activeSubTab {
	case ovTabVideos:
		if !ov.videosLoaded {
			writeLine("")
			writeLine("  " + styleOvDim.Render("fetching videos…"))
			pad()
			return b.String()
		}
		if ov.videosErr != "" {
			writeLine("")
			writeLine("  " + styleOvErr.Render("error: "+ov.videosErr))
			pad()
			return b.String()
		}
		if len(ov.videos) == 0 {
			writeLine("")
			writeLine("  " + styleOvDim.Render("no videos found"))
			pad()
			return b.String()
		}
		start, end := ovScrollWindow(ov.cursor, height, len(ov.videos))
		for i := start; i < end && written < height; i++ {
			row := m.renderVideoRow(ov.videos[i])
			if i == ov.cursor {
				writeLine(styleOvSelected.Render("▸ " + row))
			} else {
				writeLine("  " + row)
			}
		}

	case ovTabClips:
		if !ov.clipsLoaded {
			writeLine("")
			writeLine("  " + styleOvDim.Render("fetching clips…"))
			pad()
			return b.String()
		}
		if ov.clipsErr != "" {
			writeLine("")
			writeLine("  " + styleOvErr.Render("error: "+ov.clipsErr))
			pad()
			return b.String()
		}
		if len(ov.clips) == 0 {
			writeLine("")
			writeLine("  " + styleOvDim.Render("no clips found"))
			pad()
			return b.String()
		}
		start, end := ovScrollWindow(ov.cursor, height, len(ov.clips))
		for i := start; i < end && written < height; i++ {
			row := m.renderClipRow(ov.clips[i])
			if i == ov.cursor {
				writeLine(styleOvSelected.Render("▸ " + row))
			} else {
				writeLine("  " + row)
			}
		}
	}

	pad()
	return b.String()
}

func (m model) renderVideoRow(v twitch.Video) string {
	w := m.width - 4
	tag := videoTypeTag(v.Type)
	dur := styleOvDuration.Render(fmt.Sprintf("%8s", v.Duration))
	views := styleOvViewers.Render(fmt.Sprintf("%7d views", v.ViewCount))
	date := styleOvDim.Render(shortDate(v.PublishedAt))
	// tag(3) + sp + title + sp + dur(8) + sp + views(12) + sp + date(10)
	metaW := 3 + 1 + 8 + 1 + 12 + 1 + 10
	titleW := max(w-metaW, 10)
	title := fmt.Sprintf("%-*s", titleW, truncate(v.Title, titleW))
	return fmt.Sprintf("%s %s  %s  %s  %s", tag, title, dur, views, date)
}

func (m model) renderClipRow(c twitch.Clip) string {
	w := m.width - 4
	dur := styleOvDuration.Render(fmt.Sprintf("%6.1fs", c.Duration))
	views := styleOvViewers.Render(fmt.Sprintf("%7d views", c.ViewCount))
	date := styleOvDim.Render(shortDate(c.CreatedAt))
	metaW := 6 + 1 + 12 + 1 + 10
	titleW := max(w-metaW, 10)
	title := fmt.Sprintf("%-*s", titleW, truncate(c.Title, titleW))
	return fmt.Sprintf("%s  %s  %s  %s", title, dur, views, date)
}

func (m model) renderOvBottom() string {
	if m.overview.status != "" {
		return styleOvDim.Render("  "+m.overview.status) + "\n"
	}
	hints := []struct{ key, val string }{
		{"↑↓/jk", "move"},
		{"enter", "play"},
		{"p", "preview"},
		{"[", "prev tab"},
		{"]", "next tab"},
		{"i/esc", "back"},
		{"q", "quit"},
	}
	parts := make([]string, len(hints))
	for i, h := range hints {
		parts[i] = styleOvHintKey.Render(h.key) + styleOvHintVal.Render(" "+h.val)
	}
	return "  " + strings.Join(parts, styleOvHintSep.Render("  ·  ")) + "\n"
}

// ── helpers ───────────────────────────────────────────────────────────────────

func ovScrollWindow(cursor, height, total int) (start, end int) {
	start = 0
	if cursor >= height {
		start = cursor - height + 1
	}
	end = min(start+height, total)
	return
}

func videoTypeTag(t string) string {
	switch t {
	case "archive":
		return styleOvDim.Render("VOD")
	case "highlight":
		return styleOvLive.Render("HLT")
	case "upload":
		return styleOvMeta.Render("UPL")
	default:
		return styleOvDim.Render("   ")
	}
}

func formatViewers(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatFollowers(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatStartedAt(s string) string {
	if len(s) >= 16 {
		return s[11:16] + " UTC"
	}
	return s
}

func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// wrapText breaks text into at most maxLines lines of at most maxWidth runes.
func wrapText(text string, maxWidth, maxLines int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	var lines []string
	cur := ""
	for _, w := range words {
		if len(cur)+len(w)+1 > maxWidth {
			if cur != "" {
				lines = append(lines, cur)
				if len(lines) >= maxLines {
					if len(lines[len(lines)-1]) > maxWidth-1 {
						lines[len(lines)-1] = lines[len(lines)-1][:maxWidth-1] + "…"
					}
					return lines
				}
			}
			cur = w
		} else {
			if cur == "" {
				cur = w
			} else {
				cur += " " + w
			}
		}
	}
	if cur != "" && len(lines) < maxLines {
		lines = append(lines, cur)
	}
	return lines
}

// playerLaunchURL is a thin shim so overview.go does not import player directly.
// It is defined in tui.go.
var playerLaunchURL func(m model, url string) error
