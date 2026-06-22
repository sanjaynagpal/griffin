package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// metricHistoryCap is the number of samples retained per service (5 min at the
// 5-second sampling cadence).
const metricHistoryCap = 60

// metricSample is one timestamped metrics reading.
type metricSample struct {
	m  ProcessMetrics
	at time.Time
}

// MetricHistory is a fixed-capacity ring buffer of recent metric samples.
type MetricHistory struct {
	samples []metricSample
	head    int // index of the next write
	count   int // number of valid samples (<= metricHistoryCap)
}

// Push records a new sample at the current time, overwriting the oldest once
// the buffer is full.
func (h *MetricHistory) Push(m ProcessMetrics) {
	if h.samples == nil {
		h.samples = make([]metricSample, metricHistoryCap)
	}
	h.samples[h.head] = metricSample{m: m, at: time.Now()}
	h.head = (h.head + 1) % metricHistoryCap
	if h.count < metricHistoryCap {
		h.count++
	}
}

// ordered returns the retained samples oldest-first.
func (h *MetricHistory) ordered() []metricSample {
	out := make([]metricSample, 0, h.count)
	if h.count == 0 {
		return out
	}
	start := 0
	if h.count == metricHistoryCap {
		start = h.head // buffer full: oldest sits at head
	}
	for i := 0; i < h.count; i++ {
		out = append(out, h.samples[(start+i)%metricHistoryCap])
	}
	return out
}

// series extracts one float per sample via f, oldest-first.
func (h *MetricHistory) series(f func(ProcessMetrics) float64) []float64 {
	s := h.ordered()
	out := make([]float64, len(s))
	for i, smp := range s {
		out[i] = f(smp.m)
	}
	return out
}

// rssGrowth reports the RSS trend across the window: ↑ growing, ↓ shrinking,
// → stable (within a 1 MB deadband to suppress jitter).
func (h *MetricHistory) rssGrowth() string {
	s := h.ordered()
	if len(s) < 2 {
		return "→"
	}
	delta := float64(s[len(s)-1].m.RSS) - float64(s[0].m.RSS)
	const deadband = 1 << 20 // 1 MB
	switch {
	case delta > deadband:
		return "↑"
	case delta < -deadband:
		return "↓"
	default:
		return "→"
	}
}

