# Griffin — Technical Specification

> **Status:** v1.0

---

## 1. Purpose

Griffin is a terminal UI application written in Go that supervises the services belonging to an application. It provides a single pane of glass for operators to see which services are running, start or stop individual or all services, restart them, and tail their logs — all from a keyboard-driven full-screen interface.

Griffin operates in two modes simultaneously:

- **Orchestrator** — it calls each service's run command with `START` or `STOP` to trigger lifecycle transitions.
- **Observer** — it determines the true state of each service by watching OS-level signals: whether the service's PID file exists and whether the process it identifies is alive. Griffin never trusts the run command's output to determine state; it only trusts what the operating system reports.

Griffin is **application-agnostic**: it places no requirements on the services it supervises. Services do not know about Griffin and their run commands are not tailored to it. The run command's `STATUS` argument is invoked on demand and its output shown to the operator as-is — Griffin does not parse it.

Griffin's own configuration — the registry — lives at `$APP_ROOT` and is created by Griffin during initialisation.

---

## 2. Scope

### In scope

- One-time initialisation (`griffin init`) that builds `griffin-registry.yaml` from a user-provided service list or by scanning `$APP_ROOT`.
- Registry stubs requiring operator review before first use.
- Discovering all services registered in `griffin-registry.yaml` on startup.
- Determining service state (RUNNING / STOPPED) by observing PID file existence and process liveness.
- Starting a service by invoking its run command with `START`, then polling for the PID file to appear.
- Stopping a service by invoking its run command with `STOP`, then polling until the PID is gone or the process is dead.
- Restarting a service (STOP then START) in one action.
- Starting or stopping all services in one bulk action.
- Invoking the run command with `STATUS` on demand and displaying the output to the operator.
- Viewing the live tail of a service's log output within the TUI.
- Cross-platform operation: Linux, macOS, and Windows (amd64).

### Out of scope (v1)

- Automatic restart / watchdog behaviour.
- Health-check polling or alerting.
- Service dependency ordering.
- Remote control (HTTP API, gRPC).
- Configuration of services themselves (environment variables, JVM flags, etc.).
- Log rotation.
- General non-interactive CLI subcommands (beyond `griffin init`).

---

## 3. Environment

| Variable | Default | Description |
|---|---|---|
| `APP_ROOT` | *(required)* | Absolute path to the application base folder containing all service folders. |

---

## 4. Service Discovery

### Terminology

| Term | Meaning |
|---|---|
| `$APP_ROOT` | The application base folder. Each immediate child folder is a service. |
| Service | An immediate child folder of `$APP_ROOT`. The folder name is the service name. |
| `$COMPONENT_ROOT` | The root folder of a specific service — i.e., `$APP_ROOT/<service-name>/`. All paths within a service are expressed relative to `$COMPONENT_ROOT`. |
| Run command | A script in `$COMPONENT_ROOT/bin/` that accepts `START`, `STOP`, or `STATUS` as its argument. Its interface is defined by the application, not Griffin. |
| PID file | A file with a `.pid` extension written by the run command when the service starts, containing the integer process ID of the service. Each service produces exactly one PID file. Its location may be declared in the registry or discovered by Griffin at runtime. |

### 4.1 Installed Service Layout

Services arrive at `$APP_ROOT` already fully installed. Every service folder contains exactly two subdirectories: `bin/` for executables and `cfg/` for configuration files. There are no sub-service folders. Griffin places no files inside service folders; all files it creates live at `$APP_ROOT`.

```
$APP_ROOT/
├── griffin-registry.yaml       ← created by `griffin init`; owned by Griffin
├── alpha-eight/                ← service "alpha-eight"; $COMPONENT_ROOT = this folder
│   ├── bin/                    ← $COMPONENT_ROOT/bin/ — executables
│   │   ├── runA8.sh            ← run command (Unix); its interface is the app's concern
│   │   ├── runA8.ps1           ← run command (Windows)
│   │   └── lib/
│   ├── cfg/                    ← $COMPONENT_ROOT/cfg/ — configuration files
│   └── alpha-eight.pid         ← written by the run command on START; read by Griffin
├── gamma-go/
│   ├── bin/
│   │   ├── run.sh
│   │   ├── run.ps1
│   │   └── lib/
│   ├── cfg/
│   └── gamma-go.pid
└── xyz-service/
    ├── bin/
    │   ├── runXYZ.sh
    │   └── runXYZ.ps1
    ├── cfg/
    └── xyz-service.pid
```

### 4.2 Service Registration via Registry

On normal startup, Griffin reads `$APP_ROOT/griffin-registry.yaml` to learn which service folders exist and which run command to invoke for each. If the registry is absent, Griffin reports the problem and directs the operator to run `griffin init`. The `pid.file` field is optional — Griffin discovers it at runtime when not declared.

The registry is the single source of truth for:
- Which folders under `$APP_ROOT` are managed services.
- Which script is the run command for each service.
- Where each service's PID file is located.

### 4.3 Service Metadata

For display purposes, Griffin resolves each service's name and port from the registry entry. If `name` or `port` are absent from the registry, the service name (folder name) is used as the display name and port is shown as `—`. Griffin does not read application configuration files; the application manages its own environment-based configuration independently.

### 4.4 Registry Format (`griffin-registry.yaml`)

The registry is generated by `griffin init` and lives at `$APP_ROOT/griffin-registry.yaml`. Operators must review and complete it before running Griffin normally — specifically, every `run.*` and `pid.file` field must be filled in. Each entry is keyed by the service name (folder name). All paths are relative to `$COMPONENT_ROOT`.

```yaml
alpha-eight:
  name: Alpha Eight             # optional display name shown in the TUI
  port: "8080"                  # optional
  tls: true                     # optional; enables TLS certificate probing on the declared port
  run.unix: bin/runA8.sh        # required; relative to $COMPONENT_ROOT
  run.windows: bin/runA8.ps1    # required on Windows
  pid.file: alpha-eight.pid     # optional; relative to $COMPONENT_ROOT

gamma-go:
  run.unix: bin/run.sh
  run.windows: bin/run.ps1
  # pid.file omitted — Griffin will scan $COMPONENT_ROOT for *.pid at runtime

xyz-service:
  port: "8443"
  tls: true
  run.unix: bin/runXYZ.sh
  run.windows: bin/runXYZ.ps1
```

The `tls` field is optional and defaults to `false`. When `true`, Griffin probes the declared `port` on each refresh cycle to collect TLS certificate information (§8.3). A `port` must be declared for `tls: true` to take effect; if `port` is absent Griffin logs a configuration warning and skips the probe.

`pid.file` is optional. When omitted, Griffin scans `$COMPONENT_ROOT` for any file matching `*.pid` at status-check time; since each service is a single process there will be at most one such file. If `pid.file` is provided it takes precedence over scanning. A service entry missing `run.unix` (on Unix) or `run.windows` (on Windows) is invalid for start/stop operations but Griffin will still observe its status via PID discovery.

