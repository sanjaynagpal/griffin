//go:build windows

package main

import "golang.org/x/sys/windows"

// IsAlive reports whether the process with the given PID is alive on Windows.
// It opens a limited process handle and reads the exit code.
// Exit code 259 (STILL_ACTIVE) means the process is still running.
func IsAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const stillActive = 259
	return code == stillActive
}
