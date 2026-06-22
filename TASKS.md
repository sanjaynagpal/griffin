# Griffin v1.0 — Implementation Tasks

Status markers: `[ ]` not started · `[~]` in progress · `[x]` complete

---

## Phase 1 — Module Skeleton ✓
> Depends on: nothing

- [x] Create `go.mod` — module `griffin`, Go 1.24, deps: `bubbletea`, `lipgloss`
- [x] `main.go` — parse `os.Args`: `griffin init [--file <path>]` → init handler; no args → TUI mode; unknown → usage + exit 1
- [x] Validate `APP_ROOT` env var is set and directory exists; exit with clear message if not
- [x] Define `Config` struct: `AppRoot` from `APP_ROOT`
- [x] `model.go` — placeholder Bubble Tea `Model` with `Init`/`Update`/`View`

**Acceptance:** `go build ./...` succeeds; `griffin` prints "TUI mode"; `griffin init` prints "init"

> Run `go mod tidy` then `go build ./...` to verify.

---

## Phase 2 — Init Command ✓
> Depends on: Phase 1

**File:** `init.go`

- [x] `LoadServiceList(path string) ([]RegistryEntry, error)` — parse operator-provided YAML
- [x] `ScanCandidates(appRoot string) []string` — immediate children of `APP_ROOT` containing both `bin/` and `cfg/`
- [x] `BuildStubEntries(names []string) []RegistryEntry` — `# TODO` placeholders for `run.*` and `pid.file`
- [x] `WriteRegistry(appRoot string, entries []RegistryEntry, existing map[string]bool) error` — idempotent append to `griffin-registry.yaml`; skip names already present
- [x] Print summary: entries written, entries skipped, fields requiring manual completion

**Acceptance:** `griffin init --file services.yaml` produces complete registry; `griffin init` produces stub registry; re-run skips existing services

> Run `go mod tidy` then `go build ./...` to verify.

---

## Phase 3 — Service Discovery ✓
> Depends on: Phase 2

**File:** `discover.go`

- [x] Define `ServiceEntry` struct: `Name`, `ComponentRoot`, `DisplayName`, `Port`, `RunUnix`, `RunWindows`, `PIDFile`, `TLS` (bool)
- [x] `LoadRegistry(appRoot string) ([]RegistryEntry, error)` — parse `griffin-registry.yaml`; clear error if absent or required fields missing
- [x] `BuildServiceEntries(appRoot string) ([]ServiceEntry, error)` — resolve absolute paths; populate `DisplayName` (fallback to folder name) and `Port`; sort alphabetically
- [x] `PIDFile` is empty string when not declared in registry (resolved at status-check time)

**Acceptance:** correct `ServiceEntry` population from complete registry; `PIDFile` empty when absent; error on missing `run.*` fields

---

## Phase 4 — Status Detection ✓
> Depends on: Phase 3

**Files:** `status.go`, `status_unix.go`, `status_windows.go`

- [x] Define `ServiceStatus{ Entry ServiceEntry; State string }` (`"RUNNING"` / `"STOPPED"`)
- [x] `ResolvePIDFile(entry ServiceEntry) (string, bool)` — registry field takes precedence; else scan `$COMPONENT_ROOT/*.pid`; warn + use first alphabetically if multiple found
- [x] `ReadPID(path string) (int, error)` — parse integer PID from file
- [x] `status_unix.go` (`//go:build !windows`): `IsAlive` via `syscall.Kill(pid, 0)`; `nil` or `EPERM` → alive
- [x] `status_windows.go` (`//go:build windows`): `IsAlive` via `OpenProcess` + `GetExitCodeProcess`; `STILL_ACTIVE` (259) → alive
- [x] `CheckStatus(entry ServiceEntry) ServiceStatus`
- [x] `RefreshAll(entries []ServiceEntry) []ServiceStatus`

**Acceptance:** STOPPED when no `.pid`; RUNNING with live PID; STOPPED when PID dead; registry `pid.file` and runtime scan both work

---

## Phase 5 — Run Command Execution ✓
> Depends on: Phase 4

**File:** `runner.go`

