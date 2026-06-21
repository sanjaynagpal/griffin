#Requires -Version 7
# Griffin counter background worker (Windows / PowerShell).
# Started by run.ps1; do not invoke directly.
#
# Runs a tight arithmetic loop summing integers 1..N, then sleeps. Each burst
# result is logged with elapsed time and a running grand total. Appends to
# $StdoutLog so the log grows across restarts.

param(
    [Parameter(Mandatory)][string]$StdoutLog,
    [Parameter(Mandatory)][string]$StderrLog,
    [int]$Burst    = 50000,
    [int]$Interval = 3
)

$null = New-Item -ItemType Directory -Force -Path (Split-Path $StdoutLog -Parent)

$stdout = [System.IO.StreamWriter]::new($StdoutLog, $true)   # append
$stdout.AutoFlush = $true

try {
    $run        = 0
    $grandTotal = [long]0

    while ($true) {
        $sw = [System.Diagnostics.Stopwatch]::StartNew()

        # CPU burst — pure integer arithmetic, no subprocesses.
        $acc = [long]0
        for ($i = 1; $i -le $Burst; $i++) { $acc += $i }

        $sw.Stop()
        $run++
        $grandTotal += $acc

        $stdout.WriteLine(
            "$(Get-Date -Format 'o') INFO  burst={0,-4} n={1,-6} sum={2,-13} elapsed={3:F0}ms  total={4}" -f
            $run, $Burst, $acc, $sw.Elapsed.TotalMilliseconds, $grandTotal
        )

        Start-Sleep -Seconds $Interval
    }
} catch {
    $errWriter = [System.IO.StreamWriter]::new($StderrLog, $true)
    $errWriter.AutoFlush = $true
    $errWriter.WriteLine("$(Get-Date -Format 'o') ERROR $_")
    $errWriter.Close()
} finally {
    $stdout.Close()
}
