//go:build !linux

package player

import "syscall"

// detachAttr returns nil on non-Linux platforms; mpv will still start
// but will not be placed into a new session.
func detachAttr() *syscall.SysProcAttr {
	return nil
}
