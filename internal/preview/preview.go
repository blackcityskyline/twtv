// Package preview fetches and renders a Twitch stream thumbnail in the
// terminal on demand. Rendering uses chafa(1) which supports all major
// terminal graphics protocols (kitty, sixel, iTerm2, and block fallback).
// Nothing is cached — every call downloads and renders fresh.
package preview

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	thumbWidth  = 1280
	thumbHeight = 720
	httpTimeout = 8 * time.Second
)

// thumbURL builds the Twitch CDN URL for a live stream thumbnail.
func thumbURL(login string) string {
	return fmt.Sprintf(
		"https://static-cdn.jtvnw.net/previews-ttv/live_user_%s-%dx%d.jpg",
		strings.ToLower(login), thumbWidth, thumbHeight,
	)
}

// Show downloads the thumbnail for login and renders it into the terminal
// using chafa. cols and rows are the desired character-cell dimensions.
// Returns an error if chafa is not installed or the download fails.
func Show(login string, cols, rows int) error {
	if _, err := exec.LookPath("chafa"); err != nil {
		return fmt.Errorf("chafa not found — install chafa to enable previews")
	}

	// Download to a temp file so chafa can seek it.
	tmp, err := os.CreateTemp("", "twtv-preview-*.jpg")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if err := download(thumbURL(login), tmp); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return render(tmp.Name(), cols, rows)
}

// download fetches url and writes the body into dst.
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

// render calls chafa to print the image at path to stdout.
func render(path string, cols, rows int) error {
	cmd := exec.Command("chafa",
		"--size", strconv.Itoa(cols)+"x"+strconv.Itoa(rows),
		"--stretch",
		path,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
