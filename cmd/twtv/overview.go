package main

// overview.go — channel overview page (key: i)
//
// Sixel avatar rendering:
// For sixel terminals (foot) the image cannot live inside View() because
// BubbleTea overwrites those screen cells on the next redraw.
// Instead we store the raw image bytes in overviewModel.avatarData and
// call avatar.DrawSixelAt() from cmdDrawAvatar() — a tea.Cmd that writes
// the sixel escape directly to os.Stdout at the known screen position.
//
// Avatar screen position (1-based terminal rows/cols):
//   row = 3  (row 1: header, row 2: divider, row 3: first info line)
//   col = 3  (2 spaces indent + 1 for 1-based)
//
// cmdDrawAvatar is dispatched:
//   • when avatarData is first received (overviewAvatarLoadedMsg)
//   • on every WindowSizeMsg while in modeOverview (screen was redrawn)
//   • after any key that causes a View() redraw in modeOverview
//
// For kitty: avatar is embedded in View() via renderKitty() as before.
// For symbols: avatar is embedded in View() as plain UTF-8 block art.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/user/twtv/internal/avatar"
	"github.com/user/twtv/internal/config"
	"github.com/user/twtv/internal/preview"
	"github.com/user/twtv/internal/twitch"
)

// ── avatar dimensions ─────────────────────────────────────────────────────────

const (
	avatarCols  = 8 // character columns
	avatarRows  = 4 // character rows
	avatarGapOv = 3 // must equal avatar.avatarGap

	// avatarScreenRow/Col: 1-based terminal coordinates of the avatar cell.
	// Row 1 = header, Row 2 = divider → avatar starts at row 3.
	// Col 1+2 = "  " indent → avatar starts at col 3.
	avatarScreenRow = 3
	avatarScreenCol = 3
)

const avatarIndent = 2 + avatarCols + avatarGapOv

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

type overviewAvatarLoadedMsg struct {
	data  []byte   // raw image bytes (for sixel direct-draw)
	lines []string // rendered lines (for kitty/symbols, nil for sixel)
}

type overviewVideosLoadedMsg struct {
	videos []twitch.Video
	err    error
}

type overviewClipsLoadedMsg struct {
	clips []twitch.Clip
	err   error
}

type ovCleanupMsg struct{}
type ovAvatarDrawnMsg struct{} // returned after DrawSixelAt completes

// ── data model ────────────────────────────────────────────────────────────────

