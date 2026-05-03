package player

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Launch starts mpv with the given URL and optional extra arguments.
// When quiet is true, mpv's stdout/stderr are discarded and the status
// line is suppressed. The process is detached so it outlives the TUI.
func Launch(url string, extra []string, quiet bool) error {
	args := []string{url}
	args = append(args, extra...)
	if quiet {
		args = append(args, "--msg-level=statusline=no")
	}

	cmd := exec.Command("mpv", args...)
	cmd.SysProcAttr = detachAttr()
	cmd.Stdin = nil

	if quiet {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mpv start failed: %w", err)
	}
	return cmd.Process.Release()
}
