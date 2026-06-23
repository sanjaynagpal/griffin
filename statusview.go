package main

import (
	"fmt"
	"strings"
	"time"
)

// StatusView renders the top-level service table.
type StatusView struct {
	statuses []ServiceStatus
	metrics  map[string]ProcessMetrics
	cursor   int
}

func newStatusView(statuses []ServiceStatus, metrics map[string]ProcessMetrics) StatusView {
	return StatusView{statuses: statuses, metrics: metrics}
}

// View renders the status table plus a detail strip for the selected service.
func (v StatusView) View(width int, sel selectionStyle) string {
	if len(v.statuses) == 0 {
		return "\n  No services found. Run 'griffin init' first.\n"
	}

	// Determine service name column width from data.
	nameW := len("Service")
	for _, s := range v.statuses {
		if n := len(s.Entry.DisplayName); n > nameW {
			nameW = n
		}
	}

	running := 0
	for _, s := range v.statuses {
		if s.State == "RUNNING" {
			running++
		}
	}

	var b strings.Builder

	// Title bar.
	b.WriteString("\n")
	b.WriteString(styleBold.Render("  Griffin"))
	b.WriteString(styleDim.Render(fmt.Sprintf("  —  %d running, %d stopped",
		running, len(v.statuses)-running)))
	b.WriteString("\n\n")

	// Column headers.
	// Widths: PID 7 · Port 5 · CPU% 7 · Memory 9 · Uptime 9 · Dir 9
	// All metric columns are right-aligned (positive width in Sprintf). A
	// one-column pointer gutter brackets the row on each side.
	header := fmt.Sprintf("    %-*s  %-10s  %7s  %5s  %7s  %9s  %9s  %9s",
		nameW, "Service", "Status", "PID", "Port", "CPU%", "Memory", "Uptime", "Dir")
	b.WriteString(styleDim.Render(header))
	b.WriteByte('\n')

	sep := "    " + strings.Repeat("─", nameW) + "  " +
		strings.Repeat("─", 10) + "  " +
		strings.Repeat("─", 7) + "  " +
		strings.Repeat("─", 5) + "  " +
		strings.Repeat("─", 7) + "  " +
		strings.Repeat("─", 9) + "  " +
		strings.Repeat("─", 9) + "  " +
		strings.Repeat("─", 9)
	b.WriteString(styleDim.Render(sep))
	b.WriteByte('\n')

	// One row per service. The selected row is marked with coloured pointers in
	// the left and right gutters and a bold name — no inverse background, so the
	// RUNNING/STOPPED colour stays visible.
	for i, s := range v.statuses {
		m := v.metrics[s.Entry.Name]

		pid := "—"
		if s.PID > 0 {
			pid = fmt.Sprintf("%d", s.PID)
		}
		port := "—"
		if s.Entry.Port != "" {
			port = s.Entry.Port
		}

		// Metrics columns — "—" for stopped services or first sample.
		cpu, rss, uptime := "—", "—", "—"
		if m.Available {
			if m.CPUPercent >= 0 {
				cpu = fmt.Sprintf("%.1f%%", m.CPUPercent)
			}
			if m.RSS > 0 {
				rss = formatBytes(m.RSS)
			}
			if m.Uptime > 0 {
				uptime = formatDuration(m.Uptime)
			}
		}
		// Dir is collected for every service regardless of state.
		dir := "—"
		if m.DirBytes > 0 {
			dir = formatBytes(m.DirBytes)
		}

		selected := i == v.cursor

		name := fmt.Sprintf("%-*s", nameW, s.Entry.DisplayName)
		if selected {
			name = styleBold.Render(name)
		}

		// Colour the state cell (kept regardless of selection).
		stateStyle := styleStopped
		if s.State == "RUNNING" {
			stateStyle = styleRunning
		}
		stateStr := stateStyle.Render(fmt.Sprintf("%-10s", s.State))

		// Selection gutters (pointer > < or solid sidebar bar) bracket the row.
		left, right := sel.gutters(selected)

		line := fmt.Sprintf(" %s %s  %s  %7s  %5s  %7s  %9s  %9s  %9s %s",
			left, name, stateStr, pid, port, cpu, rss, uptime, dir, right)

		b.WriteString(line)
		b.WriteByte('\n')
	}

	// Detail strip for selected service.
	b.WriteString("\n")
	b.WriteString(v.renderDetailStrip())
	b.WriteString("\n\n")

	// Key hints (two lines).
	b.WriteString(styleDim.Render(
		"  ↑/↓ nav    s start    K stop    R restart    a/A bulk all    r refresh",
	))
	b.WriteByte('\n')
	b.WriteString(styleDim.Render(
		"  i info    l logs    m metrics    g gutter style    t theme    q quit",
	))
	b.WriteByte('\n')

	_ = width
	return b.String()
}

// renderDetailStrip returns a one-line runtime summary for the selected service.
func (v StatusView) renderDetailStrip() string {
	if len(v.statuses) == 0 || v.cursor >= len(v.statuses) {
		return ""
	}
	s := v.statuses[v.cursor]
	m := v.metrics[s.Entry.Name]

	var parts []string

	if s.Entry.Port != "" {
		parts = append(parts, ":"+s.Entry.Port)
	}

	if m.ConnCount >= 0 {
		conn := fmt.Sprintf("%d conns", m.ConnCount)
		if m.Established > 0 {
			conn += fmt.Sprintf(" (%d estab)", m.Established)
		}
		parts = append(parts, conn)
	}

	// Listening ports not already shown as the registered service port.
	if len(m.Listening) > 0 {
		var extra []string
		for _, p := range m.Listening {
			ps := fmt.Sprintf("%d", p)
			if ps != s.Entry.Port {
				extra = append(extra, ps)
			}
		}
		if len(extra) > 0 {
			parts = append(parts, "listen: "+strings.Join(extra, ", "))
		}
	}

	if m.DirBytes > 0 {
		parts = append(parts, "dir "+formatBytes(m.DirBytes))
	}

	if m.VolumeFree > 0 {
		parts = append(parts, "free "+formatBytes(m.VolumeFree))
	}

	if m.TLSProbed {
		if m.TLSErr != "" {
			parts = append(parts, "TLS: err")
		} else if !m.TLSExpiry.IsZero() {
			days := int(time.Until(m.TLSExpiry).Hours() / 24)
			if days < 0 {
				parts = append(parts, "TLS: EXPIRED")
			} else if days <= 14 {
				parts = append(parts, fmt.Sprintf("TLS: %dd ⚠", days))
			} else {
				parts = append(parts, fmt.Sprintf("TLS: %dd", days))
			}
		}
	}

	var b strings.Builder
	b.WriteString(styleBold.Render("  " + s.Entry.DisplayName))
	if len(parts) > 0 {
		b.WriteString(styleDim.Render("  ·  " + strings.Join(parts, "  ·  ")))
	}
	return b.String()
}