### 4.5 Initialisation (`griffin init`)

`griffin init` creates `griffin-registry.yaml`. It supports two input modes:

#### Mode 1 — Service list file (preferred)

The operator provides a YAML file listing each service's name, run command, and PID file. This is the fastest path to a complete registry.

```
griffin init --file services.yaml
```

Format of `services.yaml`:

```yaml
alpha-eight:
  run.unix: bin/runA8.sh
  run.windows: bin/runA8.ps1
  pid.file: alpha-eight.pid     # optional; Griffin scans *.pid if omitted

gamma-go:
  run.unix: bin/run.sh
  run.windows: bin/run.ps1
  # pid.file omitted — discovered at runtime
```

Griffin reads this file and writes the registry. `pid.file` and the display fields `name` and `port` are all optional.

#### Mode 2 — Scan (fallback)

If no `--file` is given, Griffin scans `$APP_ROOT` for immediate child folders that contain both `bin/` and `cfg/` subdirectories and creates a stub registry entry for each. **Griffin does not attempt to infer run commands.** Run command fields are written as `# TODO` placeholders for the operator to fill in. `pid.file` is omitted from stubs — Griffin will discover the `.pid` file at runtime.

```
griffin init
```

Generated stub:

```yaml
alpha-eight:
  # name:      (optional display name shown in the TUI)
  # port:      (optional port shown in the TUI)
  run.unix:    # TODO: path to run command relative to $COMPONENT_ROOT
  run.windows: # TODO: path to run command relative to $COMPONENT_ROOT
  # pid.file omitted — Griffin scans $COMPONENT_ROOT for *.pid at runtime
```

#### Registry File Behaviour

| Condition | Result |
|---|---|
| Registry does not exist | Created |
| Registry already exists | New entries appended; existing entries untouched |
| Folder already has a registry entry | Skipped |
| No new candidates found | Prints notice; file unchanged |

`griffin init` is idempotent — re-running after all folders are registered prints "No new service folders found" and exits without modifying the file.

#### Recommended Workflow

```
# Option A: provide a service list (preferred)
griffin init --file services.yaml

# Option B: scan and fill in manually
griffin init
$EDITOR $APP_ROOT/griffin-registry.yaml   # fill in all # TODO lines

# Then start Griffin
griffin
```

---

## 5. Status Detection

Griffin determines service state exclusively by observing OS-level signals. It does not call the run command to determine state.

### 5.1 States

| State | Meaning |
|---|---|
| **RUNNING** | The service's PID file exists and the process it names is alive. |
| **STOPPED** | The PID file does not exist, or the process it names is no longer alive. |

### 5.2 PID File Resolution

For each service, Griffin resolves the PID file path using this precedence:

1. **Registry `pid.file`** — if declared, resolve it as `$COMPONENT_ROOT/<pid.file>` and use it directly.
2. **Runtime scan** — if not declared, scan `$COMPONENT_ROOT` for any file matching `*.pid`. Since each service is a single process, there is at most one such file. If more than one is found, Griffin reports a configuration warning and uses the first alphabetically.

### 5.3 Status Check Procedure

On every status refresh, for each service:

1. Resolve the PID file path (§5.2). If no `.pid` file exists → **STOPPED**.
2. Parse the integer PID from the file. If unparseable → **STOPPED** (treat as stale).
3. Probe process liveness:
   - **Linux / macOS**: `syscall.Kill(pid, 0)` — signal 0 tests existence without delivery. `errno == nil` or `errno == EPERM` → alive.
   - **Windows**: `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, pid)` + `GetExitCodeProcess`; exit code `STILL_ACTIVE` (259) → alive.
4. Process alive → **RUNNING**; otherwise → **STOPPED**.

Griffin never writes to or removes the PID file. The PID file is entirely owned by the service's run command.

### 5.4 Refresh Cadence

The status table refreshes automatically every 5 seconds. The operator can force an immediate refresh with `r`.

---

## 6. Process Metrics

When a service is RUNNING, Griffin collects OS-level metrics for the process identified by its PID file. Metrics are refreshed on the same 5-second cycle as status. When a metric is unavailable on the current platform, Griffin shows `—` rather than an error.

### 6.1 Metric Definitions

| Metric | Source | Notes |
|---|---|---|
| **CPU %** | Delta CPU time ÷ elapsed wall time | Computed over each refresh interval |
| **User CPU time** | Cumulative user-space execution time | Since process start |
| **System CPU time** | Cumulative kernel-space execution time | Since process start |
| **RSS** | Resident Set Size | Physical RAM currently in use |
| **RSS growth rate** | Delta RSS ÷ elapsed time, computed from the ring-buffer history | e.g. `+2.4 MB/min`; negative = shrinking; `—` until two samples are available |
| **Memory %** | RSS ÷ total system RAM | Shows how much of the machine the process is consuming |
| **Peak RSS (HWM)** | High Water Mark — maximum RSS since process start | Linux: from `/proc/<pid>/status` VmHWM; macOS/Windows: `—` |
| **Swap** | Bytes of the process's address space currently paged to disk | Non-zero is a sign of memory pressure; Linux only |
| **Virtual memory** | Total address space reserved | Includes mapped files, heap, stacks |
| **Configured memory limit** | Memory limit declared in the process's command-line arguments | Detected from cmdline via `proc.Cmdline()`; see §8.4 for supported flags |
| **Limit utilisation** | RSS ÷ configured memory limit | Only shown when a limit is detected; colour-coded at 80 % and 90 % thresholds |
| **Disk read** | Cumulative bytes read from disk | Since process start |
| **Disk write** | Cumulative bytes written to disk | Since process start |
| **Uptime** | `now − process creation time` | Formatted as `Xd Xh Xm Xs` |
| **Threads** | OS thread count | Active threads in the process |
| **Open files** | Open file descriptor count | Files, sockets, pipes |
| **Listen address** | Local address and port the process is bound to in LISTEN state | e.g. `0.0.0.0:8080`; confirms the declared port is actually open |
| **Inbound connections** | Count of ESTABLISHED connections on the listening port | Active clients connected to this service |
| **Outbound connections** | Remote addresses this process has ESTABLISHED connections to | Reveals databases, caches, and peer services this service is connected to; grouped by remote host:port with a count |
| **Connection state counts** | Counts per TCP state across all sockets | e.g. `ESTABLISHED 17  TIME_WAIT 2  CLOSE_WAIT 1` |
| **Dir size** | Total bytes consumed by `$COMPONENT_ROOT/` | Recursive directory walk; applies to RUNNING and STOPPED services |
| **TLS version** | Protocol version negotiated during handshake | e.g. `TLS 1.3`, `TLS 1.2`; requires `tls: true` in registry |
| **Cipher suite** | Cipher suite negotiated during handshake | e.g. `TLS_AES_256_GCM_SHA384` |
| **Certificate subject** | Common Name (CN) and Organisation (O) from the leaf certificate | |
| **Certificate issuer** | CA name from the leaf certificate | e.g. `Let's Encrypt`, `self-signed` |
| **Not Before / Not After** | Certificate validity window | |
| **Days until expiry** | `cert.NotAfter − now` | Shown with colour coding; negative = already expired |
| **SANs** | Subject Alternative Names (DNS names and IP addresses the cert covers) | |
| **Self-signed** | Whether issuer equals subject | |

