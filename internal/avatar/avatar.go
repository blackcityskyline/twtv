// Package avatar downloads a profile image and renders it as a string
// that can be embedded directly into a BubbleTea View() output.
//
// Protocol selection (auto-detected from environment):
//
//  1. Kitty Graphics Protocol  — $TERM == "xterm-kitty" or TERM_PROGRAM == "kitty"
//  2. Sixel (foot)             — $TERM starts with "foot"
//  3. Unicode block symbols    — universal fallback via chafa --format symbols
//
// ── Sixel rendering strategy ──────────────────────────────────────────────────
//
// Sixel images cannot be embedded inside BubbleTea's View() string because
// BubbleTea redraws the entire screen on every Update, overwriting the cells
// where the sixel was painted with the new text content.
//
// Instead, for sixel terminals we use DrawSixelAt(): the caller provides the
// terminal row/column where the avatar should appear, and we write the sixel
// escape directly to os.Stdout using ANSI cursor positioning (CSI H).
// This is called from a tea.Cmd after each redraw.
//
// View() for sixel mode returns plain spaces as a placeholder so that lipgloss
// reserves the correct layout area. The actual image is painted on top.
//
// ── Kitty rendering strategy ──────────────────────────────────────────────────
//
// For kitty we use the same direct-write approach via DrawKittyAt().
// Kitty images are deleted on overview exit via DeleteAll().
package avatar

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// avatarGap is the number of spaces between avatar right edge and text column.
// Must match avatarGapOv in overview.go.
const avatarGap = 3

// realChafaPaths is the priority list for the real chafa ELF binary,
// used to bypass wrapper scripts that may be installed in /usr/local/bin.
var realChafaPaths = []string{
	"/usr/bin/chafa",
	"/usr/local/bin/chafa",
	"/bin/chafa",
	"/opt/homebrew/bin/chafa",
}

// findChafa returns the path to the real chafa binary (isReal=true) or
// a wrapper (isReal=false). Real binary = ELF magic bytes.
func findChafa() (path string, isReal bool) {
	for _, p := range realChafaPaths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		hdr := make([]byte, 4)
		n, _ := f.Read(hdr)
		f.Close()
		if n >= 4 && hdr[0] == 0x7f && hdr[1] == 'E' {
			return p, true // ELF
		}
		if n >= 2 && hdr[0] == '#' && hdr[1] == '!' {
			continue // shell script wrapper, keep looking
		}
	}
	if p, err := exec.LookPath("chafa"); err == nil {
		return p, false
	}
	return "", false
}

// ── protocol detection ────────────────────────────────────────────────────────

type protocol int

const (
	protoSymbols protocol = iota
	protoSixel
	protoKitty
)

func detectProtocol() protocol {
	term := os.Getenv("TERM")
	termProg := os.Getenv("TERM_PROGRAM")
	if term == "xterm-kitty" || termProg == "kitty" {
		return protoKitty
	}
	if strings.HasPrefix(term, "foot") {
		return protoSixel
	}
	return protoSymbols
}

// IsEscapeProtocol reports whether the terminal uses kitty or sixel.
func IsEscapeProtocol() bool {
	p := detectProtocol()
	return p == protoKitty || p == protoSixel
}

// IsSixel reports whether the terminal uses sixel (direct-draw mode).
func IsSixel() bool {
	return detectProtocol() == protoSixel
}

// DeleteAll returns the kitty "delete all images" escape.
// Returns empty string for non-kitty terminals.
func DeleteAll() string {
	if detectProtocol() == protoKitty {
		return "\x1b_Ga=d\x1b\\"
	}
	return ""
}

// ── data download ─────────────────────────────────────────────────────────────

// Download fetches image data from imageURL. Exported so callers can cache it.
func Download(imageURL string) ([]byte, error) {
	if imageURL == "" {
		return nil, fmt.Errorf("empty URL")
	}
	return download(imageURL)
}

// ── Render (for kitty and symbols — embedded in View) ────────────────────────

// Render downloads imageURL and returns lines for embedding in View().
//
// Kitty: returns rows strings with APC escape in [0] + cursor-up/right prefixes.
// Symbols: returns plain UTF-8 chafa block art.
// Sixel: returns nil — use DrawSixelAt() instead.
func Render(imageURL string, cols, rows int) []string {
	if imageURL == "" {
		return nil
	}
	data, err := download(imageURL)
	if err != nil || len(data) == 0 {
		return nil
	}
	switch detectProtocol() {
	case protoKitty:
		return renderKitty(data, cols, rows)
	case protoSixel:
		// Sixel cannot be embedded in View() — use DrawSixelAt() instead.
		return nil
	default:
		return renderChafaSymbols(data, cols, rows)
	}
}

// RenderData is like Render but accepts already-downloaded image data.
func RenderData(data []byte, cols, rows int) []string {
	if len(data) == 0 {
		return nil
	}
	switch detectProtocol() {
	case protoKitty:
		return renderKitty(data, cols, rows)
	case protoSixel:
		return nil
	default:
		return renderChafaSymbols(data, cols, rows)
	}
}

// ── DrawSixelAt: direct stdout positioning (sixel only) ──────────────────────

