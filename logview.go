package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// logView is the state of the live log viewer for a single service.
type logView struct {
	entry      ServiceEntry
	stream     string // "stdout" | "stderr"
	lines      []string
	offset     int   // index of the top visible line when not following
	follow     bool  // auto-scroll to the newest line
	byteOffset int64 // file position for incremental tailing
	exists     bool  // false when the active log file is missing
}

func newLogView(entry ServiceEntry) logView {
	return logView{entry: entry, stream: "stdout", follow: true, exists: true}
}

// logPath returns the on-disk path of a service's stdout/stderr log.
func logPath(entry ServiceEntry, stream string) string {
	return filepath.Join(entry.ComponentRoot, "logs", stream+".log")
}

// readLogChunk reads from offset to EOF and returns the complete lines found,
// the new byte offset (advanced only past the last newline so a partial
// trailing line is re-read next tick), and whether the file exists. A file that
// has shrunk since the last read is treated as rotated and re-read from start.
func readLogChunk(path string, offset int64) (lines []string, newOffset int64, exists bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, false
	}
	defer f.Close()

	if info, err := f.Stat(); err == nil && info.Size() < offset {
		offset = 0 // rotated/truncated — start over
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, true
	}
	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return nil, offset, true
	}
	idx := bytes.LastIndexByte(data, '\n')
	if idx < 0 {
		return nil, offset, true // no complete line yet
	}
	chunk := data[:idx+1]
	newOffset = offset + int64(len(chunk))
	text := strings.TrimSuffix(string(chunk), "\n")
	return strings.Split(text, "\n"), newOffset, true
}

// View renders the log viewer, clipped to the terminal height. Output is
// header + separator + content + separator + legend; the caller's fitToFrame
// pads any slack so the block stays a fixed size for Bubble Tea's diff renderer.
func (lv logView) View(width, height int) string {
	var b strings.Builder

	// --- Header --------------------------------------------------------------
	streamTab := func(name string) string {
		if name == lv.stream {
			return styleBold.Render("[" + name + "]")
		}
		return styleDim.Render(" " + name + " ")
	}
	mode := "paused"
	if lv.follow {
		mode = "following"
	}
	b.WriteString(styleBold.Render(lv.entry.DisplayName))
	b.WriteString("  ")
	b.WriteString(streamTab("stdout"))
	b.WriteString(" ")
	b.WriteString(streamTab("stderr"))
	b.WriteString(styleDim.Render(fmt.Sprintf("  ·  %s  ·  %d lines", mode, len(lv.lines))))
	b.WriteString("\n")
	b.WriteString(styleDim.Render(strings.Repeat("─", max(width-2, 20))))
	b.WriteString("\n")

	// Rows available for log content (reserve header, two separators, legend).
	rows := height - 4
	if height <= 0 {
		rows = 20
	}
	if rows < 1 {
		rows = 1
	}

	if !lv.exists {
		b.WriteString("\n  ")
		b.WriteString(styleWarn.Render("log file not found:"))
		b.WriteString("\n  ")
		b.WriteString(styleDim.Render(logPath(lv.entry, lv.stream)))
		b.WriteString("\n  ")
		b.WriteString(styleDim.Render("waiting for it to appear…"))
		b.WriteString("\n")
	} else if len(lv.lines) == 0 {
		b.WriteString("  ")
		b.WriteString(styleDim.Render("(no log output yet)"))
		b.WriteString("\n")
	} else {
		n := len(lv.lines)
		start := lv.offset
		if lv.follow {
			start = n - rows
		}
		if start > n-rows {
			start = n - rows
		}
		if start < 0 {
			start = 0
		}
		end := start + rows
		if end > n {
			end = n
		}
		for _, ln := range lv.lines[start:end] {
			b.WriteString("  ")
			b.WriteString(clipLine(ln, max(width-2, 10)))
			b.WriteString("\n")
		}
	}

	// --- Legend --------------------------------------------------------------
	b.WriteString(styleDim.Render(strings.Repeat("─", max(width-2, 20))))
	b.WriteString("\n")
	b.WriteString(styleDim.Render("  ↑/↓  scroll    f  follow    tab  switch stream    b/Esc  back"))
	b.WriteString("\n")

	return b.String()
}

// clipLine truncates s to at most w runes (with an ellipsis) so long log lines
// don't wrap and disturb the fixed-height layout.
func clipLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}