### 6.2 Platform Availability

| Metric | Linux | macOS | Windows |
|---|---|---|---|
| CPU %, user/system time | ✓ | ✓ | ✓ |
| RSS, virtual memory, memory %, RSS growth rate | ✓ | ✓ | ✓ |
| Peak RSS (HWM) | ✓ (VmHWM in `/proc/<pid>/status`) | — | — |
| Swap usage | ✓ (VmSwap in `/proc/<pid>/status`) | — | — |
| Configured memory limit (cmdline parse) | ✓ | ✓ | ✓ |
| Uptime, threads | ✓ | ✓ | ✓ |
| Disk read/write | ✓ | elevated privileges required | ✓ |
| Open files | ✓ | elevated privileges required | approximate via handle count |
| Listen address, inbound/outbound connections, state counts | ✓ (via `/proc/<pid>/net/tcp`) | elevated privileges required | ✓ (via `iphlpapi.dll`) |
| Dir size | ✓ | ✓ | ✓ |
| TLS certificate info | ✓ | ✓ | ✓ |

Griffin uses `github.com/shirou/gopsutil/v3/process` for process metrics. Directory size is collected independently via `filepath.WalkDir` (standard library, no external dependency). No CGO is required. Unavailable metrics are shown as `—` without surfacing an error.

---

## 7. TUI Design

Griffin's interface is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and styled with [Lip Gloss](https://github.com/charmbracelet/lipgloss). It has four views: the **Status View** (default), the **Info Panel**, the **Metrics Panel**, and the **Log View**.

### 7.1 Status View

The status table shows a compact summary of each service. Metric columns are populated only for RUNNING services.

```
Griffin   root=/opt/app
──────────────────────────────────────────────────────────────────────────────────────────────────────
  #   Service             Status     Port    CPU%    RSS      Uptime      Dir       TLS
 ──  ──────────────────  ────────  ──────  ──────  ───────  ─────────  ───────  ──────────
  1  alpha-eight         RUNNING    8080    2.1%    512 MB   2d 4h      1.2 GB   45d
  2  gamma-go            STOPPED    8086      —       —        —         340 MB   12d !!
  3  kepler-eleven       RUNNING    8084    0.4%    128 MB   14h 22m    88 MB    —
  4  xyz-service         RUNNING    8443    1.8%    256 MB   6h 55m     12 MB    3d !!!
──────────────────────────────────────────────────────────────────────────────────────────────────────
[↑↓] select  [s] start  [K] stop  [R] restart  [i] info  [m] metrics  [l] logs  [a/A] bulk  [r] refresh  [q] quit
```

- The cursor row is highlighted (reverse video or a distinct background).
- **RUNNING** is rendered in green; **STOPPED** in yellow.
- The **Dir** column shows the total bytes consumed by `$COMPONENT_ROOT/` and is populated for **all** services, regardless of whether they are running. A stopped service can still accumulate large amounts of data in its log, work, or cache directories.
- The **TLS** column shows days until certificate expiry for services with `tls: true` in the registry. Colour coding: > 30 days — normal; 15–30 days — yellow `!!`; < 15 days — red `!!!`; already expired — red `EXPIRED`. Services without `tls: true` show `—`.
- Services are sorted alphabetically by name.
- Column widths adapt to the longest service name.
- The status table refreshes automatically every 5 seconds; `r` forces an immediate refresh.

### 7.2 Keybindings — Status View

| Key | Action |
|---|---|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `s` | Start the selected service (no-op if RUNNING) |
| `K` (shift) | Stop the selected service (no-op if STOPPED) |
| `R` (shift) | Restart the selected service |
| `i` | Open Info Panel for the selected service |
| `m` | Open Metrics Panel (all services) |
| `l` | Open Log View for the selected service |
| `a` | Start all STOPPED services |
| `A` (shift) | Stop all RUNNING services |
| `r` | Refresh all statuses immediately |
| `q` | Quit (running services are not touched) |

### 7.3 Info Panel

Pressing `i` opens the Info Panel, which has four sections displayed together:

**Griffin metrics** — CPU, disk, thread, and open-file metrics collected from the OS for the process named by the PID file.

**Memory** — detailed memory breakdown: RSS, peak RSS, memory as a percentage of system RAM, swap usage, RSS growth rate derived from the ring-buffer history, and the configured memory limit detected from the process command line (§8.4).

**Network** — listen socket, inbound client count, and outbound connections to other services. Derived from `process.Connections()`.

**TLS certificate** — certificate details obtained by making a TLS dial to the declared port. Only shown for services with `tls: true` in the registry.

**Component root disk usage** — a per-subdirectory breakdown of all disk space consumed under `$COMPONENT_ROOT/`. Populated for both RUNNING and STOPPED services.

**Run command output** — the stdout from invoking `<run-command> STATUS`, shown verbatim for the operator to read. Griffin does not parse it.

```
Info — alpha-eight
──────────────────────────────────────────────────────────────────────────────────
Process metrics
  PID          14321          Uptime        2d 4h 12m
  CPU %        2.1%           Threads       24
  User CPU     1h 14m         Open files    312
  System CPU   8m 32s         Disk read     1.2 GB
  Virtual      2.1 GB         Disk write    340 MB

Memory
  RSS          512 MB         Memory %      12.5% of system
  Peak RSS     680 MB         Swap             0 B
  Growth rate  +2.4 MB/min ↑
  Limit (Xmx)  2 GB           Utilisation   25.6%

Network
  Listen        0.0.0.0:8080   LISTEN
  Inbound       12 ESTABLISHED

  Outbound connections
    127.0.0.1:5432    ESTABLISHED ×3     (PostgreSQL pool)
    127.0.0.1:6379    ESTABLISHED ×4     (Redis pool)
    10.0.1.5:9092     ESTABLISHED ×1

  States   ESTABLISHED 20   TIME_WAIT 2   CLOSE_WAIT 1

TLS certificate
  Version       TLS 1.3
  Cipher        TLS_AES_256_GCM_SHA384
  Subject       CN=alpha-eight.example.com
  Issuer        Let's Encrypt
  Valid from    2026-01-10
  Expires       2026-07-10   (20 days)  !!
  SANs          alpha-eight.example.com
                10.0.1.10

Component root disk usage   ($APP_ROOT/alpha-eight/)   total: 1.2 GB
  bin/       8.4 MB
  cfg/       512 KB
  logs/      890 MB  !!
  work/      180 MB
  cache/     140 MB

Service status output
  Service: alpha-eight
  Status:  Running on port 8080
  Build:   v2.4.1 (2026-05-10)
──────────────────────────────────────────────────────────────────────────────────
[any key] close
```

