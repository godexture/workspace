# Best-effort cleanup for anything dev-up.ps1 left running -- e.g. after a
# closed terminal skipped its own Ctrl+C teardown. Kills by whatever's
# actually bound to our dev ports rather than by process name/command line,
# since `go run`/`bun run dev`'s real child processes vary by build.

Get-NetTCPConnection -LocalPort 8787, 5173 -ErrorAction SilentlyContinue |
    Select-Object -ExpandProperty OwningProcess -Unique |
    ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
