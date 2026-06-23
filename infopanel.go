package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// infoPanelData is the fully-collected detail for a single service, gathered
// fresh each time the Info Panel opens.
type infoPanelData struct {
	status     ServiceStatus
	m          ProcessMetrics // detailed metrics (threads/open files/mem% included)
	net        networkDetail
	tls        tlsDetail
	statusOut  string // raw stdout from the run script's STATUS command
	statusErr  string // OS-level error invoking the STATUS command
	disk       []dirSlice
	diskTotal  uint64
	volumeFree uint64
}

// loadInfoPanel gathers every detail section concurrently. It blocks until all
// collectors finish (each is internally time-bounded), then returns the result
// for the caller to hand back to the Bubble Tea model as a message.
func loadInfoPanel(s ServiceStatus) infoPanelData {
	d := infoPanelData{status: s}
	var wg sync.WaitGroup

	if s.State == "RUNNING" && s.PID > 0 {
		wg.Add(1)
		go func() { defer wg.Done(); d.m = CollectMetricsDetailed(s.PID) }()

		wg.Add(1)
		go func() { defer wg.Done(); d.net = collectNetworkDetail(s.PID) }()
	} else {
		d.m = ProcessMetrics{Available: false, CPUPercent: -1, ConnCount: -1, Established: -1}
	}

	// Service-reported status, always attempted (works for stopped services too).
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := Invoke(s.Entry, "STATUS", 5)
		d.statusOut = strings.TrimSpace(r.Stdout)
		if r.Err != nil {
			d.statusErr = r.Err.Error()
		}
	}()

	// Component-root disk usage, collected for running and stopped services.
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.disk, d.diskTotal = dirBreakdown(s.Entry.ComponentRoot)
		d.volumeFree = volumeFreeBytes(s.Entry.ComponentRoot)
	}()

	// TLS handshake probe, only for running TLS services with a port.
	if s.State == "RUNNING" && s.Entry.TLS && s.Entry.Port != "" {
		wg.Add(1)
		go func() { defer wg.Done(); d.tls = probeTLSDetail("localhost", s.Entry.Port) }()
	}

	wg.Wait()
	return d
}