Unavailable metrics show `—`. If the run command exits with a non-zero code, the service status output section shows the error instead. In the Memory section, the growth rate arrow (`↑` / `→` / `↓`) indicates the trend over the most recent ring-buffer window; limit utilisation is colour-coded at 80 % (yellow) and 90 % (red); the Limit row is omitted if no supported flag is detected in the cmdline. In the disk usage section, subdirectories that account for more than 50% of the total component root size are flagged with `!!`. In the Network section, outbound connections are grouped by remote host:port with a count; if Griffin recognises the remote port as a well-known service (e.g. 5432 → PostgreSQL, 6379 → Redis, 27017 → MongoDB, 3306 → MySQL, 9092 → Kafka), it appends a label in parentheses. Unavailable connection data (platform permissions) shows `—` for the entire Network section. In the TLS section, the expiry date is colour-coded using the same thresholds as the Status View column; the TLS section is omitted entirely for services without `tls: true` in the registry.

### 7.4 Log View

Pressing `l` opens the Log View, which displays the tail of the service's log files. Log files are expected at `$COMPONENT_ROOT/logs/stdout.log` and `$COMPONENT_ROOT/logs/stderr.log`, written by the service itself.

```
Logs — alpha-eight   [stdout | stderr]   (following tail)
────────────────────────────────────────────────────────────────
2026-06-20 10:14:01  INFO  Server started on :8080
2026-06-20 10:14:02  INFO  Connected to database
2026-06-20 10:14:05  INFO  Received request GET /api/status
...
────────────────────────────────────────────────────────────────
[↑↓] scroll  [tab] toggle stdout/stderr  [f] follow  [b] back
```

- Default mode is **follow** (auto-scrolls to the tail as new lines arrive).
- `↑` / `↓` scrolls; scrolling up exits follow mode.
- `f` re-enables follow mode.
- `tab` toggles between stdout and stderr.
- `b` or `esc` returns to the Status View.
- Log lines are read by polling the file every 500 ms.
- If the log file does not exist, the view displays a notice and retries on each tick.

### 7.5 Metrics Panel

Pressing `m` from the Status View opens the Metrics Panel — a full-screen view showing live OS metrics for **all** services simultaneously, updated on the same 5-second refresh cycle.

The panel is modelled after Windows Task Manager's Processes tab: each service occupies one row, with sparkline graphs representing the recent history of CPU % and RSS alongside current scalar values for disk I/O and uptime. Stopped services are rendered dimmed with `—` in all metric columns; their sparkline areas are drawn as flat separator lines.

```
Griffin   root=/opt/app   [3 RUNNING / 1 STOPPED]   refresh in 3s
──────────────────────────────────────────────────────────────────────────────────────────────────────
total cpu 4.9%   total rss 768 MB   disk r/s 1.2 MB   disk w/s 340 KB   total dir 1.6 GB

Service             ── cpu % ──────────────────   ── rss ──────────────────────────   disk r/w        uptime        dir
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
alpha-eight         ▁▁▂▃▄▅▆▅▄▃▃▄▅▆▇█▇▆▅▄  2.1%   ▅▅▅▅▆▆▆▇▇▇▇▇▇▇▇▇▇▇▇▇  512 MB  +2.4 MB/m ↑   ↑1.2MB ↓840KB  2d 4h 12m   1.2 GB !!
gamma-go            ────────────────────    —      ────────────────────    —         —            —              STOPPED     340 MB
kepler-eleven       ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▂▂▂▂▂▂  0.4%   ▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂▂  128 MB  +0.1 MB/m →   ↑48KB  ↓12KB   14h 22m     88 MB
xyz-service         ▂▃▄▅▆▇█▇▆▅▄▃▂▁▁▂▃▄▄▃  2.4%   ▃▃▃▃▃▃▃▄▄▄▄▄▄▄▄▄▄▄▄▄  128 MB  +0.0 MB/m →   ↑8KB   ↓2KB    6h 55m      12 MB
──────────────────────────────────────────────────────────────────────────────────────────────────────────────────
[↑↓] select  [s] start  [K] stop  [tab] status view  [q] quit
```

**Sparklines** are rendered using Unicode block characters `▁▂▃▄▅▆▇█`. Each sparkline is 20 characters wide, representing the **last 60 samples** (5 minutes at a 5-second refresh cadence) scaled to the local range of the visible window. Samples are accumulated in a per-service ring buffer; the oldest sample is evicted when the buffer is full. On the first refresh after Griffin starts, the sparkline is rendered with whatever samples are available (may be a single point).

**Summary bar** at the top shows totals across all services: combined CPU % and RSS (RUNNING only), aggregate disk read/write rates (per-second delta between the last two samples, RUNNING only), and total dir size across **all** services (RUNNING and STOPPED). The `!!` flag on a dir column value indicates that the component root for that service exceeds 50% of the total combined dir size — a quick visual cue that one service is disproportionately consuming disk.

**Navigation**: `↑`/`↓` moves the cursor row (same service selection as the Status View); `s`/`K` start/stop the selected service and trigger a refresh; `tab` or `esc` returns to the Status View.

---

## 8. Process Metrics Collection

Griffin collects metrics for a RUNNING service by opening a `gopsutil` process handle using the PID from the service's PID file. Collection is best-effort: if a metric call returns an error (e.g. insufficient privileges), that metric is recorded as unavailable and shown as `—`.

CPU % is computed as a delta: Griffin records the process's cumulative CPU time on each refresh cycle and divides the increment by elapsed wall-clock time. The first refresh after a service starts shows `—` for CPU % until a second sample is available.

### 8.1 Component Root Disk Usage

Directory size is collected for every service regardless of its state. Griffin walks `$COMPONENT_ROOT/` recursively using `filepath.WalkDir`, summing the sizes of all regular files. Symlinks are not followed.

The scan returns two results:

- **Total size** — shown in the `Dir` column of the Status View and as a column in the Metrics Panel.
- **Per-subdirectory breakdown** — shown in the Info Panel. Griffin lists each immediate child directory of `$COMPONENT_ROOT/` with its subtree size.

Because directory walking can be expensive for services with large log trees, the disk scan runs on a separate, slower cadence: **every 30 seconds** (every sixth 5-second tick), rather than on every status refresh. The previously collected value is shown between scans. The scan runs concurrently with status collection and does not block the UI.

### 8.2 Network Connection Collection

Griffin calls `process.Connections("all")` from `gopsutil/v3/process`, which returns every socket the process has open. Each entry includes: local address, remote address, TCP state (LISTEN, ESTABLISHED, TIME_WAIT, CLOSE_WAIT, etc.), and transport protocol (TCP/UDP).

From this flat list, Griffin derives:

