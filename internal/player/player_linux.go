//go:build linux

package player

import "syscall"

// detachAttr returns platform-specific process attributes that detach
// the child from the controlling terminal, creating a new session.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