// renderInfoPanel draws the full-detail view for one service. appRoot is shown
// at the top for context and used to relativise the component-root path. When
// stale is true the data is cached from a previous collection still refreshing.
func renderInfoPanel(d infoPanelData, appRoot string, width int, stale bool) string {
	s := d.status
	m := d.m

	var b strings.Builder

	// --- Header --------------------------------------------------------------
	b.WriteString(styleDim.Render("  App root  " + appRoot))
	b.WriteString("\n")
	stateStyle := styleStopped
	if s.State == "RUNNING" {
		stateStyle = styleRunning
	}
	pid := ""
	if s.PID > 0 {
		pid = fmt.Sprintf("  ·  PID %d", s.PID)
	}
	b.WriteString("  ")
	b.WriteString(styleBold.Render(s.Entry.DisplayName))
	b.WriteString("  ")
	b.WriteString(stateStyle.Render(s.State))
	b.WriteString(styleDim.Render(pid))
	if stale {
		b.WriteString(styleDim.Render("  ·  refreshing…"))
	}
	b.WriteString("\n")
	b.WriteString(styleDim.Render(strings.Repeat("─", max(width-4, 40))))
	b.WriteString("\n")

	// --- Process -------------------------------------------------------------
	if m.Available {
		section(&b, "PROCESS")
		if m.CPUPercent >= 0 {
			b.WriteString(infoRow("CPU", fmt.Sprintf("%.1f%%", m.CPUPercent)))
		} else {
			b.WriteString(infoRow("CPU", "—"))
		}
		if m.UserCPU > 0 || m.SystemCPU > 0 {
			b.WriteString(infoRow("user / sys", fmt.Sprintf("%.2fs / %.2fs", m.UserCPU, m.SystemCPU)))
		}
		if m.DiskRead > 0 || m.DiskWrite > 0 {
			b.WriteString(infoRow("Disk I/O", fmt.Sprintf("r %s  ·  w %s",
				formatBytes(m.DiskRead), formatBytes(m.DiskWrite))))
		}
		if m.Threads > 0 {
			b.WriteString(infoRow("Threads", fmt.Sprintf("%d", m.Threads)))
		}
		if m.OpenFiles > 0 {
			b.WriteString(infoRow("Open files", fmt.Sprintf("%d", m.OpenFiles)))
		}
		if m.Uptime > 0 {
			b.WriteString(infoRow("Uptime", formatDuration(m.Uptime)))
		}
	}

	// --- Memory --------------------------------------------------------------
	if m.Available {
		section(&b, "MEMORY")
		if m.RSS > 0 {
			b.WriteString(infoRow("Memory", formatBytes(m.RSS)))
		}
		if m.PeakRSS > 0 {
			b.WriteString(infoRow("Peak Memory", formatBytes(m.PeakRSS)))
		}
		if m.VirtualMem > 0 {
			b.WriteString(infoRow("Virtual", formatBytes(m.VirtualMem)))
		}
		if m.MemPercent > 0 {
			b.WriteString(infoRow("Mem %", fmt.Sprintf("%.1f%%", m.MemPercent)))
		}
		if m.Swap > 0 {
			b.WriteString(infoRow("Swap", formatBytes(m.Swap)))
		}
		// Configured limit + utilisation, colour-coded. Row omitted when no
		// limit is detected (cgroup unset or non-Linux).
		if m.MemLimit > 0 {
			val := formatBytes(m.MemLimit)
			if m.RSS > 0 {
				util := float64(m.RSS) / float64(m.MemLimit) * 100
				txt := fmt.Sprintf("%s  (%.0f%% used)", val, util)
				switch {
				case util >= 90:
					txt = styleWarn.Render(txt)
				case util >= 80:
					txt = styleCaution.Render(txt)
				}
				val = txt
			}
			b.WriteString(infoRow("Limit", val))
		}
	}

	// --- Network -------------------------------------------------------------
	if s.State == "RUNNING" {
		section(&b, "NETWORK")
		if s.Entry.Port != "" {
			b.WriteString(infoRow("Port", s.Entry.Port))
		}
		if !d.net.Available {
			b.WriteString(infoRow("Connections", "—"))
		} else {
			b.WriteString(infoRow("Connections", fmt.Sprintf("%d total  ·  %d established",
				d.net.Total, d.net.Established)))
			if len(d.net.Listening) > 0 {
				ports := make([]string, len(d.net.Listening))
				for i, p := range d.net.Listening {
					ports[i] = fmt.Sprintf("%d", p)
				}
				b.WriteString(infoRow("Listening", strings.Join(ports, ", ")))
			}
			if d.net.Inbound > 0 {
				b.WriteString(infoRow("Inbound", fmt.Sprintf("%d", d.net.Inbound)))
			}
			for _, g := range d.net.Outbound {
				label := g.Remote
				if g.Label != "" {
					label += "  " + styleDim.Render("("+g.Label+")")
				}
				b.WriteString(infoRow("→ "+label, fmt.Sprintf("%d", g.Count)))
			}
			if len(d.net.StateCounts) > 0 {
				b.WriteString(infoRow("States", formatStateCounts(d.net.StateCounts)))
			}
		}
	}

	// --- TLS certificate -----------------------------------------------------
	// Section present only for services declared with tls: true.
	if s.Entry.TLS {
		section(&b, "TLS CERTIFICATE")
		switch {
		case !d.tls.Probed:
			b.WriteString(infoRow("Status", "—"))
		case d.tls.Err != "":
			b.WriteString(infoRow("Status", styleWarn.Render("probe failed: "+d.tls.Err)))
		default:
			if d.tls.Version != "" {
				b.WriteString(infoRow("Version", d.tls.Version))
			}
			if d.tls.Cipher != "" {
				b.WriteString(infoRow("Cipher", d.tls.Cipher))
			}
			if d.tls.Subject != "" {
				b.WriteString(infoRow("Subject", d.tls.Subject))
			}
			issuer := d.tls.Issuer
			if d.tls.SelfSigned {
				issuer += styleDim.Render("  (self-signed)")
			}
			if d.tls.Issuer != "" {
				b.WriteString(infoRow("Issuer", issuer))
			}
			if !d.tls.NotBefore.IsZero() {
				b.WriteString(infoRow("Valid", fmt.Sprintf("%s  →  %s",
					d.tls.NotBefore.Format("2006-01-02"), d.tls.NotAfter.Format("2006-01-02"))))
			}
			if !d.tls.NotAfter.IsZero() {
				b.WriteString(infoRow("Days left", tlsDaysLeft(d.tls.NotAfter)))
			}
			if len(d.tls.SANs) > 0 {
				b.WriteString(infoRow("SANs", strings.Join(d.tls.SANs, ", ")))
			}
		}
	}

	// --- Component root disk usage -------------------------------------------
	section(&b, "DISK")
	if s.Entry.ComponentRoot != "" {
		b.WriteString(infoRow("Component root", relPath(appRoot, s.Entry.ComponentRoot)))
	}
	for _, sl := range d.disk {
		val := formatBytes(sl.Bytes)
		// Flag any single child that dominates more than half the tree.
		if d.diskTotal > 0 && float64(sl.Bytes) > float64(d.diskTotal)*0.5 {
			val += "  " + styleWarn.Render("!!")
		}
		b.WriteString(infoRow("  "+sl.Name, val))
	}
	b.WriteString(infoRow("Total", formatBytes(d.diskTotal)))
	if d.volumeFree > 0 {
		b.WriteString(infoRow("Volume free", formatBytes(d.volumeFree)))
	}

	// --- Service status output -----------------------------------------------
	section(&b, "STATUS OUTPUT")
	switch {
	case d.statusErr != "":
		b.WriteString(indentBlock(styleWarn.Render(d.statusErr)))
	case d.statusOut != "":
		b.WriteString(indentBlock(d.statusOut))
	default:
		b.WriteString(infoRow("", styleDim.Render("(no output)")))
	}

	return b.String()
}

