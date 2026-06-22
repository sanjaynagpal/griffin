package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// RunResult holds the outcome of invoking a service run command.
type RunResult struct {
	Stdout   string
	ExitCode int
	Err      error // non-nil only for OS-level errors (spawn failed, timeout)
}

// Invoke runs the service's run script with the given argument (START/STOP/STATUS).
// stdout and stderr are captured together. Platform-specific command construction
// is in runner_unix.go / runner_windows.go.
func Invoke(entry ServiceEntry, arg string, timeoutSecs int) RunResult {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(timeoutSecs)*time.Second,
	)
	defer cancel()

	cmd := buildRunCmd(ctx, entry, arg)
	if cmd == nil {
		return RunResult{Err: fmt.Errorf("no run script configured for this platform")}
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Script ran but exited non-zero — not a Go error.
			return RunResult{Stdout: out.String(), ExitCode: exitErr.ExitCode()}
		}
		return RunResult{Stdout: out.String(), ExitCode: -1, Err: err}
	}
	return RunResult{Stdout: out.String(), ExitCode: 0}
}

// StartService invokes START and polls briefly until a live PID file appears.
// The script daemonises the process and writes the PID file before returning,
// so in the happy path the file appears within a second or two.
func StartService(entry ServiceEntry) error {
	r := Invoke(entry, "START", 10)
	if r.Err != nil {
		return fmt.Errorf("start failed: %w", r.Err)
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("start exited %d: %s", r.ExitCode, r.Stdout)
	}

	// Short confirmation poll — give the background process up to 5 s to
	// write its PID file and appear in the process table.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pidFile := ResolvePIDFile(entry); pidFile != "" {
			if pid, err := ReadPID(pidFile); err == nil && IsAlive(pid) {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("start timed out: no live process within 5s")
}

// StopService invokes STOP and trusts the script's own exit code.
// The run scripts already implement their own wait-for-death loop, so
// there is no need to poll from Go — doing so risks false positives from
// PID reuse (Windows recycles PIDs very quickly).
func StopService(entry ServiceEntry) error {
	r := Invoke(entry, "STOP", 15)
	if r.Err != nil {
		return fmt.Errorf("stop failed: %w", r.Err)
	}
	if r.ExitCode != 0 {
		return fmt.Errorf("stop exited %d: %s", r.ExitCode, r.Stdout)
	}
	return nil
}

// RestartService stops then starts a service. Abort if stop fails.
func RestartService(entry ServiceEntry) error {
	if err := StopService(entry); err != nil {
		return fmt.Errorf("restart abort — stop: %w", err)
	}
	if err := StartService(entry); err != nil {
		return fmt.Errorf("restart abort — start: %w", err)
	}
	return nil
}
