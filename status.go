package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ServiceStatus is the runtime state of one service at a point in time.
type ServiceStatus struct {
	Entry ServiceEntry
	State string // "RUNNING" or "STOPPED"
	PID   int    // 0 when stopped
}

// ResolvePIDFile returns the PID file path for entry. The registry field
// takes precedence. Otherwise the component root is scanned: <name>.pid
// is checked first, then any *.pid file (first alphabetically).
// Returns "" when no file can be found.
func ResolvePIDFile(entry ServiceEntry) string {
	if entry.PIDFile != "" {
		return entry.PIDFile
	}
	// Prefer <service-name>.pid.
	named := filepath.Join(entry.ComponentRoot, entry.Name+".pid")
	if _, err := os.Stat(named); err == nil {
		return named
	}
	// Fall back to any *.pid in the component root.
	matches, _ := filepath.Glob(filepath.Join(entry.ComponentRoot, "*.pid"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// ReadPID reads and parses an integer PID from path.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading pid file %q: %w", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parsing pid in %q: %w", path, err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid %d in %q", pid, path)
	}
	return pid, nil
}

// CheckStatus determines the current state of a single service.
func CheckStatus(entry ServiceEntry) ServiceStatus {
	pidFile := ResolvePIDFile(entry)
	if pidFile == "" {
		return ServiceStatus{Entry: entry, State: "STOPPED"}
	}
	pid, err := ReadPID(pidFile)
	if err != nil {
		return ServiceStatus{Entry: entry, State: "STOPPED"}
	}
	if !IsAlive(pid) {
		return ServiceStatus{Entry: entry, State: "STOPPED"}
	}
	return ServiceStatus{Entry: entry, State: "RUNNING", PID: pid}
}

// RefreshAll checks every service and returns results in the same order as
// the input slice.
func RefreshAll(entries []ServiceEntry) []ServiceStatus {
	out := make([]ServiceStatus, len(entries))
	for i, e := range entries {
		out[i] = CheckStatus(e)
	}
	return out
}
