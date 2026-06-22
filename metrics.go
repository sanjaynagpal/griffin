package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// ProcessMetrics holds point-in-time metrics for a single process.
type ProcessMetrics struct {
	CPUPercent float64 // % of one CPU core since last sample; -1 = first sample (no delta yet)
	UserCPU    float64 // cumulative user-mode CPU seconds
	SystemCPU  float64 // cumulative kernel-mode CPU seconds
	RSS        uint64  // resident set size (bytes)
	VirtualMem uint64  // virtual address space (bytes)
	DiskRead   uint64  // total bytes read from disk since process start
	DiskWrite  uint64  // total bytes written to disk since process start
	Threads    int32
	OpenFiles  int32 // 0 on Windows (not supported by gopsutil)
	Uptime     time.Duration
	MemPercent float32 // RSS as % of total system RAM
	PeakRSS    uint64  // peak RSS; Linux only (from /proc/<pid>/status)
	Swap       uint64  // swap usage; Linux only
	Available  bool    // false only when the process handle cannot be opened
	// Network connections (Phase 6b).
	ConnCount   int      // total TCP+UDP connections across all states; -1 = not collected
	Established int      // count of ESTABLISHED TCP connections
	Listening   []uint32 // TCP/UDP ports in LISTEN state, sorted ascending
	// Memory limit (Phase 6c).
	MemLimit uint64 // cgroup memory limit in bytes; 0 = unlimited or unknown (Linux only)
	// Component root disk usage (Phase 6d).
	DirBytes   uint64 // total size of all files under the component root
	VolumeFree uint64 // free bytes on the volume containing the component root
	// TLS certificate probe (Phase 6e).
	TLSProbed  bool      // true if a probe was attempted this cycle
	TLSExpiry  time.Time // leaf certificate NotAfter; zero if probe failed or no TLS
	TLSSubject string    // leaf certificate Subject CN
	TLSIssuer  string    // leaf certificate Issuer CN
	TLSErr     string    // non-empty if the probe attempt failed
}

// cpuSample is the previous CPU measurement used to compute the delta.
type cpuSample struct {
	total float64 // user + system seconds at the time of the last sample
	wall  time.Time
}

var (
	cpuMu   sync.Mutex
	cpuPrev = make(map[int]cpuSample)
)

// CollectMetrics gathers process metrics for pid. Fields unsupported on the
// current platform are left at their zero value. Available is false only when
// the OS process handle cannot be opened (process does not exist).
func CollectMetrics(pid int) ProcessMetrics {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return ProcessMetrics{Available: false}
	}

	m := ProcessMetrics{Available: true, CPUPercent: -1, ConnCount: -1, Established: -1}

	// CPU times and delta.
	if times, err := p.Times(); err == nil {
		cpuTotal := times.User + times.System
		m.UserCPU = times.User
		m.SystemCPU = times.System

		cpuMu.Lock()
		prev, hasPrev := cpuPrev[pid]
		now := time.Now()
		cpuPrev[pid] = cpuSample{total: cpuTotal, wall: now}
		cpuMu.Unlock()

		if hasPrev && !prev.wall.IsZero() {
			elapsed := now.Sub(prev.wall).Seconds()
			if elapsed > 0 {
				m.CPUPercent = math.Max(0, (cpuTotal-prev.total)/elapsed*100)
			}
		}
	}

	// Memory.
	if mem, err := p.MemoryInfo(); err == nil {
		m.RSS = mem.RSS
		m.VirtualMem = mem.VMS
	}
	// MemoryPercent is skipped: it calls mem.VirtualMemory() which can trigger
	// a slow WMI query on Windows. Computed on demand in the Info Panel instead.

	// Peak RSS and swap from /proc/<pid>/status (Linux only; 0 elsewhere).
	m.PeakRSS, m.Swap = readProcStatus(pid)

	// Memory limit from cgroup (Linux only; 0 elsewhere).
	m.MemLimit = readMemLimit(pid)

	// Disk I/O counters.
	if io, err := p.IOCounters(); err == nil {
		m.DiskRead = io.ReadBytes
		m.DiskWrite = io.WriteBytes
	}

	// Thread count — skipped here; gopsutil's Windows snapshot can be slow.
	// Collected on demand in the Info Panel instead.

	// Open files: skipped. On Windows gopsutil enumerates all system handles
	// via NtQuerySystemInformation which takes several seconds and blocks the
	// metrics goroutine past the collection timeout. Collected on demand only.

	// Uptime from process create time (milliseconds since Unix epoch).
	if ct, err := p.CreateTime(); err == nil {
		m.Uptime = time.Since(time.UnixMilli(ct))
	}

	// Network connections.
	// Uses GetExtendedTcpTable / GetExtendedUdpTable on Windows — fast OS table
	// lookups, not WMI or NtQuerySystemInformation.
	if conns, err := p.Connections(); err == nil {
		m.ConnCount = len(conns)
		m.Established = 0
		seen := map[uint32]struct{}{}
		for _, c := range conns {
			if c.Status == "LISTEN" {
				seen[c.Laddr.Port] = struct{}{}
			} else if c.Status == "ESTABLISHED" {
				m.Established++
			}
		}
		for port := range seen {
			m.Listening = append(m.Listening, port)
		}
		sort.Slice(m.Listening, func(i, j int) bool { return m.Listening[i] < m.Listening[j] })
	}

	return m
}

