// Package preview fetches and renders thumbnails on demand using chafa(1).
// Supports live stream thumbnails (by login) and arbitrary URLs (for VODs/clips).
//
// tea.ExecProcess fully suspends the TUI, hands the terminal to the process,
// then restores the TUI when the process exits.
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
	thumbWidth  = 1280
	thumbHeight = 720
	httpTimeout = 8 * time.Second
)

// LiveThumbURL returns the Twitch CDN URL for a live stream thumbnail.
func LiveThumbURL(login string) string {
	return fmt.Sprintf(
		"https://static-cdn.jtvnw.net/previews-ttv/live_user_%s-%dx%d.jpg",
		strings.ToLower(login), thumbWidth, thumbHeight,
	)
}

// ExpandThumbURL replaces {width}/{height} placeholders in a Twitch thumbnail
// URL template (used for VOD and clip thumbnail URLs from the API).
func ExpandThumbURL(tmpl string, w, h int) string {
	s := strings.ReplaceAll(tmpl, "{width}", fmt.Sprintf("%d", w))
	s = strings.ReplaceAll(s, "{height}", fmt.Sprintf("%d", h))
	return s
}

// Command downloads the thumbnail for a live stream (by login) and returns
// an exec.Cmd that renders it with chafa at the given character-cell size.
func Command(login string, cols, rows int) (*exec.Cmd, error) {
	return CommandFromURL(LiveThumbURL(login), cols, rows)
}

// CommandFromURL downloads an arbitrary thumbnail URL and returns an exec.Cmd
// that renders it with chafa at the given size.
// Use this for VOD and clip thumbnails.
func CommandFromURL(thumbURLStr string, cols, rows int) (*exec.Cmd, error) {
	if _, err := exec.LookPath("chafa"); err != nil {
		return nil, fmt.Errorf("chafa not installed — run: pacman -S chafa  or  apt install chafa")
	}

	// Expand any remaining {width}/{height} placeholders.
	thumbURLStr = ExpandThumbURL(thumbURLStr, thumbWidth, thumbHeight)

	tmp, err := os.CreateTemp("", "twtv-preview-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("temp file: %w", err)
	}

	if err := download(thumbURLStr, tmp); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, fmt.Errorf("download thumbnail: %w", err)
	}
	tmp.Close()

	tmpPath := tmp.Name()
	size := fmt.Sprintf("%dx%d", cols, rows)
	script := fmt.Sprintf(
		`clear; chafa --size %s --fit-width %q; printf '\n\e[7m press any key to close \e[0m\n'; read -n1 -s; rm -f %q`,
		size, tmpPath, tmpPath,
	)
	return exec.Command("sh", "-c", script), nil
}

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
