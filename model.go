package main

import (
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Message types — each carries the result of an async operation.
// ---------------------------------------------------------------------------

type tickMsg time.Time

// statusRefreshMsg is the result of RefreshAll running in a goroutine.
type statusRefreshMsg []ServiceStatus

// metricsRefreshMsg is the result of CollectAll running in a goroutine.
type metricsRefreshMsg map[string]ProcessMetrics

// lifecycleMsg is returned by start/stop/restart goroutines.
type lifecycleMsg struct {
	action string // "started" | "stopped" | "restarted"
	name   string
	err    error
}

// infoLoadedMsg carries the fully-collected detail for the Info Panel.
type infoLoadedMsg struct {
	data infoPanelData
}

// bulkDoneMsg summarises the outcome of a bulk start/stop action. action is the
// present-tense verb ("start" | "stop").
type bulkDoneMsg struct {
	action string
	ok     int
	fail   int
	err    error // first error encountered, surfaced in the status bar
}

// logTickMsg drives the 500 ms log-tail poll while the Log View is open. The
// generation tag lets stale ticks from a previous session be discarded.
type logTickMsg struct{ gen int }

// logDataMsg carries newly-read log content back to the model.
type logDataMsg struct {
	stream    string
	lines     []string
	newOffset int64
	exists    bool
	reset     bool // replace existing lines rather than append
}

// ---------------------------------------------------------------------------
// View modes
// ---------------------------------------------------------------------------

type viewMode int

const (
	modeStatus  viewMode = iota // main service table
	modeInfo                    // full-detail info panel for the selected service
	modeLog                     // live log viewer for the selected service
	modeMetrics                 // sparkline metrics dashboard for all services
)

// ---------------------------------------------------------------------------
// Async commands — these return tea.Cmd values that run in goroutines.
// ---------------------------------------------------------------------------

func doRefreshStatus(entries []ServiceEntry) tea.Cmd {
	return func() tea.Msg {
		return statusRefreshMsg(RefreshAll(entries))
	}
}

func doCollectMetrics(statuses []ServiceStatus) tea.Cmd {
	return func() tea.Msg {
		return metricsRefreshMsg(CollectAll(statuses))
	}
}

func doStart(entry ServiceEntry) tea.Cmd {
	return func() tea.Msg {
		return lifecycleMsg{action: "started", name: entry.DisplayName, err: StartService(entry)}
	}
}

func doStop(entry ServiceEntry) tea.Cmd {
	return func() tea.Msg {
		return lifecycleMsg{action: "stopped", name: entry.DisplayName, err: StopService(entry)}
	}
}

func doRestart(entry ServiceEntry) tea.Cmd {
	return func() tea.Msg {
		return lifecycleMsg{action: "restarted", name: entry.DisplayName, err: RestartService(entry)}
	}
}

// doStartAll starts every STOPPED service concurrently and reports a summary.
func doStartAll(statuses []ServiceStatus) tea.Cmd {
	return bulkLifecycle("start", statuses, "STOPPED", StartService)
}

// doStopAll stops every RUNNING service concurrently and reports a summary.
func doStopAll(statuses []ServiceStatus) tea.Cmd {
	return bulkLifecycle("stop", statuses, "RUNNING", StopService)
}

// bulkLifecycle runs op against every service in the given state, concurrently,
// and returns a single bulkDoneMsg once all have completed.
func bulkLifecycle(action string, statuses []ServiceStatus, state string, op func(ServiceEntry) error) tea.Cmd {
	return func() tea.Msg {
		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			ok, fail int
			firstErr error
		)
		for _, s := range statuses {
			if s.State != state {
				continue
			}
			s := s
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := op(s.Entry)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					fail++
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %w", s.Entry.DisplayName, err)
					}
				} else {
					ok++
				}
			}()
		}
		wg.Wait()
		return bulkDoneMsg{action: action, ok: ok, fail: fail, err: firstErr}
	}
}

// doLoadInfo gathers all Info Panel detail sections in a goroutine.
func doLoadInfo(s ServiceStatus) tea.Cmd {
	return func() tea.Msg {
		return infoLoadedMsg{data: loadInfoPanel(s)}
	}
}

// doReadLog reads new content for a service's log stream from a byte offset.
func doReadLog(entry ServiceEntry, stream string, offset int64, reset bool) tea.Cmd {
	return func() tea.Msg {
		lines, newOffset, exists := readLogChunk(logPath(entry, stream), offset)
		return logDataMsg{stream: stream, lines: lines, newOffset: newOffset, exists: exists, reset: reset}
	}
}

