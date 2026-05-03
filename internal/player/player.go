package player

import (
	"os"
	"os/exec"
)

// Launch starts mpv detached, streaming its log to stdout.
func Launch(url string, extraArgs []string, quiet bool) error {
	args := append(extraArgs, url)
	cmd := exec.Command("mpv", args...)
	cmd.SysProcAttr = sysProcAttr() // platform-specific setsid equivalent
	if quiet {
		cmd.Stdout = nil
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.NewFile(0, os.DevNull)
	return cmd.Start()
}