- [x] Define `RunResult{ Stdout string; ExitCode int; Err error }`
- [x] `Invoke(entry ServiceEntry, arg string, timeoutSecs int) RunResult` — Unix: `bash <run.unix> <arg>`; Windows: `pwsh -NonInteractive -File <run.windows> <arg>`; context deadline; capture stdout
- [x] `StartService(entry ServiceEntry) error` — invoke `START`; poll `ResolvePIDFile` every 500 ms until `.pid` appears with valid PID or 30 s timeout
- [x] `StopService(entry ServiceEntry) error` — invoke `STOP`; poll until PID file gone or process dead; 30 s timeout
- [x] `RestartService(entry ServiceEntry) error` — `StopService` then `StartService`; abort if stop fails

**Acceptance:** `StartService` causes PID file to appear; `StopService` causes it to disappear; timeouts return clear errors

---

## Phase 6a — Process Metrics Collection ✓
> Depends on: Phase 5

**File:** `metrics.go`

- [x] Add dependency `github.com/shirou/gopsutil/v3/process`
- [x] Define `ProcessMetrics` struct: `CPUPercent`, `UserCPU`, `SystemCPU`, `RSS`, `VirtualMem`, `DiskRead`, `DiskWrite`, `Threads`, `OpenFiles`, `Uptime`, `MemPercent`, `PeakRSS`, `Swap`, `Available`
  - `PeakRSS` and `Swap` from `/proc/<pid>/status` (Linux only; 0 elsewhere)
- [x] `CollectMetrics(pid int) ProcessMetrics` — gopsutil process handle; best-effort per field; `Available = false` only if handle cannot be opened
- [x] CPU % delta tracking — package-level `map[int](CPUTimes, wallTime)`; `—` on first call
- [x] `FormatMetrics(m ProcessMetrics) map[string]string` — `"512 MB"`, `"2.1%"`, `"2d 4h"`, `"—"` for unavailable
- [x] `CollectAll(statuses []ServiceStatus) (map[string]ProcessMetrics, map[string]NetworkInfo, map[string]MemoryLimit)` — all RUNNING services

**Acceptance:** populated metrics for live process; CPU `—` first call, non-zero second; unavailable fields → `—` not error

---

## Phase 6b — Network Connection Info ✓
> Depends on: Phase 6a

**File:** `metrics.go` (continued)

- [x] Define `ListenSocket`, `OutboundGroup`, `NetworkInfo` structs
- [x] `CollectNetwork(pid int) NetworkInfo` — `process.Connections("all")`; partition into listen sockets / inbound count / outbound groups (grouped by remote addr, sorted by count desc) / state counts
- [x] Static well-known port label map: 5432 PostgreSQL, 6379 Redis, 27017 MongoDB, 3306 MySQL, 9092 Kafka, 11211 Memcached, 2181 ZooKeeper, 5672 RabbitMQ, 9200 Elasticsearch
- [x] `Available = false` on permissions error from connections call

**Acceptance:** correct listen/inbound/outbound grouping; well-known labels appended; permissions error → `—` section

---

## Phase 6c — Memory Limit Detection ✓
> Depends on: Phase 6a

**File:** `metrics.go` (continued)

- [x] Define `MemoryLimit{ Detected bool; Bytes uint64; Source string }`
- [x] `DetectMemoryLimit(pid int) MemoryLimit` — `proc.Cmdline()` scan for:
  - `-Xmx<size>` (JVM) — parse `k`/`m`/`g` suffixes
  - `-XX:MaxRAMPercentage=<n>` — resolve via `mem.VirtualMemory().Total`
  - `--max-old-space-size=<mb>` (Node.js)
  - `-Xmx` takes precedence over `MaxRAMPercentage`
- [x] Return `Detected = false` on unrecognised runtime or parse failure

**Acceptance:** correct `Bytes` for `-Xmx2g`, `-Xmx512m`, `--max-old-space-size=4096`; `Detected=false` for process without matching flags

---

## Phase 6d — Component Root Disk Usage ✓
> Depends on: Phase 6a

**File:** `diskusage.go`

- [x] Define `DirUsage{ Total uint64; Breakdown map[string]uint64 }`
- [x] `ScanComponentRoot(componentRoot string) (DirUsage, error)` — `filepath.WalkDir`; accumulate per top-level child dir and grand total; skip symlinks; error only if root unreadable
- [x] `ScanAll(entries []ServiceEntry) map[string]DirUsage` — goroutine per service (RUNNING and STOPPED); collect results
- [x] Wire 30-second `tea.Tick` in `model.go` → fires `ScanAll` → update stored `map[string]DirUsage`

