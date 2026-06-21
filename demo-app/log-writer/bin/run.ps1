#Requires -Version 7
# Griffin run command for log-writer (Windows / PowerShell).
# Usage: pwsh -NonInteractive -File run.ps1 START|STOP|STATUS
#
# Starts a background worker that writes structured log lines every second,
# cycling through INFO / WARN / ERROR levels. Demonstrates Griffin's Log View.

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
        Write-Host "log-writer: already running (pid $(Read-Pid))"
        return
    }

    $null = New-Item -ItemType Directory -Force -Path $LogDir

    $workerScript = Join-Path $ScriptDir "worker.ps1"
    $proc = Start-Process pwsh `
        -ArgumentList @(
            '-NonInteractive', '-File', $workerScript,
            '-StdoutLog', $StdoutLog,
            '-StderrLog', $StderrLog
        ) `
        -WindowStyle Hidden `
        -PassThru

    "$($proc.Id)" | Set-Content $PidFile
    Write-Host "log-writer: started (pid $($proc.Id))"
}

function Invoke-Stop {
    if (-not (Test-Running)) {
        Write-Host "log-writer: not running"
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
    Write-Host "log-writer: stopped (pid $id)"
}

function Get-ServiceStatus {
    if (Test-Running) {
        $id    = Read-Pid
        $lines = 0
        if (Test-Path $StdoutLog) { $lines = (Get-Content $StdoutLog).Count }
        Write-Host "Status:     RUNNING"
        Write-Host "PID:        $id"
        Write-Host "Log lines:  $lines"
        Write-Host "Log file:   $StdoutLog"
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
