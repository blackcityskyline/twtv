package terminal

import (
	"fmt"
	"os/exec"
)

// knownTerminals is the search order for auto-detection.
var knownTerminals = []string{
	"alacritty", "kitty", "wezterm", "foot", "xterm", "urxvt", "st",
}

// Detect returns the path to the first terminal emulator found in PATH,
// or an empty string if none is found.
func Detect() string {
	for _, name := range knownTerminals {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// Launch starts args in the given terminal emulator and detaches it.
func Launch(term string, args ...string) error {
	cmd := exec.Command(term, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch terminal %q: %w", term, err)
	}
	return cmd.Process.Release()
}