// section writes a blank line and a styled section heading.
func section(b *strings.Builder, title string) {
	b.WriteString("\n")
	b.WriteString(styleHeading.Render(title))
	b.WriteString("\n")
}

// relPath returns target relative to base (e.g. the service folder name); it
// falls back to the absolute path if a relative form can't be computed.
func relPath(base, target string) string {
	if r, err := filepath.Rel(base, target); err == nil {
		return r
	}
	return target
}

// infoRow renders a labelled value row. The label is dimmed and the value kept
// at full brightness so the two read as distinct columns.
func infoRow(label, value string) string {
	return fmt.Sprintf("  %s  %s\n", styleDim.Render(fmt.Sprintf("%-16s", label)), value)
}

// indentBlock indents every line of s by four spaces.
func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = "    " + ln
	}
	return strings.Join(lines, "\n") + "\n"
}

// formatStateCounts renders connection state tallies as "ESTABLISHED 3  LISTEN 1"
// in a stable (alphabetical) order.
func formatStateCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, counts[k])
	}
	return strings.Join(parts, "  ·  ")
}

// tlsDaysLeft formats days-until-expiry with threshold colouring:
// >30d normal · 15–30d caution (yellow) · <15d critical (red) · past → EXPIRED.
func tlsDaysLeft(notAfter time.Time) string {
	days := int(time.Until(notAfter).Hours() / 24)
	if days < 0 {
		return styleWarn.Render("EXPIRED")
	}
	txt := fmt.Sprintf("%d days", days)
	switch {
	case days < 15:
		return styleWarn.Render(txt)
	case days <= 30:
		return styleCaution.Render(txt)
	default:
		return txt
	}
}