// logTickCmd schedules the next 500 ms log-tail poll for the given generation.
func logTickCmd(gen int) tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return logTickMsg{gen: gen}
	})
}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type model struct {
	cfg         Config
	entries     []ServiceEntry
	statuses    []ServiceStatus
	metrics     map[string]ProcessMetrics
	view        StatusView
	mode        viewMode
	err         error
	width       int
	height      int
	busy        bool
	statusBar   string
	infoData    *infoPanelData // detail for the open Info Panel; nil while loading
	infoLoading bool
	log         logView // state for the open Log View
	logGen      int     // increments each time the Log View opens

	metricHistory map[string]*MetricHistory // per-service sample ring buffers
	metricsCursor int                       // selected row in the Metrics Panel
	lastTick      time.Time                 // when the last 5 s refresh tick fired
	sel           selectionStyle            // how the selected row is marked in tables
	graphMode     graphMode                 // focused-graph style in the Metrics Panel (line/area/off)
	themeIdx      int                       // index of the active colour scheme
}

// initialModel builds the root model from config. It does NOT block on any
// OS calls — all data collection is kicked off asynchronously from Init().
func initialModel(cfg Config) model {
	entries, err := BuildServiceEntries(cfg.AppRoot)
	m := model{
		cfg:           cfg,
		entries:       entries,
		metrics:       make(map[string]ProcessMetrics),
		metricHistory: make(map[string]*MetricHistory),
		mode:          modeStatus,
		err:           err,
		lastTick:      time.Now(),
		graphMode:     graphLine,
	}
	if err == nil {
		m.view = newStatusView(nil, m.metrics)
	}
	return m
}

// Init fires the first status refresh immediately; the tick timer runs in
// parallel so the first real tick doesn't wait 5 s.
func (m model) Init() tea.Cmd {
	if m.err != nil {
		return nil
	}
	return tea.Batch(
		doRefreshStatus(m.entries),
		tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
	)
}

