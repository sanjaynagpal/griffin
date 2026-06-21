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

## Phase 3 — Service Discovery
> Depends on: Phase 2

**File:** `discover.go`

- [ ] Define `ServiceEntry` struct: `Name`, `ComponentRoot`, `DisplayName`, `Port`, `RunUnix`, `RunWindows`, `PIDFile`, `TLS` (bool)
- [ ] `LoadRegistry(appRoot string) ([]RegistryEntry, error)` — parse `griffin-registry.yaml`; clear error if absent or required fields missing
- [ ] `BuildServiceEntries(appRoot string) ([]ServiceEntry, error)` — resolve absolute paths; populate `DisplayName` (fallback to folder name) and `Port`; sort alphabetically
- [ ] `PIDFile` is empty string when not declared in registry (resolved at status-check time)

**Acceptance:** correct `ServiceEntry` population from complete registry; `PIDFile` empty when absent; error on missing `run.*` fields

---

## Phase 4 — Status Detection
> Depends on: Phase 3

**Files:** `status.go`, `status_unix.go`, `status_windows.go`

- [ ] Define `ServiceStatus{ Entry ServiceEntry; State string }` (`"RUNNING"` / `"STOPPED"`)
- [ ] `ResolvePIDFile(entry ServiceEntry) (string, bool)` — registry field takes precedence; else scan `$COMPONENT_ROOT/*.pid`; warn + use first alphabetically if multiple found
- [ ] `ReadPID(path string) (int, error)` — parse integer PID from file
- [ ] `status_unix.go` (`//go:build !windows`): `IsAlive` via `syscall.Kill(pid, 0)`; `nil` or `EPERM` → alive
- [ ] `status_windows.go` (`//go:build windows`): `IsAlive` via `OpenProcess` + `GetExitCodeProcess`; `STILL_ACTIVE` (259) → alive
- [ ] `CheckStatus(entry ServiceEntry) ServiceStatus`
- [ ] `RefreshAll(entries []ServiceEntry) []ServiceStatus`

**Acceptance:** STOPPED when no `.pid`; RUNNING with live PID; STOPPED when PID dead; registry `pid.file` and runtime scan both work

---

## Phase 5 — Run Command Execution
> Depends on: Phase 4

**File:** `runner.go`

- [ ] Define `RunResult{ Stdout string; ExitCode int; Err error }`
- [ ] `Invoke(entry ServiceEntry, arg string, timeoutSecs int) RunResult` — Unix: `bash <run.unix> <arg>`; Windows: `pwsh -NonInteractive -File <run.windows> <arg>`; context deadline; capture stdout
- [ ] `StartService(entry ServiceEntry) error` — invoke `START`; poll `ResolvePIDFile` every 500 ms until `.pid` appears with valid PID or 30 s timeout
- [ ] `StopService(entry ServiceEntry) error` — invoke `STOP`; poll until PID file gone or process dead; 30 s timeout
- [ ] `RestartService(entry ServiceEntry) error` — `StopService` then `StartService`; abort if stop fails

**Acceptance:** `StartService` causes PID file to appear; `StopService` causes it to disappear; timeouts return clear errors

---

## Phase 6a — Process Metrics Collection
> Depends on: Phase 5

**File:** `metrics.go`

- [ ] Add dependency `github.com/shirou/gopsutil/v3/process`
- [ ] Define `ProcessMetrics` struct: `CPUPercent`, `UserCPU`, `SystemCPU`, `RSS`, `VirtualMem`, `DiskRead`, `DiskWrite`, `Threads`, `OpenFiles`, `Uptime`, `MemPercent`, `PeakRSS`, `Swap`, `Available`
  - `PeakRSS` and `Swap` from `/proc/<pid>/status` (Linux only; 0 elsewhere)
- [ ] `CollectMetrics(pid int) ProcessMetrics` — gopsutil process handle; best-effort per field; `Available = false` only if handle cannot be opened
- [ ] CPU % delta tracking — package-level `map[int](CPUTimes, wallTime)`; `—` on first call
- [ ] `FormatMetrics(m ProcessMetrics) map[string]string` — `"512 MB"`, `"2.1%"`, `"2d 4h"`, `"—"` for unavailable
- [ ] `CollectAll(statuses []ServiceStatus) (map[string]ProcessMetrics, map[string]NetworkInfo, map[string]MemoryLimit)` — all RUNNING services

**Acceptance:** populated metrics for live process; CPU `—` first call, non-zero second; unavailable fields → `—` not error

---

## Phase 6b — Network Connection Info
> Depends on: Phase 6a

**File:** `metrics.go` (continued)