- **Listen sockets** — entries where `Status == "LISTEN"`. There is typically one per protocol (TCP4, TCP6). The local address confirms the bind address and port.
- **Inbound connections** — entries where `Laddr.Port` matches a listen socket's port and `Status == "ESTABLISHED"`. The count is the number of active clients connected to this service.
- **Outbound connections** — entries where `Raddr.Port != 0` and `Laddr.Port` does not match any listen socket (i.e. the process initiated the connection). Grouped by `Raddr.IP:Raddr.Port` with a count. Sorted by count descending.
- **State counts** — a frequency map over `Status` for all sockets (ESTABLISHED, TIME_WAIT, CLOSE_WAIT, etc.).

**Well-known port labels**: Griffin maintains a static map of common remote port numbers to service names for display purposes only:

| Port | Label |
|---|---|
| 5432 | PostgreSQL |
| 3306 | MySQL |
| 1433 | SQL Server |
| 27017 | MongoDB |
| 6379 | Redis |
| 11211 | Memcached |
| 9092 | Kafka |
| 2181 | ZooKeeper |
| 5672 | RabbitMQ |
| 9200 | Elasticsearch |

Labels are cosmetic annotations; they do not affect any Griffin logic.

Network connection data is collected on the same **5-second** refresh cycle as process metrics. If `process.Connections()` returns an error (platform permissions), the entire Network section shows `—`.

The `NetworkInfo` struct:

```go
type ListenSocket struct {
    LocalAddr string // "0.0.0.0:8080"
    Protocol  string // "tcp4", "tcp6", "udp"
}

type OutboundGroup struct {
    RemoteAddr string // "127.0.0.1:5432"
    State      string // "ESTABLISHED"
    Count      int
    Label      string // "PostgreSQL" if port is well-known; empty otherwise
}

type NetworkInfo struct {
    ListenSockets []ListenSocket
    InboundCount  int
    Outbound      []OutboundGroup // sorted by Count descending
    StateCounts   map[string]int  // "ESTABLISHED" → 20, "TIME_WAIT" → 2
    Available     bool
}
```

### 8.3 TLS Certificate Probing

For each service with `tls: true` in the registry, Griffin dials the declared port over TCP and performs a TLS handshake using Go's standard `crypto/tls` package. The dial uses `InsecureSkipVerify: true` so that self-signed certificates and already-expired certificates are still readable — Griffin is inspecting, not validating. The probe uses a 5-second connection timeout.

```go
conn, err := tls.DialWithDialer(
    &net.Dialer{Timeout: 5 * time.Second},
    "tcp",
    net.JoinHostPort("127.0.0.1", entry.Port),
    &tls.Config{InsecureSkipVerify: true},
)
```

From the resulting `tls.ConnectionState` and the leaf `*x509.Certificate`, Griffin collects:

```go
type TLSInfo struct {
    Version     string    // "TLS 1.3", "TLS 1.2"
    CipherSuite string    // human-readable name via tls.CipherSuiteName
    Subject     string    // cert.Subject.CommonName
    Issuer      string    // cert.Issuer.CommonName; "self-signed" if Issuer == Subject
    NotBefore   time.Time
    NotAfter    time.Time
    DaysLeft    int       // int(cert.NotAfter.Sub(now).Hours() / 24); negative = expired
    SANs        []string  // cert.DNSNames + string representations of cert.IPAddresses
    SelfSigned  bool      // cert.Issuer.String() == cert.Subject.String()
    Available   bool      // false if dial fails or port is not declared
}
```

**Probe cadence**: TLS probes run once every **24 hours** — on a separate `tea.Tick` independent of the 5-second status refresh. The previously collected value is shown between probes, so the displayed expiry date ages naturally without re-probing. Probes are only made when the service is RUNNING (port is reachable).

An **on-demand probe** is also triggered when the operator opens the Info Panel for a service that has `tls: true` — this gives a fresh read at exactly the moment the operator is looking at TLS details, without waiting for the next daily tick. The result is written back to the top-level model so the Status View TLS column also updates immediately.

**Expiry thresholds** (applied consistently in Status View TLS column and Info Panel TLS section):

| Days left | Display | Colour |
|---|---|---|
| > 30 | `45d` | normal |
| 15 – 30 | `22d !!` | yellow |
| 1 – 14 | `8d !!!` | red |
| 0 | `TODAY !!!` | red |
| < 0 | `EXPIRED` | red |

The probe dials `127.0.0.1` (loopback) on the declared port. This means Griffin must be running on the same host as the services — consistent with its general design as a local supervisor.

### 8.4 Memory Limit Detection

Griffin reads the process command line via `proc.Cmdline()` and scans it for known memory-limit flags. This is purely OS-level observation — it requires no knowledge of the application's internals.

Supported flags:

| Runtime | Flag | Example | Parsed value |
|---|---|---|---|
| JVM (Java) | `-Xmx<size>` | `-Xmx2g`, `-Xmx512m` | Max heap size |
| JVM (Java) | `-XX:MaxRAMPercentage=<n>` | `-XX:MaxRAMPercentage=75.0` | % of system RAM; Griffin resolves to bytes |
| Node.js | `--max-old-space-size=<mb>` | `--max-old-space-size=4096` | Old-generation heap limit in MB |
| .NET | `DOTNET_GCHeapHardLimit` (env var) | not in cmdline | out of scope for cmdline parse |

Size suffixes recognised: `k`/`K` (KiB), `m`/`M` (MiB), `g`/`G` (GiB). Unrecognised suffixes or unparseable values are ignored; `MemoryLimit.Detected` remains `false`.

```go
type MemoryLimit struct {
    Detected bool
    Bytes    uint64  // resolved limit in bytes
    Source   string  // e.g. "-Xmx2g", "--max-old-space-size=4096"
}
```

`DetectMemoryLimit(pid int) MemoryLimit` — calls `proc.Cmdline()`, iterates tokens, returns the first recognised match. If multiple JVM flags are present (e.g. both `-Xmx` and `-XX:MaxRAMPercentage`), `-Xmx` takes precedence as it is the more explicit bound.

`MemoryLimit` is collected on the same 5-second refresh cycle as other process metrics, since the command line does not change while the process is running.

---

## 9. Start Sequence

1. Invoke the run command with `START` **synchronously**:
   - Unix: `bash <run.unix> START`
   - Windows: `pwsh -NonInteractive -File <run.windows> START`
   - Environment: inherited from Griffin's own process unchanged.
2. Wait for the command to exit. The start script is expected to daemonise the service and return quickly.
3. Poll `$COMPONENT_ROOT/<pid.file>` every 500 ms until the file appears and contains a valid PID, or until a fixed internal timeout (30 s) elapses → report error: "service did not write PID file".
4. Trigger a status refresh.

---

## 10. Stop Sequence

