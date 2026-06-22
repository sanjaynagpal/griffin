//go:build !windows

package main

import "syscall"

// IsAlive reports whether the process with the given PID is alive.
// Signal 0 checks existence without delivering a real signal.
// EPERM means the process exists but we lack permission to signal it.
func IsAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