- [ ] Define `ListenSocket`, `OutboundGroup`, `NetworkInfo` structs
- [ ] `CollectNetwork(pid int) NetworkInfo` — `process.Connections("all")`; partition into listen sockets / inbound count / outbound groups (grouped by remote addr, sorted by count desc) / state counts
- [ ] Static well-known port label map: 5432 PostgreSQL, 6379 Redis, 27017 MongoDB, 3306 MySQL, 9092 Kafka, 11211 Memcached, 2181 ZooKeeper, 5672 RabbitMQ, 9200 Elasticsearch
- [ ] `Available = false` on permissions error from connections call

**Acceptance:** correct listen/inbound/outbound grouping; well-known labels appended; permissions error → `—` section

---

## Phase 6c — Memory Limit Detection
> Depends on: Phase 6a

**File:** `metrics.go` (continued)

- [ ] Define `MemoryLimit{ Detected bool; Bytes uint64; Source string }`
- [ ] `DetectMemoryLimit(pid int) MemoryLimit` — `proc.Cmdline()` scan for:
  - `-Xmx<size>` (JVM) — parse `k`/`m`/`g` suffixes
  - `-XX:MaxRAMPercentage=<n>` — resolve via `mem.VirtualMemory().Total`
  - `--max-old-space-size=<mb>` (Node.js)
  - `-Xmx` takes precedence over `MaxRAMPercentage`
- [ ] Return `Detected = false` on unrecognised runtime or parse failure

**Acceptance:** correct `Bytes` for `-Xmx2g`, `-Xmx512m`, `--max-old-space-size=4096`; `Detected=false` for process without matching flags

---

## Phase 6d — Component Root Disk Usage
> Depends on: Phase 6a

**File:** `diskusage.go`

- [ ] Define `DirUsage{ Total uint64; Breakdown map[string]uint64 }`
- [ ] `ScanComponentRoot(componentRoot string) (DirUsage, error)` — `filepath.WalkDir`; accumulate per top-level child dir and grand total; skip symlinks; error only if root unreadable
- [ ] `ScanAll(entries []ServiceEntry) map[string]DirUsage` — goroutine per service (RUNNING and STOPPED); collect results
- [ ] Wire 30-second `tea.Tick` in `model.go` → fires `ScanAll` → update stored `map[string]DirUsage`

**Acceptance:** correct totals and per-subdir sizes; stopped services included; Dir column populated for all services

---

## Phase 6e — TLS Certificate Probing
> Depends on: Phase 6a

**File:** `tlsprobe.go`

- [ ] Define `TLSInfo` struct: `Version`, `CipherSuite`, `Subject`, `Issuer`, `NotBefore`, `NotAfter`, `DaysLeft`, `SANs`, `SelfSigned`, `Available`
- [ ] `ProbeTLS(entry ServiceEntry) TLSInfo` — `tls.DialWithDialer` to `127.0.0.1:<port>`, 5 s timeout, `InsecureSkipVerify: true`; parse leaf cert; no-op if `Port == ""`
- [ ] `ProbeAllTLS(entries []ServiceEntry, statuses []ServiceStatus) map[string]TLSInfo` — only `tls: true` + RUNNING services
- [ ] Wire 24-hour `tea.Tick` in `model.go` → fires `ProbeAllTLS`
- [ ] On-demand probe when Info Panel opens for `tls: true` service; write result back to top-level model
- [ ] Expiry colour thresholds: > 30 d normal · 15–30 d yellow `!!` · < 15 d red `!!!` · expired red `EXPIRED`

**Acceptance:** correct `TLSInfo` from live TLS port; `Available=false` for non-TLS; correct colour in Status View TLS column

---

## Phase 7 — Status View
> Depends on: Phases 5, 6a, 6b, 6c, 6d, 6e

**Files:** `statusview.go`, `styles.go`, `model.go`

- [ ] `StatusViewModel` struct: `[]ServiceStatus`, `map[string]ProcessMetrics`, `map[string]DirUsage`, `map[string]TLSInfo`, cursor index, status-bar message
- [ ] `View()` renders table with columns: `#` / `Service` / `Status` / `Port` / `CPU%` / `RSS` / `Uptime` / `Dir` / `TLS`
  - RUNNING green, STOPPED yellow; cursor row highlighted
  - Dir populated for all services (RUNNING and STOPPED)
  - TLS column: colour-coded days remaining; `—` if `tls: false`
  - Column widths adapt to longest service name
- [ ] Keybindings in `Update`:
  - `↑`/`↓` / `j`/`k` — cursor navigation
  - `s` — start selected; `K` — stop selected; `R` — restart selected
  - `a` / `A` — bulk start all STOPPED / stop all RUNNING
  - `i` — open Info Panel; `m` — open Metrics Panel; `l` — open Log View
  - `r` — immediate refresh; `q` — quit