// CollectAll gathers metrics for all services. Running services get full
// process metrics; stopped services get disk info only. Each service is
// queried in its own goroutine and the whole operation is capped at 2 s.
func CollectAll(statuses []ServiceStatus) map[string]ProcessMetrics {
	if len(statuses) == 0 {
		return map[string]ProcessMetrics{}
	}

	type result struct {
		name string
		m    ProcessMetrics
	}

	ch := make(chan result, len(statuses))
	for _, s := range statuses {
		s := s
		go func() {
			var m ProcessMetrics
			if s.State == "RUNNING" && s.PID > 0 {
				m = CollectMetrics(s.PID)
			} else {
				m = ProcessMetrics{Available: false, CPUPercent: -1, ConnCount: -1, Established: -1}
			}
			// Disk info is collected for every service regardless of state.
			m.DirBytes = collectDirSize(s.Entry.ComponentRoot)
			m.VolumeFree = volumeFreeBytes(s.Entry.ComponentRoot)

			// TLS certificate probe — only for running services with TLS and a port.
			if s.State == "RUNNING" && s.Entry.TLS && s.Entry.Port != "" {
				m.TLSProbed = true
				expiry, subject, issuer, probeErr := probeTLS("localhost", s.Entry.Port)
				if probeErr != nil {
					m.TLSErr = probeErr.Error()
				} else {
					m.TLSExpiry = expiry
					m.TLSSubject = subject
					m.TLSIssuer = issuer
				}
			}

			ch <- result{s.Entry.Name, m}
		}()
	}

	out := make(map[string]ProcessMetrics, len(statuses))
	deadline := time.After(2 * time.Second)
	for range statuses {
		select {
		case r := <-ch:
			out[r.name] = r.m
		case <-deadline:
			return out
		}
	}
	return out
}

// FormatMetrics returns a map of human-readable strings keyed by metric name.
// "—" is returned for unavailable metrics or the first CPU sample.
func FormatMetrics(m ProcessMetrics) map[string]string {
	dash := "—"
	out := map[string]string{
		"cpu":           dash,
		"user_cpu":      dash,
		"sys_cpu":       dash,
		"rss":           dash,
		"virt":          dash,
		"disk_r":        dash,
		"disk_w":        dash,
		"threads":       dash,
		"files":         dash,
		"uptime":        dash,
		"mem_pct":       dash,
		"peak_rss":      dash,
		"swap":          dash,
		"conn_count":    dash,
		"established":   dash,
		"listening":     dash,
		"mem_limit":     dash,
		"dir_size":      dash,
		"vol_free":      dash,
		"tls_expiry":    dash,
		"tls_days_left": dash,
		"tls_subject":   dash,
		"tls_issuer":    dash,
	}
	if !m.Available {
		return out
	}
	if m.CPUPercent >= 0 {
		out["cpu"] = fmt.Sprintf("%.1f%%", m.CPUPercent)
	}
	out["user_cpu"] = fmt.Sprintf("%.2fs", m.UserCPU)
	out["sys_cpu"] = fmt.Sprintf("%.2fs", m.SystemCPU)
	if m.RSS > 0 {
		out["rss"] = formatBytes(m.RSS)
	}
	if m.VirtualMem > 0 {
		out["virt"] = formatBytes(m.VirtualMem)
	}
	out["disk_r"] = formatBytes(m.DiskRead)
	out["disk_w"] = formatBytes(m.DiskWrite)
	if m.Threads > 0 {
		out["threads"] = fmt.Sprintf("%d", m.Threads)
	}
	out["files"] = fmt.Sprintf("%d", m.OpenFiles)
	if m.Uptime > 0 {
		out["uptime"] = formatDuration(m.Uptime)
	}
	if m.MemPercent > 0 {
		out["mem_pct"] = fmt.Sprintf("%.1f%%", m.MemPercent)
	}
	if m.PeakRSS > 0 {
		out["peak_rss"] = formatBytes(m.PeakRSS)
	}
	if m.Swap > 0 {
		out["swap"] = formatBytes(m.Swap)
	}
	// Network connections (Phase 6b).
	if m.ConnCount >= 0 {
		out["conn_count"] = fmt.Sprintf("%d", m.ConnCount)
		out["established"] = fmt.Sprintf("%d", m.Established)
	}
	if len(m.Listening) > 0 {
		ports := make([]string, len(m.Listening))
		for i, p := range m.Listening {
			ports[i] = fmt.Sprintf("%d", p)
		}
		out["listening"] = strings.Join(ports, ", ")
	}
	if m.MemLimit > 0 {
		out["mem_limit"] = formatBytes(m.MemLimit)
	}
	if m.DirBytes > 0 {
		out["dir_size"] = formatBytes(m.DirBytes)
	}
	if m.VolumeFree > 0 {
		out["vol_free"] = formatBytes(m.VolumeFree)
	}
	if m.TLSProbed {
		if m.TLSErr != "" {
			out["tls_expiry"] = "probe failed"
			out["tls_days_left"] = "—"
		} else if !m.TLSExpiry.IsZero() {
			out["tls_expiry"] = m.TLSExpiry.Format("2006-01-02")
			days := int(time.Until(m.TLSExpiry).Hours() / 24)
			switch {
			case days < 0:
				out["tls_days_left"] = "EXPIRED"
			case days == 0:
				out["tls_days_left"] = "expires today"
			default:
				out["tls_days_left"] = fmt.Sprintf("%dd", days)
			}
			if m.TLSSubject != "" {
				out["tls_subject"] = m.TLSSubject
			}
			if m.TLSIssuer != "" {
				out["tls_issuer"] = m.TLSIssuer
			}
		}
	}
	return out
}