// Update handles all incoming messages. Blocking work is never done here —
// every slow operation is dispatched as a tea.Cmd goroutine.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	// --- Async results -------------------------------------------------------

	case statusRefreshMsg:
		m.statuses = []ServiceStatus(msg)
		m.view.statuses = m.statuses
		// Kick off metrics collection now that we have fresh PIDs.
		return m, doCollectMetrics(m.statuses)

	case metricsRefreshMsg:
		m.metrics = map[string]ProcessMetrics(msg)
		m.view.metrics = m.metrics
		// Append the fresh sample to each service's history so the Metrics
		// Panel's sparklines persist across view switches.
		for _, s := range m.statuses {
			h := m.metricHistory[s.Entry.Name]
			if h == nil {
				h = &MetricHistory{}
				m.metricHistory[s.Entry.Name] = h
			}
			h.Push(m.metrics[s.Entry.Name])
		}

	case infoLoadedMsg:
		// Ignore if the user already closed the panel before collection finished.
		if m.mode == modeInfo {
			d := msg.data
			m.infoData = &d
			m.infoLoading = false
		}

	case logTickMsg:
		// Keep tailing only while the Log View is open and this is the current
		// generation (discards stale ticks from a previous session).
		if m.mode == modeLog && msg.gen == m.logGen {
			return m, tea.Batch(
				doReadLog(m.log.entry, m.log.stream, m.log.byteOffset, false),
				logTickCmd(m.logGen),
			)
		}

	case logDataMsg:
		if m.mode != modeLog || msg.stream != m.log.stream {
			return m, nil
		}
		m.log.exists = msg.exists
		if msg.reset {
			m.log.lines = msg.lines
		} else if len(msg.lines) > 0 {
			m.log.lines = append(m.log.lines, msg.lines...)
		}
		m.log.byteOffset = msg.newOffset

	case lifecycleMsg:
		m.busy = false
		if msg.err != nil {
			m.statusBar = fmt.Sprintf("✗ %s %s: %s", msg.action, msg.name, msg.err)
		} else {
			m.statusBar = fmt.Sprintf("✓ %s %s", msg.action, msg.name)
		}
		// Refresh status (and metrics will follow via statusRefreshMsg).
		return m, doRefreshStatus(m.entries)

	case bulkDoneMsg:
		m.busy = false
		verbed := "started"
		if msg.action == "stop" {
			verbed = "stopped"
		}
		switch {
		case msg.ok == 0 && msg.fail == 0:
			m.statusBar = fmt.Sprintf("No services to %s", msg.action)
		case msg.fail == 0:
			m.statusBar = fmt.Sprintf("✓ %s %d service(s)", verbed, msg.ok)
		default:
			m.statusBar = fmt.Sprintf("⚠ %s %d, %d failed: %s", verbed, msg.ok, msg.fail, msg.err)
		}
		return m, doRefreshStatus(m.entries)

	// --- Timer ---------------------------------------------------------------

	case tickMsg:
		m.lastTick = time.Now()
		return m, tea.Batch(
			doRefreshStatus(m.entries),
			tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }),
		)

	// --- Keyboard ------------------------------------------------------------

	case tea.KeyMsg:
		// ctrl+c quits from any mode.
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		// Esc always returns to the status view.
		if msg.String() == "esc" {
			m.mode = modeStatus
			m.infoData = nil
			m.infoLoading = false
			return m, nil
		}

		// 'g' toggles the selection gutter style (pointer > < vs solid sidebar)
		// across the table views.
		if msg.String() == "g" && (m.mode == modeStatus || m.mode == modeMetrics) {
			if m.sel == selPointer {
				m.sel = selSidebar
			} else {
				m.sel = selPointer
			}
			return m, nil
		}

		// 't' cycles the colour scheme from the table views.
		if msg.String() == "t" && (m.mode == modeStatus || m.mode == modeMetrics) {
			m.themeIdx++
			applyTheme(m.themeIdx)
			m.statusBar = "Theme: " + themeName(m.themeIdx)
			return m, nil
		}

		switch m.mode {
		case modeInfo:
			// Any key dismisses the Info Panel and returns to the table.
			m.mode = modeStatus
			m.infoData = nil
			m.infoLoading = false
			return m, nil

		case modeLog:
			switch msg.String() {
			case "b":
				m.mode = modeStatus
				return m, nil
			case "up", "k":
				m.log.follow = false
				if m.log.offset > 0 {
					m.log.offset--
				}
			case "down", "j":
				m.log.follow = false
				if m.log.offset < len(m.log.lines)-1 {
					m.log.offset++
				}
			case "f":
				m.log.follow = true
			case "tab":
				// Switch stream and reload from the top.
				if m.log.stream == "stdout" {
					m.log.stream = "stderr"
				} else {
					m.log.stream = "stdout"
				}
				m.log.lines = nil
				m.log.byteOffset = 0
				m.log.offset = 0
				m.log.follow = true
				m.log.exists = true
				return m, doReadLog(m.log.entry, m.log.stream, 0, true)
			}

		case modeMetrics:
			switch msg.String() {
			case "tab", "b":
				m.mode = modeStatus
				return m, nil
			case "B":
				m.graphMode = (m.graphMode + 1) % 3
				m.statusBar = "Graph: " + m.graphMode.String()
			case "q":
				return m, tea.Quit
			case "up", "k":
				if m.metricsCursor > 0 {
					m.metricsCursor--
				}
			case "down", "j":
				if m.metricsCursor < len(m.statuses)-1 {
					m.metricsCursor++
				}
			case "s":
				if !m.busy && len(m.statuses) > 0 {
					sel := m.statuses[m.metricsCursor]
					if sel.State == "STOPPED" {
						m.busy = true
						m.statusBar = fmt.Sprintf("Starting %s…", sel.Entry.DisplayName)
						return m, doStart(sel.Entry)
					}
				}
			case "K":
				if !m.busy && len(m.statuses) > 0 {
					sel := m.statuses[m.metricsCursor]
					if sel.State == "RUNNING" {
						m.busy = true
						m.statusBar = fmt.Sprintf("Stopping %s…", sel.Entry.DisplayName)
						return m, doStop(sel.Entry)
					}
				}
			}

		case modeStatus:
			switch msg.String() {
			case "q":
				return m, tea.Quit

			case "up", "k":
				if m.view.cursor > 0 {
					m.view.cursor--
				}

			case "down", "j":
				if m.view.cursor < len(m.statuses)-1 {
					m.view.cursor++
				}

			case "enter", "i":
				if len(m.statuses) > 0 {
					m.mode = modeInfo
					m.infoData = nil
					m.infoLoading = true
					return m, doLoadInfo(m.statuses[m.view.cursor])
				}

			case "l":
				if len(m.statuses) > 0 {
					m.logGen++
					m.mode = modeLog
					m.log = newLogView(m.statuses[m.view.cursor].Entry)
					return m, tea.Batch(
						doReadLog(m.log.entry, m.log.stream, 0, true),
						logTickCmd(m.logGen),
					)
				}

			case "m":
				if len(m.statuses) > 0 {
					if m.metricsCursor >= len(m.statuses) {
						m.metricsCursor = 0
					}
					m.mode = modeMetrics
				}

			case "r":
				if m.err == nil {
					m.statusBar = ""
					return m, doRefreshStatus(m.entries)
				}

			case "s":
				if !m.busy && len(m.statuses) > 0 {
					sel := m.statuses[m.view.cursor]
					if sel.State == "STOPPED" {
						m.busy = true
						m.statusBar = fmt.Sprintf("Starting %s…", sel.Entry.DisplayName)
						return m, doStart(sel.Entry)
					}
				}

			case "K":
				if !m.busy && len(m.statuses) > 0 {
					sel := m.statuses[m.view.cursor]
					if sel.State == "RUNNING" {
						m.busy = true
						m.statusBar = fmt.Sprintf("Stopping %s…", sel.Entry.DisplayName)
						return m, doStop(sel.Entry)
					}
				}

			case "R":
				if !m.busy && len(m.statuses) > 0 {
					sel := m.statuses[m.view.cursor]
					m.busy = true
					m.statusBar = fmt.Sprintf("Restarting %s…", sel.Entry.DisplayName)
					return m, doRestart(sel.Entry)
				}

			case "a":
				if !m.busy && countInState(m.statuses, "STOPPED") > 0 {
					m.busy = true
					m.statusBar = "Starting all stopped services…"
					return m, doStartAll(m.statuses)
				}

			case "A":
				if !m.busy && countInState(m.statuses, "RUNNING") > 0 {
					m.busy = true
					m.statusBar = "Stopping all running services…"
					return m, doStopAll(m.statuses)
				}
			}
		}
	}

	return m, nil
}

