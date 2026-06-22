# Griffin Demo Guide

Step-by-step walkthrough for demonstrating Griffin using the included demo app. The demo app ships three supervised services that collectively exercise every panel and metric Griffin can display.

---

## Prerequisites

### Linux / macOS
| Requirement | Check |
|---|---|
| Go 1.24+ | `go version` |
| bash | `bash --version` |
| python3 | `python3 --version` (http-server only) |

### Windows
| Requirement | Check |
|---|---|
| Go 1.24+ | `go version` |
| PowerShell 7 (`pwsh`) | `pwsh --version` |

No Python required on Windows — the http-server uses `System.Net.HttpListener`.

---

## 1. Build Griffin

```sh
# Linux / macOS
cd /path/to/griffin
go build -o griffin .
```

```powershell
# Windows
cd C:\path\to\griffin
go build -o griffin.exe .
```

---

## 2. Point Griffin at the demo app

Griffin reads `APP_ROOT` to find the application folder.

```sh
# Linux / macOS
export APP_ROOT=/path/to/griffin/demo-app
```

```powershell
# Windows
$env:APP_ROOT = "C:\path\to\griffin\demo-app"
```

---

## 3. Initialise the service registry

Griffin reads `services.yaml` to build `griffin-registry.yaml`. Run this once per checkout.

```sh
# Linux / macOS
./griffin init --file "$APP_ROOT/services.yaml"
```

```powershell
# Windows
.\griffin.exe init --file "$env:APP_ROOT\services.yaml"
```

Expected output:

```
griffin init: wrote 3 entries, skipped 0
  ✓ counter
  ✓ http-server
  ✓ log-writer
```

This creates `demo-app/griffin-registry.yaml`. Open it to verify the `run.unix` / `run.windows` paths resolve correctly.

---

## 4. Start the demo services

Start all three services before launching Griffin so there is data to observe immediately.

### Linux / macOS

```sh
bash "$APP_ROOT/http-server/bin/run.sh" START
bash "$APP_ROOT/log-writer/bin/run.sh"  START
bash "$APP_ROOT/counter/bin/run.sh"     START
```

### Windows (PowerShell)

```powershell
pwsh -NonInteractive -File "$env:APP_ROOT\http-server\bin\run.ps1" START
pwsh -NonInteractive -File "$env:APP_ROOT\log-writer\bin\run.ps1"  START
pwsh -NonInteractive -File "$env:APP_ROOT\counter\bin\run.ps1"     START
```

Verify each service is running:

```sh
# Linux / macOS
bash "$APP_ROOT/http-server/bin/run.sh" STATUS
bash "$APP_ROOT/log-writer/bin/run.sh"  STATUS
bash "$APP_ROOT/counter/bin/run.sh"     STATUS
```

```powershell
# Windows
pwsh -NonInteractive -File "$env:APP_ROOT\http-server\bin\run.ps1" STATUS
pwsh -NonInteractive -File "$env:APP_ROOT\log-writer\bin\run.ps1"  STATUS
pwsh -NonInteractive -File "$env:APP_ROOT\counter\bin\run.ps1"     STATUS
```

---

## 5. Launch Griffin

```sh
# Linux / macOS
./griffin
```

```powershell
# Windows
.\griffin.exe
```

---

## 6. What to show in Griffin

Walk through each panel of the TUI in this order.

### 6.1 Status View — all services at a glance

The Status View lists every service with its current state (RUNNING / STOPPED), PID, and uptime. All three services should appear RUNNING immediately after step 4.

**What to point out:**
- Status is polled in real time — kill one service manually and Griffin detects it within the polling interval.
- PID is cross-referenced to the OS process table, not just the PID file.

```sh
# Kill http-server from another terminal and watch Griffin react
kill $(cat "$APP_ROOT/http-server/http-server.pid")
```

```powershell
Stop-Process -Id (Get-Content "$env:APP_ROOT\http-server\http-server.pid") -Force
```