// CollectMetricsDetailed returns the periodic metrics for pid plus the fields
// that CollectMetrics deliberately skips for speed (thread count, open-file
// count, memory percent). Those calls can be slow on Windows, so they are only
// paid for on demand when the Info Panel opens for a single service.
func CollectMetricsDetailed(pid int) ProcessMetrics {
	m := CollectMetrics(pid)
	if !m.Available {
		return m
	}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return m
	}
	if n, err := p.NumThreads(); err == nil {
		m.Threads = n
	}
	if pct, err := p.MemoryPercent(); err == nil {
		m.MemPercent = pct
	}
	if files, err := p.OpenFiles(); err == nil {
		m.OpenFiles = int32(len(files))
	}
	return m
}

// wellKnownPorts maps common service ports to a human-readable label, appended
// to outbound connection groups in the Info Panel.
var wellKnownPorts = map[uint32]string{
	2181:  "ZooKeeper",
	3306:  "MySQL",
	5432:  "PostgreSQL",
	5672:  "RabbitMQ",
	6379:  "Redis",
	9092:  "Kafka",
	9200:  "Elasticsearch",
	11211: "Memcached",
	27017: "MongoDB",
}

// outboundGroup is a set of outbound connections sharing one remote address.
type outboundGroup struct {
	Remote string // "ip:port"
	Label  string // well-known service label, or ""
	Count  int
}

// networkDetail is the richer connection breakdown rendered by the Info Panel.
// A connection is classified as inbound when its local port is one the process
// is listening on, and outbound otherwise.
type networkDetail struct {
	Available   bool
	Listening   []uint32
	Total       int
	Established int
	Inbound     int
	Outbound    []outboundGroup
	StateCounts map[string]int
}

// collectNetworkDetail enumerates a process's sockets and partitions them into
// listen sockets, an inbound count, and outbound groups keyed by remote address.
func collectNetworkDetail(pid int) networkDetail {
	d := networkDetail{StateCounts: map[string]int{}}
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return d
	}
	conns, err := p.Connections()
	if err != nil {
		return d
	}
	d.Available = true
	d.Total = len(conns)

	listenSet := map[uint32]struct{}{}
	for _, c := range conns {
		if c.Status == "LISTEN" {
			listenSet[c.Laddr.Port] = struct{}{}
		}
	}
	for port := range listenSet {
		d.Listening = append(d.Listening, port)
	}
	sort.Slice(d.Listening, func(i, j int) bool { return d.Listening[i] < d.Listening[j] })

	groups := map[string]*outboundGroup{}
	for _, c := range conns {
		if c.Status != "" {
			d.StateCounts[c.Status]++
		}
		if c.Status == "ESTABLISHED" {
			d.Established++
		}
		if c.Status == "LISTEN" {
			continue
		}
		// No remote address → nothing to group on (e.g. UDP, CLOSED).
		if c.Raddr.IP == "" || c.Raddr.Port == 0 {
			continue
		}
		if _, isLocalListen := listenSet[c.Laddr.Port]; isLocalListen {
			d.Inbound++
			continue
		}
		key := fmt.Sprintf("%s:%d", c.Raddr.IP, c.Raddr.Port)
		if g, ok := groups[key]; ok {
			g.Count++
		} else {
			groups[key] = &outboundGroup{Remote: key, Label: wellKnownPorts[c.Raddr.Port], Count: 1}
		}
	}
	for _, g := range groups {
		d.Outbound = append(d.Outbound, *g)
	}
	sort.Slice(d.Outbound, func(i, j int) bool {
		if d.Outbound[i].Count != d.Outbound[j].Count {
			return d.Outbound[i].Count > d.Outbound[j].Count
		}
		return d.Outbound[i].Remote < d.Outbound[j].Remote
	})
	return d
}

// --- Formatting helpers -------------------------------------------------------

func formatBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}