// countInState returns the number of statuses currently in the given state.
func countInState(statuses []ServiceStatus, state string) int {
	n := 0
	for _, s := range statuses {
		if s.State == state {
			n++
		}
	}
	return n
}

// topBar is the dated header shown at the top of every panel.
func topBar() string {
	return styleDim.Render("  griffin · "+time.Now().Format("Mon 2006-01-02  15:04:05")) + "\n"
}

// View renders the current frame.
func (m model) View() string {
	if m.err != nil {
		return fitToFrame(topBar()+"\ngriffin: "+m.err.Error()+"\n\nPress q to quit.\n", m.width, m.height)
	}

	var content string
	switch m.mode {
	case modeLog:
		content = m.log.View(m.width, m.height)

	case modeMetrics:
		nextRefresh := 5*time.Second - time.Since(m.lastTick)
		content = renderMetricsPanel(m.cfg.AppRoot, m.statuses, m.metrics,
			m.metricHistory, m.metricsCursor, nextRefresh, m.width, m.sel, m.graphMode)

	case modeInfo:
		if len(m.statuses) > 0 {
			inner := "\n  Collecting details…\n"
			if m.infoData != nil {
				inner = renderInfoPanel(*m.infoData, m.width)
			}
			content = infoPanelBorder.Render(inner)
			break
		}
		m.mode = modeStatus
		fallthrough

	default:
		view := m.view.View(m.width, m.sel)
		if m.statusBar != "" {
			view += "\n  " + m.statusBar + "\n"
		}
		content = view
	}

	return fitToFrame(topBar()+content, m.width, m.height)
}

// fitToFrame forces content into a block of exactly width×height cells. Lip
// Gloss pads every line to the full width (clearing stale characters left by a
// previous, wider view such as the Log View) and truncates over-long lines so
// nothing wraps onto extra physical rows — which is what otherwise left log
// remnants on screen when returning to a shorter view. Padding rows to a fixed
// height keeps Bubble Tea's diff renderer rewriting every row. It degrades
// gracefully when width/height are still 0 before the first WindowSizeMsg.
func fitToFrame(s string, width, height int) string {
	st := lipgloss.NewStyle()
	if width > 0 {
		st = st.Width(width).MaxWidth(width)
	}
	if height > 0 {
		st = st.Height(height).MaxHeight(height)
	}
	return st.Render(s)
}
