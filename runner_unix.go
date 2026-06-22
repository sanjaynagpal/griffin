//go:build !windows

package main

import (
	"context"
	"os/exec"
)

// buildRunCmd constructs the OS command for a run script invocation on Unix.
// Returns nil when no unix run script is configured.
func buildRunCmd(ctx context.Context, entry ServiceEntry, arg string) *exec.Cmd {
	if entry.RunUnix == "" {
		return nil
	}
	return exec.CommandContext(ctx, "bash", entry.RunUnix, arg)
}