1. Invoke the run command with `STOP` **synchronously**:
   - Unix: `bash <run.unix> STOP`
   - Windows: `pwsh -NonInteractive -File <run.windows> STOP`
   - Environment: inherited from Griffin's own process unchanged.
2. Wait for the command to exit.
3. Poll every 500 ms until the PID file is gone or the process is no longer alive (§5.3 liveness probe), or until a fixed internal timeout (30 s) elapses → report error: "service did not stop".
4. Trigger a status refresh.

---

## 11. Restart

Restart is a composed operation: stop (§8) followed by start (§7), executed sequentially. If the stop step fails or times out, the start step is not attempted and the error is surfaced in the TUI.

---

## 12. Logging

Griffin reads log files written by the service itself. The expected paths are:

```
$COMPONENT_ROOT/logs/stdout.log   — service standard output
$COMPONENT_ROOT/logs/stderr.log   — service standard error
```

Griffin does not write to, rotate, or manage these files. If they are absent, the Log View displays a notice and polls until they appear.

---

## 13. Exit Behaviour

- Running services are **left running** when Griffin exits. Their lifecycle is entirely managed by the service's own run command.
- On `q` or SIGINT/SIGTERM, Griffin exits cleanly, restoring the terminal. No run commands are invoked.

---

## 14. Module File Layout

```
griffin/
├── docs/
│   └── griffin-specification.md   ← this file
├── go.mod
├── main.go                         ← entry point; dispatch init vs TUI
├── model.go                        ← top-level Bubble Tea Model, Init/Update/View
├── init.go                         ← griffin init: file mode and scan mode, registry write
├── discover.go                     ← read registry, build []ServiceEntry
├── runner.go                       ← invoke run command (START/STOP/STATUS); capture output
├── status.go                       ← RefreshAll: PID file read + liveness probe per service
├── metrics.go                      ← collect CPU, memory, disk, thread, connection metrics via gopsutil
├── diskusage.go                    ← walk $COMPONENT_ROOT/; return total size and per-subdirectory breakdown
├── tlsprobe.go                     ← TLS dial; parse leaf certificate into TLSInfo
├── status_unix.go                  ← kill-0 liveness probe      (//go:build !windows)
├── status_windows.go               ← OpenProcess liveness probe  (//go:build windows)
├── statusview.go                   ← Bubble Tea model for the status table view
├── infopanel.go                    ← Bubble Tea model for the STATUS output overlay
├── metricsview.go                  ← Bubble Tea model for the Metrics Panel (sparklines + summary bar)
├── logview.go                      ← Bubble Tea model for the log tail view
└── styles.go                       ← Lip Gloss style definitions
```

---

## 15. Implementation Plan

### Phase 1 — Module Skeleton

**Goal**: A compilable binary that dispatches between `griffin init` and TUI mode.

**Files**: `go.mod`, `main.go`, `model.go`

**Tasks**:
1. Create `go.mod` — module path `griffin`, Go 1.24, dependencies: `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`.
2. In `main.go`, parse `os.Args`:
   - `griffin init [--file <path>]` → call init handler (placeholder: print "init" and exit).
   - No args → TUI mode (placeholder: print "TUI mode" and exit).
   - Unknown args → print usage, exit 1.
3. Validate `APP_ROOT` is set and the directory exists; exit with a clear message if not.
4. Define `Config` struct: `AppRoot` parsed from `APP_ROOT` env var.

**Acceptance**: `go build ./...` succeeds; `griffin` prints "TUI mode"; `griffin init` prints "init".

---

### Phase 2 — Init Command

**Goal**: Build `griffin-registry.yaml` from a service list file or by scanning `$APP_ROOT`.

**Files**: `init.go`

**Tasks**:
1. `LoadServiceList(path string) ([]RegistryEntry, error)` — parse the operator-provided YAML file.
2. `ScanCandidates(appRoot string) []string` — returns folder names of immediate children that contain both `bin/` and `cfg/`. Used when no `--file` is given.
3. `BuildStubEntries(names []string) []RegistryEntry` — creates stub entries with `# TODO` for `run.*` and `pid.file`; `name` and `port` are left for the operator to fill in.
4. `WriteRegistry(appRoot string, entries []RegistryEntry, existing map[string]bool) error` — appends new entries to `$APP_ROOT/griffin-registry.yaml`; skips names already in `existing`.
5. Print a summary of entries written and any fields that need manual completion.
6. Idempotent: skip folders already in the registry.

**Acceptance**: `griffin init --file services.yaml` produces a complete registry; `griffin init` produces a stub registry with `# TODO` fields; re-running either form skips already-registered services.

---

### Phase 3 — Service Discovery

**Goal**: Read `griffin-registry.yaml` and produce `[]ServiceEntry`.

**Files**: `discover.go`

**Tasks**:
1. Define `ServiceEntry`:
   ```go
   type ServiceEntry struct {
       Name          string   // service name; equals the folder name under $APP_ROOT
       ComponentRoot string   // absolute path: $APP_ROOT/<name>
       DisplayName   string   // from registry name field; falls back to Name
       Port          string   // from registry port field; empty if unknown
       RunUnix       string   // absolute path to Unix run command
       RunWindows    string   // absolute path to Windows run command
       PIDFile       string   // absolute path if declared in registry; empty = discover at runtime
   }
   ```
2. `LoadRegistry(appRoot string) ([]RegistryEntry, error)` — parse `griffin-registry.yaml`; return a clear error if absent or if required fields are missing.
3. `BuildServiceEntries(appRoot string) ([]ServiceEntry, error)` — resolve absolute paths for run commands and PID files; populate `DisplayName` and `Port` from registry fields.
4. Sort entries alphabetically by `DisplayName`.

**Acceptance**: `BuildServiceEntries` on a complete registry returns correctly populated entries; `PIDFile` is empty when not declared in the registry (resolved at status-check time); returns an error only on missing `run.*` fields.

---

### Phase 4 — Status Detection

**Goal**: Determine RUNNING / STOPPED for each service by observing the PID file and process liveness.

**Files**: `status.go`, `status_unix.go`, `status_windows.go`

**Tasks**:
1. Define `ServiceStatus{ Entry ServiceEntry; State string }`.
2. `ResolvePIDFile(entry ServiceEntry) (string, bool)` — returns `entry.PIDFile` if set; otherwise scans `$COMPONENT_ROOT` for files matching `*.pid`. Returns the path and `true` if found, empty string and `false` if none exists. Logs a warning if more than one `.pid` file is found and uses the first alphabetically.
3. `ReadPID(path string) (int, error)` — reads and parses the integer PID from the file.
4. Platform-specific `IsAlive(pid int) bool`:
   - `status_unix.go` (`//go:build !windows`): `syscall.Kill(pid, 0) == nil || errno == syscall.EPERM`.
   - `status_windows.go` (`//go:build windows`): `OpenProcess` + `GetExitCodeProcess == STILL_ACTIVE`.