### 6.2 Info Panel — per-service details

Select a service in the Status View and open the Info Panel. Griffin invokes the run script with `STATUS` and displays the output verbatim.

**http-server** — shows port, serving directory, log paths.  
**log-writer** — shows line count and log file path.  
**counter** — shows burst count and last logged line.

### 6.3 Log View — live log tail

Select `log-writer` and open the Log View. The log grows at one line per second, cycling through `INFO`, `WARN`, and `ERROR` entries with realistic messages.

**What to point out:**
- Log level colouring: INFO (white), WARN (yellow), ERROR (red).
- The tail follows the file in real time — no manual refresh needed.

### 6.4 Metrics Panel — CPU, memory, disk

Select `counter` and open the Metrics Panel. The counter service deliberately burns CPU in short bursts then sleeps, making the CPU% sparkline visibly non-flat.

**What to point out:**
- CPU% sparkline shows the burst / sleep pattern as peaks and valleys.
- Memory (RSS) shows steady low usage between bursts.
- Disk usage reflects the component root (`counter/`), growing as logs are written.

### 6.5 Network info — connections per service

Select `http-server` in the Metrics Panel or a dedicated Network view. Open a browser tab to `http://localhost:8080` to generate a live connection.

**What to point out:**
- Griffin shows the listening port (8080) and active inbound connections.
- The browser connection appears and disappears in real time.

### 6.6 TLS panel (http-server with TLS)

Out of scope for the demo app in its default configuration (HTTP only). If you configure a reverse-proxy with a TLS certificate in front of the http-server, Griffin can display certificate expiry and cipher info. TLS probing runs once daily or on demand.

---

## 7. Start / stop services from within Griffin

Griffin can invoke `START` and `STOP` on a selected service without leaving the TUI.

1. Select a RUNNING service and press `s` (or the mapped stop key) → Griffin calls `run STOP`, the service transitions to STOPPED.
2. Select a STOPPED service and press `r` (or the mapped start key) → Griffin calls `run START`, the service transitions to RUNNING.

Restart `counter` after editing its config to see the CPU% sparkline change immediately.

---

## 8. Tune the counter service

`counter/cfg/config.yaml` controls burst intensity:

```yaml
iterations_per_burst: 50000   # raise to see higher CPU peaks
burst_interval_seconds: 3     # lower to shorten the valley between peaks
```

After editing:

```sh
# Linux / macOS
bash "$APP_ROOT/counter/bin/run.sh" STOP
bash "$APP_ROOT/counter/bin/run.sh" START
```

```powershell
# Windows
pwsh -NonInteractive -File "$env:APP_ROOT\counter\bin\run.ps1" STOP
pwsh -NonInteractive -File "$env:APP_ROOT\counter\bin\run.ps1" START
```

---

## 9. Stop all services

```sh
# Linux / macOS
bash "$APP_ROOT/http-server/bin/run.sh" STOP
bash "$APP_ROOT/log-writer/bin/run.sh"  STOP
bash "$APP_ROOT/counter/bin/run.sh"     STOP
```

```powershell
# Windows
pwsh -NonInteractive -File "$env:APP_ROOT\http-server\bin\run.ps1" STOP
pwsh -NonInteractive -File "$env:APP_ROOT\log-writer\bin\run.ps1"  STOP
pwsh -NonInteractive -File "$env:APP_ROOT\counter\bin\run.ps1"     STOP
```

---

## Demo service summary

| Service | Platform | What it demonstrates |
|---|---|---|
| **http-server** | Unix: Python3 `http.server` / Windows: `HttpListener` | Port monitoring, inbound connections, static file serving |
| **log-writer** | PowerShell `StreamWriter` / bash `nohup` | Log View, live tail, INFO / WARN / ERROR level colouring |
| **counter** | PowerShell `Stopwatch` loop / bash arithmetic | CPU% sparkline, burst pattern, Metrics Panel |
