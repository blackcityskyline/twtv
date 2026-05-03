package player

import (
	"os/exec"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// LaunchChat opens a TUI chat app in a new terminal window.
func LaunchChat(termPath, chatCmd, channel string) error {
	cmd := exec.Command(termPath, "-e", chatCmd, "-c", channel)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
