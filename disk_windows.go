//go:build windows

package main

import "golang.org/x/sys/windows"

// volumeFreeBytes returns the number of bytes available to the current user
// on the volume that contains path. Returns 0 on error.
func volumeFreeBytes(path string) uint64 {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &avail, &total, &free); err != nil {
		return 0
	}
	return avail
}
