# Starts the server and client dev processes and blocks until both exit.
# `go run`/`bun run dev` exec their real work as a child process, so on
# Ctrl+C (or any exit) both are torn down by PID *tree* (taskkill /T) rather
# than by Stop-Process, which only touches the wrapper we started.

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

$server = Start-Process -FilePath "go" -ArgumentList "run", "./server" -WorkingDirectory $root -NoNewWindow -PassThru
$client = Start-Process -FilePath "bun" -ArgumentList "run", "dev" -WorkingDirectory (Join-Path $root "client") -NoNewWindow -PassThru

Write-Host "server: http://localhost:8787   client: http://localhost:5173"
Write-Host "Ctrl+C to stop"

try {
    Wait-Process -Id $server.Id, $client.Id
}
finally {
    foreach ($processId in @($server.Id, $client.Id)) {
        taskkill /PID $processId /T /F 2>$null | Out-Null
    }
}