5. `CheckStatus(entry ServiceEntry) ServiceStatus` — resolves PID file, reads PID, probes liveness; returns RUNNING or STOPPED.
6. `RefreshAll(entries []ServiceEntry) []ServiceStatus`.

**Acceptance**: `RefreshAll` returns STOPPED when no `.pid` file exists in `$COMPONENT_ROOT`; RUNNING when a `.pid` file contains a live PID; STOPPED when the file exists but the process is dead; uses registry `pid.file` when declared and scan otherwise.

---

### Phase 5 — Run Command Execution

**Goal**: Invoke the run command with START, STOP, or STATUS and capture output.

**Files**: `runner.go`

**Tasks**:
1. Define `RunResult{ Stdout string; ExitCode int; Err error }`.
2. `Invoke(entry ServiceEntry, arg string, env []string, timeoutSecs int) RunResult`:
   - Builds the command: Unix `bash <run.unix> <arg>`, Windows `pwsh -NonInteractive -File <run.windows> <arg>`.
   - Runs with a context deadline (`timeoutSecs`).
   - Captures stdout; stderr is discarded (operator reads logs directly).
   - Returns `RunResult`; sets `Err` on timeout or exec failure.
3. `StartService(entry ServiceEntry) error` — calls `Invoke(START)`, then polls via `ResolvePIDFile` every 500 ms until a `.pid` file appears with a valid PID or the internal 30 s timeout elapses.
4. `StopService(entry ServiceEntry) error` — calls `Invoke(STOP)`, then polls every 500 ms until `ResolvePIDFile` finds no `.pid` file or `IsAlive` returns false, or the internal 30 s timeout elapses.
5. `RestartService(entry ServiceEntry) error` — `StopService` then `StartService`.

**Acceptance**: `StartService` against a real service causes the PID file to appear; `StopService` causes it to disappear or the process to die; timeout returns a clear error.

---

### Phase 6 — Process Metrics

**Goal**: Collect OS-level metrics for each RUNNING service process.

**Files**: `metrics.go`, `diskusage.go`

**Tasks**:
1. Add dependency: `github.com/shirou/gopsutil/v3/process`.
2. Define `ProcessMetrics`:
   ```go
   type ProcessMetrics struct {
       CPUPercent  float64  // — if unavailable
       UserCPU     float64  // seconds
       SystemCPU   float64  // seconds
       RSS         uint64   // bytes
       VirtualMem  uint64   // bytes
       DiskRead    uint64   // bytes cumulative
       DiskWrite   uint64   // bytes cumulative
       Threads     int32
       OpenFiles   int32         // -1 if unavailable
       Uptime      time.Duration
       // Memory detail
       MemPercent  float32       // RSS as % of total system RAM
       PeakRSS     uint64        // VmHWM bytes; 0 if unavailable (non-Linux)
       Swap        uint64        // VmSwap bytes; 0 if unavailable (non-Linux)
       Available   bool          // false if PID not reachable
   }
   ```
   `NetworkInfo` and `MemoryLimit` are defined separately (see §8.2 and §8.4). RSS growth rate is derived from the ring-buffer history in `metricsview.go`, not stored in `ProcessMetrics`.
3. `CollectMetrics(pid int) ProcessMetrics` — opens a gopsutil `process.Process`, calls each metric method; sets field to zero/-1 on error, sets `Available = false` only if the process handle itself cannot be opened.
4. CPU %: implement delta tracking — store last `(CPUTimes, wallTime)` per PID in a package-level map; compute percent on each call; return `—` on first call.
5. `FormatMetrics(m ProcessMetrics) map[string]string` — returns human-readable strings for each field (`"512 MB"`, `"2.1%"`, `"2d 4h"`, `"—"` for unavailable).
6. `CollectNetwork(pid int) NetworkInfo` — calls `process.Connections("all")`; partitions results into listen sockets, inbound count, outbound groups (grouped and sorted), and state counts. Appends well-known port labels from the static map. Sets `Available = false` on any error from the connections call.
7. `DetectMemoryLimit(pid int) MemoryLimit` — calls `proc.Cmdline()` and scans tokens for recognised memory-limit flags (§8.4). Returns `MemoryLimit{Detected: false}` on any parse failure or unrecognised runtime. For `-XX:MaxRAMPercentage`, read total system RAM via `mem.VirtualMemory().Total` and compute the resolved byte limit.
8. `CollectAll(statuses []ServiceStatus) (map[string]ProcessMetrics, map[string]NetworkInfo, map[string]MemoryLimit)` — maps service name → metrics, network info, and memory limit for all RUNNING services.
8. In `diskusage.go`, define `DirUsage`:
   ```go
   type DirUsage struct {
       Total       uint64            // total bytes under $COMPONENT_ROOT/
       Breakdown   map[string]uint64 // immediate child dir name → subtree bytes
   }
   ```
9. `ScanComponentRoot(componentRoot string) (DirUsage, error)` — walks `$COMPONENT_ROOT/` with `filepath.WalkDir`; accumulates sizes per top-level child directory (the first path component below `$COMPONENT_ROOT/`) and the grand total. Symlinks are skipped. Returns an error only if the root itself is unreadable.
10. `ScanAll(entries []ServiceEntry) map[string]DirUsage` — calls `ScanComponentRoot` for every service (RUNNING and STOPPED); runs each scan in a goroutine; collects results into a map keyed by service name.
11. Wire the 30-second cadence in `model.go`: maintain a separate `tea.Tick` at 30 s that fires `ScanAll` and updates the stored `map[string]DirUsage`.
12. In `tlsprobe.go`, implement `ProbeTLS(entry ServiceEntry) TLSInfo` — dials `127.0.0.1:<port>` with a 5-second timeout and `InsecureSkipVerify: true`; parses the leaf certificate into `TLSInfo`; sets `Available = false` if the port is not declared, the dial fails, or the handshake does not complete. No-op (returns `TLSInfo{Available: false}`) if `entry.Port == ""`.
13. `ProbeAllTLS(entries []ServiceEntry, statuses []ServiceStatus) map[string]TLSInfo` — calls `ProbeTLS` only for entries where `tls: true` in the registry and the service is RUNNING; returns a map keyed by service name.
14. Wire the 24-hour TLS cadence in `model.go`: a separate `tea.Tick` at 24 h that fires `ProbeAllTLS` and updates the stored `map[string]TLSInfo`. Apply the expiry thresholds to determine the display string and colour for the TLS column in the Status View. Additionally, when the Info Panel is opened for a service with `tls: true`, trigger `ProbeTLS` for that service immediately and write the result back to the top-level model before rendering the panel.

**Acceptance**: `CollectMetrics` returns populated metrics for a live process; CPU % is `—` on first call and a non-zero value on subsequent calls; unavailable metrics return `—` from `FormatMetrics`, not an error. `ScanComponentRoot` returns correct totals and per-subdirectory sizes for a test directory tree; stopped services return valid `DirUsage` entries; the Dir column is populated for all services in the Status View. `ProbeTLS` against a TLS service returns a populated `TLSInfo` with correct expiry; against a non-TLS service or closed port returns `Available = false`; the TLS column shows correct colour-coded days in the Status View and full certificate details in the Info Panel.

