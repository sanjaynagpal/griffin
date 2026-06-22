//go:build !windows

package main

import "syscall"

// volumeFreeBytes returns the number of bytes available to the current user
// on the volume that contains path. Returns 0 on error.
func volumeFreeBytes(path string) uint64 {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return 0
	}
	// Bavail is the blocks available to unprivileged users.
	// Bsize is the block size (int64 on Linux, int32 on Darwin — both safe to cast).
	return s.Bavail * uint64(s.Bsize)
}
