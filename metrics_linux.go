//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// readProcStatus reads peak RSS and swap usage from /proc/<pid>/status.
// Returns zeros on any read or parse failure.
func readProcStatus(pid int) (peakRSS, swap uint64) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		switch {
		case strings.HasPrefix(line, "VmPeak:"):
			peakRSS = parseStatusKB(line)
		case strings.HasPrefix(line, "VmSwap:"):
			swap = parseStatusKB(line)
		}
	}
	return peakRSS, swap
}

// parseStatusKB parses a line of the form "VmPeak:  1234 kB" and returns bytes.
func parseStatusKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	n, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return n * 1024 // kB → bytes
}

// readMemLimit returns the cgroup memory limit for pid in bytes, or 0 if
// unlimited or unknown. Supports both cgroup v1 and v2.
func readMemLimit(pid int) uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return 0
	}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		cgPath := strings.TrimSpace(parts[2])

		// cgroup v2: hierarchy id == "0", controllers field is empty.
		if parts[0] == "0" && parts[1] == "" {
			limitFile := filepath.Join("/sys/fs/cgroup", cgPath, "memory.max")
			if b, err := os.ReadFile(limitFile); err == nil {
				s := strings.TrimSpace(string(b))
				if s == "max" {
					return 0 // explicitly unlimited
				}
				if v, err := strconv.ParseUint(s, 10, 64); err == nil {
					return v
				}
			}
		}

		// cgroup v1: look for the memory subsystem.
		if parts[1] == "memory" {
			limitFile := filepath.Join("/sys/fs/cgroup/memory", cgPath, "memory.limit_in_bytes")
			if b, err := os.ReadFile(limitFile); err == nil {
				v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
				if err != nil {
					continue
				}
				// Sentinel: kernel sets this to (max_int64 rounded down to page)
				// when no limit is configured. Treat anything ≥ 1 PiB as unlimited.
				const onePiB = uint64(1) << 50
				if v >= onePiB {
					return 0
				}
				return v
			}
		}
	}
	return 0
}
