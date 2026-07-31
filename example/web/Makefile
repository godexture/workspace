# Local dev environment: runs the server and client straight from source
# (no Docker). See compose.yaml to instead preview the production server
# image.
#
#   make up    starts server (:8787) and client (:5173) in the foreground
#   make down  stops anything `up` left running (e.g. after a closed shell)
#
# Driven through PowerShell rather than shell job control (trap/wait/&) so
# this doesn't depend on which shell `make` happens to invoke on Windows.

.PHONY: up down

up:
	@powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/dev-up.ps1

down:
	@powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/dev-down.ps1
