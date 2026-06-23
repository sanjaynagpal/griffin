package main

import (
	"fmt"
	"strings"
)

// statusGlyph returns a coloured dot for the service state.
func statusGlyph(state string) string {
	if state == "RUNNING" {
		return styleRunning.Render("●")
	}
	return styleStopped.Render("○")
}

// compactBytes formats a byte count tersely for the narrow sidebar (e.g. 512M,
// 1.2G, 999K).
func compactBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fK", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// renderServiceList draws the always-visible left panel: one compact line per
// service with a status glyph, name, CPU%, Memory and Dir. The selected row
// gets a coloured marker and a bold name.
func renderServiceList(statuses []ServiceStatus, metrics map[string]ProcessMetrics,
	cursor int, sel selectionStyle, width int) string {

	var b strings.Builder
	running := countInState(statuses, "RUNNING")
	b.WriteString(styleHeading.Render("  Services"))
	b.WriteString(styleDim.Render(fmt.Sprintf("   %d up · %d down", running, len(statuses)-running)))
	b.WriteString("\n\n")

	if len(statuses) == 0 {
		b.WriteString(styleDim.Render("  (no services — run 'griffin init')"))
		b.WriteString("\n")
		return b.String()
	}

	// marker(1) space(1) glyph(1) space(1) name space cpu(5) space mem(5) space
	// dir(5), plus a trailing gap before the panel separator.
	const (
		metricsW = 1 + 1 + 1 + 1 + 1 + 5 + 1 + 5 + 1 + 5
		rightGap = 2
	)
	nameW := width - metricsW - rightGap
	if nameW < 6 {
		nameW = 6
	}

	for i, s := range statuses {
		m := metrics[s.Entry.Name]
		cpu, mem, dir := "—", "—", "—"
		if s.State == "RUNNING" && m.Available {
			if m.CPUPercent >= 0 {
				cpu = fmt.Sprintf("%.1f%%", m.CPUPercent)
			}
			if m.RSS > 0 {
				mem = compactBytes(m.RSS)
			}
		}
		if m.DirBytes > 0 {
			dir = compactBytes(m.DirBytes)
		}

		selected := i == cursor
		marker := " "
		if selected {
			glyph := "▶" // bigger right arrow than ▸
			if sel == selSidebar {
				glyph = "▌" // half-width sidebar bar instead of full █
			}
			marker = stylePointer.Render(glyph)
		}
		name := rightPad(s.Entry.DisplayName, nameW)
		if selected {
			name = styleBold.Render(name)
		}

		b.WriteString(marker)
		b.WriteString(" ") // gap between selection gutter and status glyph
		b.WriteString(statusGlyph(s.State))
		b.WriteString(" ")
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(styleDim.Render(fmt.Sprintf("%5s", cpu)))
		b.WriteString(" ")
		b.WriteString(styleDim.Render(fmt.Sprintf("%5s", mem)))
		b.WriteString(" ")
		b.WriteString(styleDim.Render(fmt.Sprintf("%5s", dir)))
		// Trailing rightGap columns are left for lipgloss to pad, creating space
		// before the panel separator.
		b.WriteByte('\n')
	}
	return b.String()
}
