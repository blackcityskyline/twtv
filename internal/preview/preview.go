// Package preview fetches and renders a Twitch stream thumbnail on demand.
// Rendering uses chafa(1) which supports kitty, sixel, iTerm2, and block
// character fallback. Nothing is cached — every call downloads fresh.
//
// Usage pattern with BubbleTea:
//
//	cmd, err := preview.Command(login, cols, rows)
//	if err != nil { ... }
//	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
//	    return previewDoneMsg{err: err}
//	})
//
// tea.ExecProcess fully suspends the TUI, hands the terminal to the process,
// then restores the TUI when the process exits. This avoids all rendering
// conflicts between chafa and BubbleTea.
package preview

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// Twitch CDN thumbnail dimensions. 1280×720 gives good quality
	// without being excessively large to download (~50–150 KB).
	thumbWidth  = 1280
	thumbHeight = 720

	httpTimeout = 8 * time.Second
)

// thumbURL builds the Twitch CDN URL for a live stream thumbnail.
// login must be a lowercase channel name.
func thumbURL(login string) string {
	return fmt.Sprintf(
		"https://static-cdn.jtvnw.net/previews-ttv/live_user_%s-%dx%d.jpg",
		strings.ToLower(login), thumbWidth, thumbHeight,
	)
}

// Command downloads the thumbnail for login into a temp file and returns an
// exec.Cmd that will render it with chafa at the given character-cell size.
// The temp file is removed when the command exits via a wrapper script.
//
// cols and rows should be the terminal dimensions you want the image to fill.
// The image is rendered with --fit-width to preserve aspect ratio; it will
// never exceed cols×rows but may be shorter vertically.
//
// Returns an error immediately if chafa is not installed or the download fails.
func Command(login string, cols, rows int) (*exec.Cmd, error) {
	if _, err := exec.LookPath("chafa"); err != nil {
		return nil, fmt.Errorf("chafa not installed — run: pacman -S chafa  or  apt install chafa")
	}

	tmp, err := os.CreateTemp("", "twtv-preview-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("temp file: %w", err)
	}

	if err := download(thumbURL(login), tmp); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, fmt.Errorf("download thumbnail: %w", err)
	}
	tmp.Close()

	tmpPath := tmp.Name()

	// Wrap chafa in a shell one-liner so we can:
	//   1. clear the screen before rendering
	//   2. remove the temp file afterwards
	//   3. pause with "press any key" so the image stays visible
	size := fmt.Sprintf("%dx%d", cols, rows)
	script := fmt.Sprintf(
		`clear; chafa --size %s --fit-width %q; printf '\n\e[7m press any key to close \e[0m\n'; read -n1 -s; rm -f %q`,
		size, tmpPath, tmpPath,
	)
	return exec.Command("sh", "-c", script), nil
}

// download fetches url and writes the body to dst.
func download(url string, dst io.Writer) error {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}
