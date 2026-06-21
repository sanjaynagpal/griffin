#Requires -Version 7
# Griffin http-server background worker (Windows / PowerShell).
# Started by run.ps1; do not invoke directly.
#
# Implements a lightweight HTTP file server using System.Net.HttpListener.
# Serves static files from $ServeDir on http://localhost:$Port/.
# Appends access log lines to $StdoutLog; errors go to $StderrLog.

param(
    [Parameter(Mandatory)][string]$StdoutLog,
    [Parameter(Mandatory)][string]$StderrLog,
    [Parameter(Mandatory)][string]$ServeDir,
    [string]$Port = '8080'
)

$null = New-Item -ItemType Directory -Force -Path (Split-Path $StdoutLog -Parent)

$stdout = [System.IO.StreamWriter]::new($StdoutLog, $true)   # append
$stderr = [System.IO.StreamWriter]::new($StderrLog, $true)
$stdout.AutoFlush = $true
$stderr.AutoFlush = $true

$mimeTypes = @{
    '.html' = 'text/html; charset=utf-8'
    '.css'  = 'text/css'
    '.js'   = 'application/javascript'
    '.json' = 'application/json'
    '.txt'  = 'text/plain; charset=utf-8'
    '.png'  = 'image/png'
    '.jpg'  = 'image/jpeg'
    '.svg'  = 'image/svg+xml'
    '.ico'  = 'image/x-icon'
}

$listener = [System.Net.HttpListener]::new()
$listener.Prefixes.Add("http://localhost:$Port/")

try {
    $listener.Start()
    $stdout.WriteLine("$(Get-Date -Format 'o') INFO  listening on http://localhost:$Port/")
    $stdout.WriteLine("$(Get-Date -Format 'o') INFO  serving files from $ServeDir")

    while ($listener.IsListening) {
        $ctx  = $listener.GetContext()
        $req  = $ctx.Request
        $resp = $ctx.Response

        try {
            $urlPath  = $req.Url.AbsolutePath.TrimStart('/')
            if ($urlPath -eq '') { $urlPath = 'index.html' }
            $filePath = Join-Path $ServeDir $urlPath

            if (Test-Path $filePath -PathType Leaf) {
                $content = [System.IO.File]::ReadAllBytes($filePath)
                $ext     = [System.IO.Path]::GetExtension($filePath).ToLower()
                $mime    = if ($mimeTypes[$ext]) { $mimeTypes[$ext] } else { 'application/octet-stream' }

                $resp.ContentType     = $mime
                $resp.ContentLength64 = $content.Length
                $resp.StatusCode      = 200
                $resp.OutputStream.Write($content, 0, $content.Length)
                $stdout.WriteLine("$(Get-Date -Format 'o') INFO  $($req.HttpMethod) /$urlPath 200 $($content.Length)b")
            } else {
                $msg = [System.Text.Encoding]::UTF8.GetBytes("404 Not Found: $urlPath`n")
                $resp.StatusCode      = 404
                $resp.ContentType     = 'text/plain'
                $resp.ContentLength64 = $msg.Length
                $resp.OutputStream.Write($msg, 0, $msg.Length)
                $stdout.WriteLine("$(Get-Date -Format 'o') WARN  $($req.HttpMethod) /$urlPath 404")
            }
        } catch {
            $stderr.WriteLine("$(Get-Date -Format 'o') ERROR request handler: $_")
        } finally {
            $resp.Close()
        }
    }
} catch {
    $stderr.WriteLine("$(Get-Date -Format 'o') ERROR listener: $_")
    exit 1
} finally {
    if ($listener.IsListening) { $listener.Stop() }
    $stdout.Close()
    $stderr.Close()
}
