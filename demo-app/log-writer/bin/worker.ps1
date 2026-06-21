#Requires -Version 7
# Griffin log-writer background worker (Windows / PowerShell).
# Started by run.ps1; do not invoke directly.
#
# Writes one structured log line per second, cycling through ten realistic
# messages and six log levels (INFO ×4, WARN, ERROR). Appends to $StdoutLog.

param(
    [Parameter(Mandatory)][string]$StdoutLog,
    [Parameter(Mandatory)][string]$StderrLog
)

$null = New-Item -ItemType Directory -Force -Path (Split-Path $StdoutLog -Parent)

$stdout = [System.IO.StreamWriter]::new($StdoutLog, $true)   # append
$stdout.AutoFlush = $true

$levels = @('INFO', 'INFO', 'INFO', 'INFO', 'WARN', 'ERROR')

$messages = @(
    'request processed       path=/api/health        latency=2ms'
    'cache hit               key=user:session:4821   ratio=0.94'
    'queue flushed           items=17                elapsed=3ms'
    'background task done    job=cleanup             duration=120ms'
    'latency spike detected  endpoint=/api/data      latency=248ms'
    'connection timeout      host=db.internal:5432   attempt=3'
    'request processed       path=/api/metrics       latency=1ms'
    'index rebuilt           docs=14821              elapsed=4.2s'
    'rate limit approached   client=10.0.1.4         usage=87%'
    'retry succeeded         host=cache.internal     attempt=2'
)

try {
    $tick = 0
    while ($true) {
        $lvl = $levels[$tick % $levels.Count]
        $msg = $messages[$tick % $messages.Count]
        $stdout.WriteLine("$(Get-Date -Format 'o') $($lvl.PadRight(5)) $msg")
        $tick++
        Start-Sleep -Seconds 1
    }
} catch {
    $stderr = [System.IO.StreamWriter]::new($StderrLog, $true)
    $stderr.AutoFlush = $true
    $stderr.WriteLine("$(Get-Date -Format 'o') ERROR $_")
    $stderr.Close()
} finally {
    $stdout.Close()
}
