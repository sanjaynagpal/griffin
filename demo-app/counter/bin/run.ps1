#Requires -Version 7
# Griffin run command for counter (Windows / PowerShell).
# Usage: pwsh -NonInteractive -File run.ps1 START|STOP|STATUS
#
# Starts a background worker that burns CPU in short arithmetic bursts every
# few seconds, then sleeps. Makes CPU% visible and variable in Griffin's
# Metrics Panel, demonstrating the sparkline behaviour.

param(
    [Parameter(Position = 0)]
    [ValidateSet('START', 'STOP', 'STATUS')]
    [string]$Command = ''
)

if (-not $Command) {
    Write-Error "usage: run.ps1 START|STOP|STATUS"
    exit 1
}

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------
$ScriptDir     = $PSScriptRoot
$ComponentRoot = Split-Path $ScriptDir -Parent
$ServiceName   = Split-Path $ComponentRoot -Leaf
$PidFile       = Join-Path $ComponentRoot "$ServiceName.pid"
$LogDir        = Join-Path $ComponentRoot "logs"
$StdoutLog     = Join-Path $LogDir "stdout.log"
$StderrLog     = Join-Path $LogDir "stderr.log"

# ---------------------------------------------------------------------------
# Read burst config (with safe defaults).
# ---------------------------------------------------------------------------
$burst    = 50000
$interval = 3
$cfgPath  = Join-Path $ComponentRoot "cfg\config.yaml"
if (Test-Path $cfgPath) {
    Get-Content $cfgPath | ForEach-Object {
        if ($_ -match '^\s*iterations_per_burst:\s*(\d+)')   { $burst    = [int]$Matches[1] }
        if ($_ -match '^\s*burst_interval_seconds:\s*(\d+)') { $interval = [int]$Matches[1] }
    }
}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
function Test-Running {
    if (-not (Test-Path $PidFile)) { return $false }
    $id = [int]((Get-Content $PidFile -Raw).Trim())
    return $null -ne (Get-Process -Id $id -ErrorAction SilentlyContinue)
}

function Read-Pid {
    [int]((Get-Content $PidFile -Raw).Trim())
}

# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------
function Invoke-Start {
    if (Test-Running) {
        Write-Host "counter: already running (pid $(Read-Pid))"
        return
    }

    $null = New-Item -ItemType Directory -Force -Path $LogDir

    $workerScript = Join-Path $ScriptDir "worker.ps1"
    $proc = Start-Process pwsh `
        -ArgumentList @(
            '-NonInteractive', '-File', $workerScript,
            '-StdoutLog', $StdoutLog,
            '-StderrLog', $StderrLog,
            '-Burst',     $burst,
            '-Interval',  $interval
        ) `
        -WindowStyle Hidden `
        -PassThru

    "$($proc.Id)" | Set-Content $PidFile
    Write-Host "counter: started (pid $($proc.Id)) burst=$burst interval=${interval}s"
}

function Invoke-Stop {
    if (-not (Test-Running)) {
        Write-Host "counter: not running"
        Remove-Item $PidFile -ErrorAction SilentlyContinue
        return
    }

    $id = Read-Pid
    Stop-Process -Id $id -Force -ErrorAction SilentlyContinue

    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while ([DateTime]::UtcNow -lt $deadline) {
        if (-not (Get-Process -Id $id -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Milliseconds 500
    }

    Remove-Item $PidFile -ErrorAction SilentlyContinue
    Write-Host "counter: stopped (pid $id)"
}

function Get-ServiceStatus {
    if (Test-Running) {
        $id       = Read-Pid
        $bursts   = 0
        $lastLine = ''
        if (Test-Path $StdoutLog) {
            $lines    = Get-Content $StdoutLog
            $bursts   = $lines.Count
            $lastLine = $lines | Select-Object -Last 1
        }
        Write-Host "Status:     RUNNING"
        Write-Host "PID:        $id"
        Write-Host "Bursts:     $bursts"
        Write-Host "Last entry: $(if ($lastLine) { $lastLine } else { '—' })"
    } else {
        Write-Host "Status:     STOPPED"
    }
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------
switch ($Command) {
    'START'  { Invoke-Start }
    'STOP'   { Invoke-Stop }
    'STATUS' { Get-ServiceStatus }
}