// DrawSixelAt writes a sixel image directly to os.Stdout at terminal position
// (row, col) using ANSI cursor addressing. row and col are 1-based.
// This bypasses BubbleTea's renderer entirely — safe to call from a tea.Cmd.
// No-op if the terminal is not sixel.
func DrawSixelAt(data []byte, cols, rows, termRow, termCol int) error {
	if detectProtocol() != protoSixel {
		return nil
	}

	chafaPath, isReal := findChafa()
	if chafaPath == "" {
		return fmt.Errorf("chafa not found")
	}

	tmp, err := os.CreateTemp("", "twtv-avatar-*.jpg")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	size := fmt.Sprintf("%dx%d", cols, rows)
	var args []string
	if isReal {
		args = []string{
			"--format", "sixels",
			"--size", size,
			"--stretch",
			"--font-ratio=1/2",
			tmpPath,
		}
	} else {
		args = []string{"--size", size, "--stretch", tmpPath}
	}

	sixelData, err := exec.Command(chafaPath, args...).Output()
	os.Remove(tmpPath)
	if err != nil || len(sixelData) == 0 {
		return fmt.Errorf("chafa failed: %w", err)
	}

	// chafa prepends its own escape sequences before the DCS sixel block:
	// \x1b[?25l\x1b[?80l\x1b[?8452l (cursor hide + mode resets).
	// These interfere with our cursor positioning, so we strip everything
	// before the DCS introducer \x1bP and append our own cursor control.
	dcsStart := bytes.Index(sixelData, []byte("\x1bP"))
	if dcsStart < 0 {
		// No DCS found — try without stripping
		dcsStart = 0
	}
	cleanSixel := sixelData[dcsStart:]

	// Write to /dev/tty directly — BubbleTea may intercept os.Stdout.
	tty, ttyErr := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	w := os.Stdout
	if ttyErr == nil {
		w = tty
		defer tty.Close()
	}
	// Sequence:
	//   \x1b[?25l        hide cursor
	//   \x1b[row;colH    move to avatar position
	//   cleanSixel        draw image (DCS...ST)
	//   \x1b[999;1H      push cursor to bottom so BubbleTea redraws correctly
	//   \x1b[?25h        show cursor
	_, err = fmt.Fprintf(w,
		"\x1b[?25l\x1b[%d;%dH%s\x1b[999;1H\x1b[?25h",
		termRow, termCol, string(cleanSixel),
	)
	return err
}

// DrawKittyAt writes a kitty graphics image directly to os.Stdout at terminal
// position (termRow, termCol). Used for kitty direct-draw on redraw events.
func DrawKittyAt(data []byte, cols, rows, termRow, termCol int) error {
	if detectProtocol() != protoKitty {
		return nil
	}
	lines := renderKitty(data, cols, rows)
	if len(lines) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(os.Stdout,
		"\x1b[?25l\x1b[%d;%dH%s\x1b[999;1H\x1b[?25h",
		termRow, termCol, lines[0],
	)
	return err
}

// ── kitty graphics protocol ───────────────────────────────────────────────────

func renderKitty(data []byte, cols, rows int) []string {
	const chunkSize = 4096

	b64 := base64.StdEncoding.EncodeToString(data)
	chunks := splitString(b64, chunkSize)

	var sb strings.Builder
	for i, chunk := range chunks {
		more := 1
		if i == len(chunks)-1 {
			more = 0
		}
		var payload string
		if i == 0 {
			payload = fmt.Sprintf(
				"\x1b_Ga=T,f=100,t=d,q=2,c=%d,r=%d,m=%d;%s\x1b\\",
				cols, rows, more, chunk,
			)
		} else {
			payload = fmt.Sprintf("\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
		sb.WriteString(payload)
	}

	skip := fmt.Sprintf("\x1b[%dC", cols+avatarGap)
	up := fmt.Sprintf("\x1b[%dA", rows)

	result := make([]string, rows)
	result[0] = sb.String() + up + skip
	for i := 1; i < rows; i++ {
		result[i] = skip
	}
	return result
}

// ── chafa symbols renderer ────────────────────────────────────────────────────

func renderChafaSymbols(data []byte, cols, rows int) []string {
	chafaPath, isReal := findChafa()
	if chafaPath == "" {
		return nil
	}

	tmp, err := os.CreateTemp("", "twtv-avatar-*.jpg")
	if err != nil {
		return nil
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return nil
	}
	tmp.Close()

	size := fmt.Sprintf("%dx%d", cols, rows)
	var args []string
	if isReal {
		args = []string{"--format", "symbols", "--size", size, "--stretch", tmpPath}
	} else {
		args = []string{"--size", size, "--stretch", tmpPath}
	}

	out, err := exec.Command(chafaPath, args...).Output()
	os.Remove(tmpPath)
	if err != nil {
		return nil
	}

	raw := strings.TrimRight(string(out), "\n")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

// ── http helper ───────────────────────────────────────────────────────────────

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// ── string helpers ────────────────────────────────────────────────────────────

func splitString(s string, size int) []string {
	var chunks []string
	for len(s) > size {
		chunks = append(chunks, s[:size])
		s = s[size:]
	}
	if s != "" {
		chunks = append(chunks, s)
	}
	return chunks
}
