//go:build windows

package main

import (
	"context"
	"os/exec"
)

// buildRunCmd constructs the OS command for a run script invocation on Windows.
// Returns nil when no windows run script is configured.
func buildRunCmd(ctx context.Context, entry ServiceEntry, arg string) *exec.Cmd {
	if entry.RunWindows == "" {
		return nil
	}
	return exec.CommandContext(ctx, "pwsh", "-NonInteractive", "-File", entry.RunWindows, arg)
}