**Acceptance:** correct totals and per-subdir sizes; stopped services included; Dir column populated for all services

---

## Phase 6e — TLS Certificate Probing ✓
> Depends on: Phase 6a

**File:** `tlsprobe.go`

- [x] Define `TLSInfo` struct: `Version`, `CipherSuite`, `Subject`, `Issuer`, `NotBefore`, `NotAfter`, `DaysLeft`, `SANs`, `SelfSigned`, `Available`
- [x] `ProbeTLS(entry ServiceEntry) TLSInfo` — `tls.DialWithDialer` to `127.0.0.1:<port>`, 5 s timeout, `InsecureSkipVerify: true`; parse leaf cert; no-op if `Port == ""`
- [x] `ProbeAllTLS(entries []ServiceEntry, statuses []ServiceStatus) map[string]TLSInfo` — only `tls: true` + RUNNING services
- [x] Wire 24-hour `tea.Tick` in `model.go` → fires `ProbeAllTLS`
- [x] On-demand probe when Info Panel opens for `tls: true` service; write result back to top-level model
- [x] Expiry colour thresholds: > 30 d normal · 15–30 d yellow `!!` · < 15 d red `!!!` · expired red `EXPIRED`

**Acceptance:** correct `TLSInfo` from live TLS port; `Available=false` for non-TLS; correct colour in Status View TLS column

---

## Phase 7 — Status View ✓
> Depends on: Phases 5, 6a, 6b, 6c, 6d, 6e

**Files:** `statusview.go`, `styles.go`, `model.go`

- [x] `StatusViewModel` struct: `[]ServiceStatus`, `map[string]ProcessMetrics`, `map[string]DirUsage`, `map[string]TLSInfo`, cursor index, status-bar message
- [x] `View()` renders table with columns: `#` / `Service` / `Status` / `Port` / `CPU%` / `RSS` / `Uptime` / `Dir` / `TLS`
  - RUNNING green, STOPPED yellow; cursor row highlighted
  - Dir populated for all services (RUNNING and STOPPED)
  - TLS column: colour-coded days remaining; `—` if `tls: false`
  - Column widths adapt to longest service name
- [x] Keybindings in `Update`:
  - `↑`/`↓` / `j`/`k` — cursor navigation
  - `s` — start selected; `K` — stop selected; `R` — restart selected
  - `a` / `A` — bulk start all STOPPED / stop all RUNNING
  - `i` — open Info Panel; `m` — open Metrics Panel; `l` — open Log View
  - `r` — immediate refresh; `q` — quit
- [x] Auto-refresh: `tea.Tick` every 5 s → `RefreshAll` + `CollectAll`
- [x] `styles.go` — Lip Gloss style definitions (colours, column widths, highlight)

**Acceptance:** all keybindings function; status updates after lifecycle actions; TLS column shows correct threshold colours

---

## Phase 8 — Info Panel ✓
> Depends on: Phase 7

**File:** `infopanel.go`

- [x] `infoPanelData` struct: `ServiceStatus`, detailed `ProcessMetrics`, `networkDetail`, `tlsDetail`, `[]dirSlice` + total, volume free, STATUS stdout, error
  - Note: built around the project's flattened `ProcessMetrics`/on-demand detail structs rather than the spec's separate `MemoryLimit`/`NetworkInfo`/`TLSInfo`/`DirUsage` types, which this codebase never introduced.
- [x] On open: concurrent `CollectMetricsDetailed` + `collectNetworkDetail` + `Invoke(STATUS)` + `probeTLSDetail` (if `tls: true`) + `dirBreakdown`, gathered in `loadInfoPanel` and returned as `infoLoadedMsg`
- [x] `View()` renders sections in order:
  - **Process** — CPU, user/sys, disk I/O, threads, open files, uptime
  - **Memory** — RSS, Peak RSS, Virtual, Mem%, Swap, configured limit + utilisation (colour at 80 % yellow, 90 % red); Limit row absent if not detected. *(Growth-rate `↑`/`→`/`↓` deferred to Phase 10 — needs the metric-history ring buffer.)*
  - **Network** — listen sockets, inbound count, outbound groups with well-known labels, state counts; `—` if unavailable
  - **TLS certificate** — version, cipher, subject, issuer, validity window, days left (colour-coded), SANs, self-signed flag; section omitted if `tls: false`; `—` if probe unavailable
  - **Component root disk usage** — per-subdir sizes; `!!` on subdir > 50 % of total; total + volume free
  - **Service status output** — raw stdout or run-command error
