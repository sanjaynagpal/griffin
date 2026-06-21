# Griffin Demo App

A self-contained demo application with three supervised services that demonstrate Griffin's capabilities.

## Services

| Service | What it does | Demonstrates |
|---|---|---|
| **http-server** | Python HTTP server on port 8080 serving `cfg/` | Port, inbound connections, network info |
| **log-writer** | Writes structured log lines every second | Log View, live tail, varied log levels |
| **counter** | CPU burst loop — sums integers every few seconds | CPU% sparkline, Metrics Panel, variable load |

## Requirements

### Unix / macOS
- **bash** (all services)
- **python3** (http-server only)

### Windows
- **PowerShell 7** (`pwsh`) — the `.ps1` scripts use `#Requires -Version 7`
- No Python needed — `http-server` uses `System.Net.HttpListener` on Windows

## Quick start

### Unix / macOS

#### 1. Make scripts executable

```sh
chmod +x demo-app/http-server/bin/run.sh
chmod +x demo-app/log-writer/bin/run.sh
chmod +x demo-app/counter/bin/run.sh
```

#### 2. Initialise the Griffin registry

```sh
export APP_ROOT=/path/to/griffin/demo-app
griffin init --file "$APP_ROOT/services.yaml"
```

#### 3. Run Griffin

```sh
griffin
```

### Windows (PowerShell)

#### 1. Initialise the Griffin registry

```powershell
$env:APP_ROOT = "C:\path\to\griffin\demo-app"
griffin init --file "$env:APP_ROOT\services.yaml"
```

Griffin auto-selects `run.windows` scripts when running on Windows.

#### 2. Run Griffin

```powershell
griffin
```

## Manual service control (without Griffin)

### Unix / macOS

```sh
demo-app/http-server/bin/run.sh  START|STATUS|STOP
demo-app/log-writer/bin/run.sh   START|STATUS|STOP
demo-app/counter/bin/run.sh      START|STATUS|STOP
```

### Windows (PowerShell)

```powershell
pwsh -NonInteractive -File demo-app\http-server\bin\run.ps1  START
pwsh -NonInteractive -File demo-app\http-server\bin\run.ps1  STATUS
pwsh -NonInteractive -File demo-app\http-server\bin\run.ps1  STOP

pwsh -NonInteractive -File demo-app\log-writer\bin\run.ps1   START
pwsh -NonInteractive -File demo-app\log-writer\bin\run.ps1   STATUS
pwsh -NonInteractive -File demo-app\log-writer\bin\run.ps1   STOP

pwsh -NonInteractive -File demo-app\counter\bin\run.ps1      START
pwsh -NonInteractive -File demo-app\counter\bin\run.ps1      STATUS
pwsh -NonInteractive -File demo-app\counter\bin\run.ps1      STOP
```

## Directory layout

```
demo-app/
├── services.yaml                    ← input for griffin init --file
├── http-server/
│   ├── bin/
│   │   ├── run.sh                   ← Unix run command (START/STOP/STATUS)
│   │   ├── run.ps1                  ← Windows run command
│   │   └── worker.ps1               ← Windows HTTP server worker
│   ├── cfg/
│   │   ├── config.yaml
│   │   └── index.html               ← served at http://localhost:8080
│   └── logs/                        ← created on first START
│       ├── stdout.log
│       └── stderr.log
├── log-writer/
│   ├── bin/
│   │   ├── run.sh
│   │   ├── run.ps1
│   │   └── worker.ps1               ← writes one log line per second
│   ├── cfg/config.yaml
│   └── logs/
│       └── stdout.log               ← one line per second
└── counter/
    ├── bin/
    │   ├── run.sh
    │   ├── run.ps1
    │   └── worker.ps1               ← arithmetic burst loop
    ├── cfg/config.yaml              ← tune burst size and interval here
    └── logs/
        └── stdout.log               ← one line per burst
```

## Griffin run command contract

Each `run.sh` / `run.ps1` follows the contract defined in the Griffin specification:

| Argument | Behaviour |
|---|---|
| `START` | Daemonises the service, writes PID to `$COMPONENT_ROOT/<name>.pid`, returns immediately |
| `STOP` | Terminates the process by PID, waits up to 5 s for exit, removes the PID file |
| `STATUS` | Prints human-readable status to stdout; Griffin displays this verbatim in the Info Panel |

PID files are at `$COMPONENT_ROOT/<service-name>.pid` (e.g. `demo-app/http-server/http-server.pid`).
Logs are written to `$COMPONENT_ROOT/logs/stdout.log` and `stderr.log`.

On Windows, each `run.ps1` launches a hidden `worker.ps1` via `Start-Process pwsh`. The worker owns
the PID that Griffin monitors. Workers append to log files using `System.IO.StreamWriter` in append
mode, so logs accumulate across restarts.

## Tuning the counter service

Edit `counter/cfg/config.yaml` to change the CPU burst behaviour:

```yaml
iterations_per_burst: 50000   # increase for higher CPU%
burst_interval_seconds: 3     # decrease for more frequent bursts
```

Restart the service after editing: `STOP` then `START`.