- [ ] Auto-refresh: `tea.Tick` every 5 s → `RefreshAll` + `CollectAll`
- [ ] `styles.go` — Lip Gloss style definitions (colours, column widths, highlight)

**Acceptance:** all keybindings function; status updates after lifecycle actions; TLS column shows correct threshold colours

---

## Phase 8 — Info Panel
> Depends on: Phase 7

**File:** `infopanel.go`

- [ ] `InfoPanelModel` struct: `ServiceEntry`, `ProcessMetrics`, `MemoryLimit`, `NetworkInfo`, `TLSInfo`, `DirUsage`, stdout string, err string
- [ ] On open: concurrent `CollectMetrics` + `CollectNetwork` + `Invoke(STATUS)` + `ProbeTLS` (if `tls: true`); `DirUsage` from 30 s scan in top-level model; write fresh `TLSInfo` back to top-level model
- [ ] `View()` renders five sections in order:
  - **Process metrics** — CPU, disk, threads, open files
  - **Memory** — RSS, Peak RSS, Mem%, Swap, growth rate `↑`/`→`/`↓`, configured limit + utilisation (colour at 80 % yellow, 90 % red); Limit row absent if not detected
  - **Network** — listen sockets, inbound count, outbound groups with labels, state counts; `—` if unavailable
  - **TLS certificate** — version, cipher, subject, issuer, validity, days left (colour-coded), SANs; omit section if `tls: false`; `—` if probe unavailable
  - **Component root disk usage** — per-subdir sizes; `!!` on subdir > 50 % of total
  - **Service status output** — raw stdout or run-command error
- [ ] Any keypress → return to Status View

**Acceptance:** all sections render; Memory limit row absent without `-Xmx`; utilisation colour-codes at 80 %/90 %; TLS section present only for `tls: true` services; on-demand TLS probe updates Status View column; any key dismisses

---

## Phase 9 — Log View
> Depends on: Phase 7

**File:** `logview.go`

- [ ] `LogViewModel` struct: service name, active stream (`stdout`/`stderr`), `[]string` lines, scroll offset, follow bool
- [ ] `View()` renders header with stream indicator, log lines clipped to terminal height, keybinding legend
- [ ] File tail: `tea.Tick` 500 ms; read new bytes tracking `io.Seek` offset from `$COMPONENT_ROOT/logs/stdout.log` and `stderr.log`; display notice and retry if file absent
- [ ] Keys: `↑`/`↓` scroll (exits follow mode); `f` re-enable follow; `tab` toggle stdout/stderr; `b`/`esc` back to Status View
- [ ] Wire `l` key in `StatusViewModel` and all view transitions in `model.go`

**Acceptance:** existing log lines shown on open; new lines appear within 500 ms in follow mode; `b` returns to status table; missing log file shows notice without crashing

---

## Phase 10 — Metrics Panel
> Depends on: Phases 7, 6a

**Files:** `metricsview.go`, `model.go`

- [ ] Define `MetricHistory` ring buffer: capacity 60 `ProcessMetrics` samples; `head int`, `count int`
- [ ] Add `map[string]*MetricHistory` to top-level model; populate on every 5 s tick; shared across view switches (history not lost when navigating away)
- [ ] `Sparkline(history []float64, width int) string` — scale to local min/max; map to `▁▂▃▄▅▆▇█`; right-pad with spaces if fewer than `width` samples; flat `────` when all zero/unavailable
- [ ] RSS growth rate: `(last_rss − first_rss) / elapsed` from ring buffer; `↑` growing, `→` stable, `↓` shrinking
- [ ] `SummaryBar` — total CPU %, total RSS, aggregate disk r/w rates (bytes ÷ 5 s delta), total dir size across all services
- [ ] `MetricsPanelModel` struct: `[]ServiceStatus`, `map[string]*MetricHistory`, cursor index
- [ ] `View()` renders:
  - Header (app root, running/stopped counts, time to next refresh)
  - Summary bar
  - Column headers; horizontal separator
  - One row per service: name, CPU sparkline + %, RSS sparkline + MB + growth rate, disk r/w, uptime, dir
  - Stopped services: dim colour, flat sparklines, `—` values
  - `!!` on dir value if that service > 50 % of total dir size
  - Keybinding legend
- [ ] Keys: `↑`/`↓` cursor, `s`/`K` start/stop (trigger refresh), `tab`/`esc` back, `q` quit
- [ ] Wire `m` key in `StatusViewModel`; wire `tab`/`esc` back in `MetricsPanelModel`

**Acceptance:** sparklines grow each tick; history preserved across view switches; summary bar totals correct; stopped services dim with flat sparklines; `tab` returns to status table

---

*Last updated: 2026-06-21 — Phase 2 complete*
