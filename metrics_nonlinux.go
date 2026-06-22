//go:build !linux

package main

// readProcStatus returns zeros on non-Linux platforms where /proc is not available.
func readProcStatus(_ int) (peakRSS, swap uint64) {
	return 0, 0
}

// readMemLimit returns 0 on non-Linux platforms (cgroup not available).
func readMemLimit(_ int) uint64 {
	return 0
}