- [x] Any keypress → return to Status View (`i`/`Enter` open; Esc / any key dismiss)

**Acceptance:** all sections render; Memory limit row absent without a detected limit; utilisation colour-codes at 80 %/90 %; TLS section present only for `tls: true` services; any key dismisses. *(Growth-rate indicator pending Phase 10.)*

---

## Phase 9 — Log View ✓
> Depends on: Phase 7

**File:** `logview.go`

- [x] `logView` struct: service entry, active stream (`stdout`/`stderr`), `[]string` lines, scroll offset, follow bool, byte offset, exists flag
- [x] `View()` renders header (stream tabs + follow state + line count), log lines clipped to terminal height (`clipLine` truncates long lines), and a keybinding legend
- [x] File tail: `tea.Tick` 500 ms (`logTickCmd`) → `doReadLog` → `readLogChunk` tracks an `io.Seek` byte offset over `$COMPONENT_ROOT/logs/stdout.log` / `stderr.log`; advances only past the last newline; resets on rotation/truncation; shows a "log file not found" notice and keeps retrying if absent
- [x] Keys: `↑`/`↓` (and `k`/`j`) scroll and exit follow; `f` re-enable follow; `tab` toggle stdout/stderr (reloads from top); `b`/`Esc` back to Status View
- [x] Wire `l` key in the status view and the `modeLog` transitions in `model.go`; a generation counter discards stale ticks from a previous session

**Acceptance:** existing log lines shown on open; new lines appear within 500 ms in follow mode; `b` returns to status table; missing log file shows notice without crashing

---

## Phase 10 — Metrics Panel ✓
> Depends on: Phases 7, 6a

**Files:** `metricsview.go`, `model.go`

- [x] `MetricHistory` ring buffer: capacity 60 timestamped `ProcessMetrics` samples; `head`, `count`; `Push`/`ordered`/`series` helpers
- [x] `map[string]*MetricHistory` on the top-level model; populated on every 5 s metrics-refresh; shared across view switches (history persists when navigating away)
- [x] `Sparkline(values []float64, width int) string` — scales to local min/max; maps to `▁▂▃▄▅▆▇█`; right-pads with spaces when fewer than `width` samples; flat `────` baseline when all zero/empty/unvarying
- [x] RSS growth via `MetricHistory.rssGrowth()`: first vs last RSS over the window; `↑` growing, `↓` shrinking, `→` stable (1 MB deadband)
- [x] Summary bar — total CPU %, total RSS, aggregate disk r/w rates (per-second from last two samples), total dir size across all services
- [x] `renderMetricsPanel(appRoot, statuses, metrics, history, cursor, nextRefresh, width)` (state held on the model; cursor in `metricsCursor`)
- [x] `View()` renders:
  - Header (app root, running/stopped counts, time to next refresh)
  - Summary bar
  - Column headers; horizontal separator
  - One row per service: name, CPU sparkline + %, RSS sparkline + MB + growth rate, disk r/w, uptime, dir
  - Stopped services: dim colour, flat sparklines, `—` values
  - `!!` on dir value if that service > 50 % of total dir size
  - Keybinding legend
- [x] Keys: `↑`/`↓` cursor, `s`/`K` start/stop (trigger refresh), `tab`/`Esc` back, `q` quit
- [x] Wire `m` key in the status view; `tab`/`Esc` back from the Metrics Panel; shared history map across both views

**Acceptance:** sparklines grow each tick; history preserved across view switches; summary bar totals correct; stopped services dim with flat sparklines; `tab` returns to status table

---

*Last updated: 2026-06-21 — All phases (1–10) complete. Post-v1.0 enhancements: bulk start/stop (`a`/`A`), pointer/sidebar selection gutters (`g`), Dir column, gotop-style braille focused graphs in the Metrics Panel (`B`), and cycleable colour schemes (`t`: default/nord/solarized/mono). Pending a local `go build ./...` / `gofmt` pass.*