---

### Phase 7 — Status View

**Goal**: Full-screen status table with keyboard navigation and service controls.

**Files**: `statusview.go`, `styles.go`, `model.go`

**Tasks**:
1. `StatusViewModel` struct: `[]ServiceStatus`, `map[string]ProcessMetrics`, cursor index, status-bar message.
2. `View()` renders the header, table (cursor row highlighted, RUNNING in green, STOPPED in yellow, metric columns populated from `ProcessMetrics`), and keybinding legend.
3. Handle key messages in `Update`:
   - `↑`/`↓` — move cursor.
   - `s` — `StartService`; trigger `RefreshAll`.
   - `K` — `StopService`; trigger `RefreshAll`.
   - `R` — `RestartService`; trigger `RefreshAll`.
   - `r` — trigger `RefreshAll`.
   - `a` / `A` — iterate all entries, call start/stop on applicable ones.
   - `i` — invoke STATUS; switch to Info Panel with captured output.
   - `l` — switch to Log View.
   - `q` — quit.
4. Auto-refresh: `tea.Tick` every 5 seconds calling `RefreshAll`.
5. `styles.go` — Lip Gloss styles.

**Acceptance**: All keybindings function correctly; status updates after start/stop actions.

---

### Phase 8 — Info Panel

**Goal**: Full-screen overlay showing OS-collected process metrics, network connections, component root disk usage, and raw STATUS command output.

**Files**: `infopanel.go`

**Tasks**:
1. `InfoPanelModel` struct: `ServiceEntry`, `ProcessMetrics`, `MemoryLimit`, `NetworkInfo`, `TLSInfo`, `DirUsage`, run-command stdout string, run-command error string.
2. On entry, make up to four concurrent calls: `CollectMetrics(pid)`, `CollectNetwork(pid)`, `Invoke(STATUS)`, and — if `entry.TLS == true` — `ProbeTLS(entry)`. `DirUsage` is taken from the already-collected 30-second scan result in the top-level model. The fresh `TLSInfo` result is written back to the top-level model so the Status View TLS column also reflects the latest probe.
3. `View()` renders sections in order:
   - **Process metrics** — CPU, disk, threads, open files from `ProcessMetrics`.
   - **Memory** — RSS, peak RSS, memory %, swap, growth rate (from ring-buffer delta), configured limit and utilisation (from `MemoryLimit`). Utilisation colour-coded at 80 % and 90 %. Limit row omitted if `MemoryLimit.Detected == false`.
   - **Network** — listen sockets, inbound count, outbound groups (with labels), state counts. Shows `—` for entire section if `NetworkInfo.Available == false`.
   - **TLS certificate** — version, cipher suite, subject, issuer, validity dates, days left (colour-coded), SANs, self-signed flag. Section is omitted entirely if `entry.TLS == false`. Shows `—` if `TLSInfo.Available == false` (service STOPPED or port not reachable).
   - **Component root disk usage** — per-subdirectory sizes from `DirUsage`; `!!` flag on subdirectory exceeding 50% of total.
   - **Service status output** — raw stdout or error from the run command.
4. Any keypress → return to Status View.

**Acceptance**: Pressing `i` shows all sections; Memory section shows growth rate arrow and limit utilisation for a JVM process with `-Xmx`; utilisation colour-codes at 80 % and 90 %; Limit row is absent for a process without a recognised flag; TLS section is present only for `tls: true` services; certificate expiry is colour-coded; outbound connections are grouped with counts and well-known labels; unavailable metrics and network data show `—`; run command errors are displayed gracefully; any key dismisses.

---

### Phase 9 — Log View

**Goal**: Scrollable, live-tailing log pane for a selected service.

**Files**: `logview.go`

**Tasks**:
1. `LogViewModel` struct: service name, active stream, `[]string` lines, scroll offset, follow mode bool.
2. `View()` renders a header, log lines clipped to terminal height, and keybinding legend.
3. File tail: `tea.Tick` every 500 ms; read new bytes from `$COMPONENT_ROOT/logs/{stdout,stderr}.log` (track offset with `io.Seek`), append to buffer. If file absent, display notice and retry.
4. Handle key messages: `↑`/`↓` scroll, `f` follow, `tab` toggle stream, `b`/`esc` back.
5. Wire all view transitions in `model.go`.

**Acceptance**: Log view shows existing content; new lines appear within 500 ms in follow mode; `b` returns to the status table.

---

### Phase 10 — Metrics Panel

**Goal**: A full-screen Metrics Panel showing per-service sparklines for CPU % and RSS, disk I/O rates, and a summary bar — activated by `m` from the Status View.

**Files**: `metricsview.go`, `model.go`

**Tasks**:
1. Define `MetricHistory` — a fixed-capacity ring buffer (capacity 60) storing `ProcessMetrics` samples keyed by service name:
   ```go
   type MetricHistory struct {
       samples [60]ProcessMetrics
       head    int
       count   int
   }
   ```
   Add `map[string]*MetricHistory` to the top-level model, populated on every 5-second tick.
2. `Sparkline(history []float64, width int) string` — scales values to the local min/max range of the slice and maps each sample to a Unicode block character (`▁▂▃▄▅▆▇█`). Returns a string of exactly `width` characters, right-aligned (padded with spaces on the left if fewer than `width` samples). Flat line (`────`) when all values are zero or unavailable.
3. `SummaryBar(metrics map[string]ProcessMetrics) string` — sums CPU % and RSS across all RUNNING services; computes disk read/write rates as the per-second delta between the last two samples (bytes ÷ 5 s interval).
4. `MetricsPanelModel` struct: `[]ServiceStatus`, `map[string]*MetricHistory`, cursor index.
5. `View()` renders:
   - Header line (app root, running/stopped counts, time to next refresh).
   - Summary bar line.
   - Column header row.
   - Horizontal separator.
   - One row per service: service name, CPU sparkline + current value, RSS sparkline + current value, disk read/write rate, uptime. Stopped services rendered with dim colour and `—` or flat line in sparkline columns.
   - Keybinding legend.
6. Handle keys in `Update`: `↑`/`↓` cursor, `s` start, `K` stop (trigger refresh), `tab`/`esc` back to Status View, `q` quit.
7. Wire `m` key in `StatusViewModel.Update` to switch to `MetricsPanel` view. Wire `tab`/`esc` in `MetricsPanelModel.Update` to switch back. Share the same `map[string]*MetricHistory` across both views so history is not lost when switching.

**Acceptance**: `m` from the Status View opens the panel; sparklines grow with each tick; stopped services show correctly; summary bar totals update; `tab` returns to the status table; history is preserved across view switches.

---

*End of specification v1.0*