// diskRate returns the read/write byte rates (per second) from the two most
// recent samples.
func (h *MetricHistory) diskRate() (readRate, writeRate float64) {
	s := h.ordered()
	if len(s) < 2 {
		return 0, 0
	}
	a, b := s[len(s)-2], s[len(s)-1]
	elapsed := b.at.Sub(a.at).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}
	readRate = math.Max(0, (float64(b.m.DiskRead)-float64(a.m.DiskRead))/elapsed)
	writeRate = math.Max(0, (float64(b.m.DiskWrite)-float64(a.m.DiskWrite))/elapsed)
	return readRate, writeRate
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// Sparkline maps the most recent values to block characters scaled to the local
// min/max. With fewer than width samples the line is right-padded with spaces.
// All-zero, empty, or flat input renders as a baseline (────).
func Sparkline(values []float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	min, max := math.Inf(1), math.Inf(-1)
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	if len(values) == 0 || max <= 0 || max == min {
		return strings.Repeat("─", width)
	}
	span := max - min
	var b strings.Builder
	for _, v := range values {
		idx := int((v - min) / span * float64(len(sparkRunes)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkRunes) {
			idx = len(sparkRunes) - 1
		}
		b.WriteRune(sparkRunes[idx])
	}
	return rightPad(b.String(), width)
}

// rightPad pads s with trailing spaces to exactly width runes.
func rightPad(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}

// renderMetricsPanel draws the metrics dashboard for all services.
// graphMode selects how the focused service graphs are drawn.
type graphMode int

const (
	graphLine graphMode = iota // connected line
	graphArea                  // filled mountain
	graphOff                   // hidden
)

func (g graphMode) String() string {
	switch g {
	case graphArea:
		return "area"
	case graphOff:
		return "off"
	default:
		return "line"
	}
}

func renderMetricsPanel(appRoot string, statuses []ServiceStatus, metrics map[string]ProcessMetrics,
	history map[string]*MetricHistory, cursor int, nextRefresh time.Duration, width int, sel selectionStyle, gmode graphMode) string {

	var b strings.Builder

	// Aggregates for the summary bar.
	running := 0
	var totalCPU float64
	var totalRSS, totalDir uint64
	var totalReadRate, totalWriteRate float64
	for _, s := range statuses {
		m := metrics[s.Entry.Name]
		if s.State == "RUNNING" {
			running++
			if m.CPUPercent > 0 {
				totalCPU += m.CPUPercent
			}
			totalRSS += m.RSS
		}
		totalDir += m.DirBytes
		if h := history[s.Entry.Name]; h != nil {
			rr, wr := h.diskRate()
			totalReadRate += rr
			totalWriteRate += wr
		}
	}
	stopped := len(statuses) - running

	// --- Header --------------------------------------------------------------
	secs := int(nextRefresh.Seconds())
	if secs < 0 {
		secs = 0
	}
	b.WriteString(styleBold.Render("  Metrics"))
	b.WriteString(styleDim.Render(fmt.Sprintf("  —  %s   ·   %d running, %d stopped   ·   next refresh in %ds",
		appRoot, running, stopped, secs)))
	b.WriteString("\n\n")

	// --- Summary bar ---------------------------------------------------------
	b.WriteString(styleDim.Render(fmt.Sprintf(
		"  Σ  CPU %.1f%%    Mem %s    Disk  r %s/s  w %s/s    Dir %s",
		totalCPU, formatBytes(totalRSS),
		formatBytes(uint64(totalReadRate)), formatBytes(uint64(totalWriteRate)),
		formatBytes(totalDir))))
	b.WriteString("\n\n")

	// Service-name column width.
	nameW := len("Service")
	for _, s := range statuses {
		if n := len(s.Entry.DisplayName); n > nameW {
			nameW = n
		}
	}

	// --- Column headers ------------------------------------------------------
	header := "    " + fmt.Sprintf("%-*s", nameW, "Service") +
		"  " + fmt.Sprintf("%-17s", "CPU") +
		"  " + fmt.Sprintf("%-21s", "Memory  (growth)") +
		"  " + fmt.Sprintf("%-17s", "Disk r/w") +
		"  " + fmt.Sprintf("%-7s", "Uptime") +
		"  " + fmt.Sprintf("%9s", "Dir")
	b.WriteString(styleDim.Render(header))
	b.WriteString("\n")
	b.WriteString(styleDim.Render("    " + strings.Repeat("─", max(len(header)-4, 40))))
	b.WriteString("\n")

	// --- Rows ----------------------------------------------------------------
	for i, s := range statuses {
		m := metrics[s.Entry.Name]
		h := history[s.Entry.Name]
		running := s.State == "RUNNING" && m.Available

		var cpuSeries, rssSeries []float64
		if h != nil {
			cpuSeries = h.series(func(p ProcessMetrics) float64 {
				if p.CPUPercent > 0 {
					return p.CPUPercent
				}
				return 0
			})
			rssSeries = h.series(func(p ProcessMetrics) float64 { return float64(p.RSS) })
		}

		cpuPct, rssVal, growth := "—", "—", " "
		diskCell := fmt.Sprintf("r %6s w %6s", "—", "—")
		uptime, dir := "—", "—"

		if running {
			if m.CPUPercent >= 0 {
				cpuPct = fmt.Sprintf("%.1f%%", m.CPUPercent)
			}
			if m.RSS > 0 {
				rssVal = formatBytes(m.RSS)
			}
			if h != nil {
				growth = h.rssGrowth()
				rr, wr := h.diskRate()
				diskCell = fmt.Sprintf("r %6s w %6s", formatBytes(uint64(rr)), formatBytes(uint64(wr)))
			}
			if m.Uptime > 0 {
				uptime = formatDuration(m.Uptime)
			}
		}
		if m.DirBytes > 0 {
			dir = formatBytes(m.DirBytes)
			if totalDir > 0 && float64(m.DirBytes) > float64(totalDir)*0.5 {
				dir += " " + styleWarn.Render("!!")
			}
		}

		cpuCell := Sparkline(cpuSeries, 10) + " " + fmt.Sprintf("%6s", cpuPct)
		rssCell := Sparkline(rssSeries, 10) + " " + fmt.Sprintf("%8s", rssVal) + " " + growth

		selected := i == cursor
		left, right := sel.gutters(selected)
		name := fmt.Sprintf("%-*s", nameW, s.Entry.DisplayName)
		if selected {
			name = styleBold.Render(name)
		}
		line := " " + left + " " + name +
			"  " + cpuCell +
			"  " + rssCell +
			"  " + diskCell +
			"  " + fmt.Sprintf("%7s", uptime) +
			"  " + fmt.Sprintf("%9s", dir) +
			" " + right

		// Dim stopped services unless they're the current selection.
		if !selected && !running {
			line = styleDim.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
		// Blank line between rows so the stacked sparklines read as separate
		// rows rather than one solid block.
		if i < len(statuses)-1 {
			b.WriteByte('\n')
		}
	}

	// --- Focused braille graphs for the selected service ---------------------
	if gmode != graphOff && cursor >= 0 && cursor < len(statuses) {
		focusedGraphs(&b, statuses[cursor], history[statuses[cursor].Entry.Name], width, gmode)
	}

	// --- Legend --------------------------------------------------------------
	b.WriteString("\n")
	b.WriteString(styleDim.Render(
		"  ↑/↓ navigate    s start    K stop    g gutter style    B graph (line/area/off)"))
	b.WriteString("\n")
	b.WriteString(styleDim.Render(
		"  t theme    tab/Esc back    q quit"))
	b.WriteString("\n")

	return b.String()
}

// focusedGraphs renders gotop-style braille charts of CPU%, memory (RSS) and
// component-root disk usage over the sample history for the selected service.
// Block sparklines in the table above remain the at-a-glance fallback; these
// give the detailed trend. Each chart is drawn with a left/bottom axis so the
// zero baseline is visible.
func focusedGraphs(b *strings.Builder, sel ServiceStatus, h *MetricHistory, width int, gmode graphMode) {
	wCells := 30 // 60 dot columns → the full 60-sample ring buffer
	if width > 0 && wCells > width-6 {
		wCells = width - 6
	}
	if wCells < 8 {
		wCells = 8
	}
	const hCells = 3

	var cpuSeries, memSeries, dirSeries []float64
	if h != nil {
		cpuSeries = h.series(func(p ProcessMetrics) float64 {
			if p.CPUPercent > 0 {
				return p.CPUPercent
			}
			return 0
		})
		memSeries = h.series(func(p ProcessMetrics) float64 { return float64(p.RSS) })
		dirSeries = h.series(func(p ProcessMetrics) float64 { return float64(p.DirBytes) })
	}
	b.WriteString("\n")
	b.WriteString(styleHeading.Render("  " + sel.Entry.DisplayName))
	b.WriteString(styleDim.Render("   focused graphs (last 5m)"))
	b.WriteString("\n")

	pct := func(v float64) string { return fmt.Sprintf("%.1f%%", v) }
	asBytes := func(v float64) string { return formatBytes(uint64(v)) }

	writeGraph(b, "CPU %", cpuSeries, pct, wCells, hCells, styleRunning, gmode)
	writeGraph(b, "Memory", memSeries, asBytes, wCells, hCells, stylePointer, gmode)
	writeGraph(b, "Dir", dirSeries, asBytes, wCells, hCells, styleCaution, gmode)
}

// writeGraph emits one labelled braille chart with cur/min/avg/peak stats and a
// left/bottom axis marking the zero baseline. The series is scaled so the
// window peak reaches the top of the canvas; gmode chooses a line or filled area.
func writeGraph(b *strings.Builder, label string, series []float64,
	fmtVal func(float64) string, wCells, hCells int, style lipgloss.Style, gmode graphMode) {

	cur, lo, avg, peak := seriesStats(series)
	stats := "no samples"
	if len(series) > 0 {
		stats = fmt.Sprintf("cur %s   min %s   avg %s   peak %s",
			fmtVal(cur), fmtVal(lo), fmtVal(avg), fmtVal(peak))
	}
	// The metric name is highlighted in the chart's own colour so the label,
	// axis and line all read as one colour-coded section.
	b.WriteString("  ")
	b.WriteString(style.Bold(true).Render(fmt.Sprintf("▌ %-7s", label)))
	b.WriteString("  ")
	b.WriteString(styleDim.Render(stats))
	b.WriteString("\n")

	canvas := newBrailleCanvas(wCells, hCells)
	if gmode == graphArea {
		canvas.plotArea(series, peak)
	} else {
		canvas.plotLine(series, peak)
	}
	// Each row carries a left axis "│"; the final row is the zero baseline "└──",
	// both in the chart colour to bracket the section.
	for _, row := range canvas.rows() {
		b.WriteString("  ")
		b.WriteString(style.Render("│"))
		b.WriteString(style.Render(row))
		b.WriteString("\n")
	}
	b.WriteString("  ")
	b.WriteString(style.Render("└" + strings.Repeat("─", wCells)))
	b.WriteString("\n")
}

// seriesStats returns the latest, lowest, mean, and highest values of vs.
func seriesStats(vs []float64) (cur, min, avg, max float64) {
	if len(vs) == 0 {
		return 0, 0, 0, 0
	}
	cur = vs[len(vs)-1]
	min = vs[0]
	sum := 0.0
	for _, v := range vs {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
	}
	avg = sum / float64(len(vs))
	return cur, min, avg, max
}
