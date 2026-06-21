#Requires -Version 7
# Griffin run command for http-server (Windows / PowerShell).
# Usage: pwsh -NonInteractive -File run.ps1 START|STOP|STATUS
#
# Starts a PowerShell HTTP server on port 8080 serving files from cfg/.
# The server runs as a background worker.ps1 process; no Python required.

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
# Paths — derived from this script's location so the service is relocatable.
# ---------------------------------------------------------------------------
$ScriptDir     = $PSScriptRoot
$ComponentRoot = Split-Path $ScriptDir -Parent
$ServiceName   = Split-Path $ComponentRoot -Leaf
$PidFile       = Join-Path $ComponentRoot "$ServiceName.pid"
$LogDir        = Join-Path $ComponentRoot "logs"
$StdoutLog     = Join-Path $LogDir "stdout.log"
$StderrLog     = Join-Path $LogDir "stderr.log"
$ServeDir      = Join-Path $ComponentRoot "cfg"
$Port          = '8080'

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
        Write-Host "http-server: already running (pid $(Read-Pid))"
        return
    }

    $null = New-Item -ItemType Directory -Force -Path $LogDir

    $workerScript = Join-Path $ScriptDir "worker.ps1"
    $proc = Start-Process pwsh `
        -ArgumentList @(
            '-NonInteractive', '-File', $workerScript,
            '-StdoutLog', $StdoutLog,
            '-StderrLog', $StderrLog,
            '-ServeDir',  $ServeDir,
            '-Port',      $Port
        ) `
        -WindowStyle Hidden `
        -PassThru

    "$($proc.Id)" | Set-Content $PidFile
    Write-Host "http-server: started (pid $($proc.Id)) on port $Port"
}

function Invoke-Stop {
    if (-not (Test-Running)) {
        Write-Host "http-server: not running"
        Remove-Item $PidFile -ErrorAction SilentlyContinue
        return
    }

    $id = Read-Pid
    Stop-Process -Id $id -Force -ErrorAction SilentlyContinue

    # Wait up to 5 s for the process to exit.
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while ([DateTime]::UtcNow -lt $deadline) {
        if (-not (Get-Process -Id $id -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Milliseconds 500
    }

    Remove-Item $PidFile -ErrorAction SilentlyContinue
    Write-Host "http-server: stopped (pid $id)"
}

function Get-ServiceStatus {
    if (Test-Running) {
        $id = Read-Pid
        Write-Host "Status:  RUNNING"
        Write-Host "PID:     $id"
        Write-Host "Port:    $Port"
        Write-Host "Serving: $ServeDir"
        Write-Host "Logs:    $LogDir"
    } else {
        Write-Host "Status:  STOPPED"
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