type overviewModel struct {
	login  string
	userID string

	info       *twitch.ChannelInfo
	infoErr    string
	infoLoaded bool

	// avatarData holds raw image bytes for sixel direct-draw.
	avatarData  []byte
	avatarLines []string // for kitty/symbols
	avatarReady bool

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

func (ov *overviewModel) currentLen() int {
	switch ov.activeSubTab {
	case ovTabVideos:
		return len(ov.videos)
	case ovTabClips:
		return len(ov.clips)
	}
	return 0
}

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

// ── commands ──────────────────────────────────────────────────────────────────

func cmdLoadOverviewInfo(cfg *config.Config, login, authUserID string) tea.Cmd {
	return func() tea.Msg {
		tc := twitch.New(cfg.Auth.ClientID, cfg.Auth.AccessToken)
		info, err := tc.ChannelInfoByLogin(login, authUserID)
		return overviewInfoLoadedMsg{info: info, err: err}
	}
}

// cmdLoadOverviewAvatar downloads image data and pre-renders for kitty/symbols.
// For sixel the lines will be nil; drawing happens via cmdDrawAvatar.
func cmdLoadOverviewAvatar(profileImageURL string) tea.Cmd {
	return func() tea.Msg {
		data, err := avatar.Download(profileImageURL)
		if err != nil || len(data) == 0 {
			return overviewAvatarLoadedMsg{}
		}
		lines := avatar.RenderData(data, avatarCols, avatarRows)
		return overviewAvatarLoadedMsg{data: data, lines: lines}
	}
}

// cmdDrawAvatar writes the sixel image directly to stdout at the avatar position.
// For kitty/symbols this is a no-op (image is already in View()).
func cmdDrawAvatar(data []byte) tea.Cmd {
	return func() tea.Msg {
		if len(data) > 0 && avatar.IsSixel() {
			_ = avatar.DrawSixelAt(data, avatarCols, avatarRows,
				avatarScreenRow, avatarScreenCol)
		}
		return ovAvatarDrawnMsg{}
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

func cmdCleanupAvatar() tea.Cmd {
	return func() tea.Msg {
		if esc := avatar.DeleteAll(); esc != "" {
			fmt.Print(esc)
		}
		return ovCleanupMsg{}
	}
}

// ── key handler ───────────────────────────────────────────────────────────────

func (m model) updateOverview(msg tea.KeyMsg) (model, tea.Cmd, bool) {
	ov := &m.overview

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit, false

	case "i", "esc":
		m.overview = *ov
		return m, cmdCleanupAvatar(), false

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
	// After any key redraw, repaint the sixel avatar.
	return m, cmdDrawAvatar(ov.avatarData), false
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) renderOverview() string {
	divider := styleOvDivider.Render(strings.Repeat("─", max(m.width, 1)))
	var b strings.Builder

	b.WriteString(styleOvHeader.Render(" twtv") + "  " +
		styleOvBack.Render("‹ ") + styleOvName.Render(m.overview.login) + "\n")
	b.WriteString(divider + "\n")

	infoLines := m.renderOvInfo()
	for _, l := range infoLines {
		b.WriteString(l + "\n")
	}
	b.WriteString(divider + "\n")
	b.WriteString(m.renderOvSubTabBar() + "\n")
	b.WriteString(divider + "\n")

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

// renderOvInfo builds the info block.
//
// For sixel: the avatar area is a plain space placeholder — actual image is
// painted by cmdDrawAvatar after BubbleTea flushes View() to the terminal.
// For kitty/symbols: av[i] is embedded directly.
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

	// ── text column ───────────────────────────────────────────────────────────

	var textLines []string

	liveStr := " " + styleOvOffline.Render("○ offline")
	if info.IsLive {
		liveStr = " " + styleOvLive.Render("● LIVE") +
			" " + styleOvViewers.Render(formatViewers(info.ViewerCount)+" viewers") +
			" " + styleOvDim.Render("since "+formatStartedAt(info.StartedAt))
	}
	followBadge := ""
	if info.IsFollowing {
		followBadge = "  " + styleOvFollowing.Render("✓ following")
	}
	textLines = append(textLines, styleOvName.Render(info.DisplayName)+liveStr+followBadge)

	line2 := styleOvMeta.Render("Followers: " + formatFollowers(info.FollowerCount))
	if info.GameName != "" {
		line2 += styleOvMeta.Render("  •  ") + styleOvGame.Render("Playing: "+info.GameName)
	}
	textLines = append(textLines, line2)

	if desc := strings.TrimSpace(info.Description); desc != "" {
		textW := m.width - avatarIndent
		if textW < 20 {
			textW = 20
		}
		for _, l := range wrapText(desc, textW, 4) {
			textLines = append(textLines, styleOvDesc.Render(l))
		}
	}

	// ── merge avatar + text ───────────────────────────────────────────────────

	av := ov.avatarLines
	isSixel := avatar.IsSixel()
	useEscape := avatar.IsEscapeProtocol() && !isSixel && len(av) > 0
	placeholder := strings.Repeat(" ", avatarCols)
	gap := strings.Repeat(" ", avatarGapOv)

	nRows := max(max(len(av), len(textLines)), avatarRows)
	result := make([]string, nRows)

	for i := range result {
		txLine := ""
		if i < len(textLines) {
			txLine = textLines[i]
		}

		if isSixel {
			// Sixel: leave plain spaces as placeholder, image painted separately.
			result[i] = "  " + placeholder + gap + txLine
		} else if useEscape {
			// Kitty: av[i] contains escape sequences with zero printable width.
			avEsc := ""
			if i < len(av) {
				avEsc = av[i]
			}
			result[i] = "  " + avEsc + txLine
		} else {
			// Symbols: plain UTF-8 block art.
			avLine := placeholder
			if i < len(av) {
				avLine = av[i]
			}
			result[i] = "  " + avLine + gap + txLine
		}
	}
	return result
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
	return "  " + strings.Join(parts, "  ") + styleOvDim.Render("  [ prev  ] next")
}

func (m model) renderOvList(height int) string {
	ov := m.overview
	var b strings.Builder
	written := 0

	wl := func(s string) { b.WriteString(s + "\n"); written++ }
	pad := func() {
		for written < height {
			b.WriteByte('\n')
			written++
		}
	}

	switch ov.activeSubTab {
	case ovTabVideos:
		if !ov.videosLoaded {
			wl("")
			wl("  " + styleOvDim.Render("fetching videos…"))
			pad()
			return b.String()
		}
		if ov.videosErr != "" {
			wl("")
			wl("  " + styleOvErr.Render("error: "+ov.videosErr))
			pad()
			return b.String()
		}
		if len(ov.videos) == 0 {
			wl("")
			wl("  " + styleOvDim.Render("no videos found"))
			pad()
			return b.String()
		}
		start, end := ovScrollWindow(ov.cursor, height, len(ov.videos))
		for i := start; i < end && written < height; i++ {
			row := m.renderVideoRow(ov.videos[i])
			if i == ov.cursor {
				wl(styleOvSelected.Render("▸ " + row))
			} else {
				wl("  " + row)
			}
		}

	case ovTabClips:
		if !ov.clipsLoaded {
			wl("")
			wl("  " + styleOvDim.Render("fetching clips…"))
			pad()
			return b.String()
		}
		if ov.clipsErr != "" {
			wl("")
			wl("  " + styleOvErr.Render("error: "+ov.clipsErr))
			pad()
			return b.String()
		}
		if len(ov.clips) == 0 {
			wl("")
			wl("  " + styleOvDim.Render("no clips found"))
			pad()
			return b.String()
		}
		start, end := ovScrollWindow(ov.cursor, height, len(ov.clips))
		for i := start; i < end && written < height; i++ {
			row := m.renderClipRow(ov.clips[i])
			if i == ov.cursor {
				wl(styleOvSelected.Render("▸ " + row))
			} else {
				wl("  " + row)
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

var playerLaunchURL func(m model, url string) error
