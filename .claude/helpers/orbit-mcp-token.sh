#!/usr/bin/env bash
# headersHelper for the local "orbit-local" MCP server (see .mcp.json).
#
# Claude Code runs this on every connection to the server (and again after a
# 401/403), reading its stdout as the request headers JSON. We emit the current
# platform access token as a bearer header, read fresh from disk each time — so a
# rotated token is picked up transparently without restarting Claude Code.
#
# The token file is produced by `make auth-device-k3d` / `make auth-refresh-k3d`
# (and `make mcp-local`). Override the path with ORBIT_CREDENTIALS if needed.
set -euo pipefail

CREDS="${ORBIT_CREDENTIALS:-/tmp/orbit_credentials.json}"

token=""
if [ -f "$CREDS" ]; then
  token="$(jq -r '.access_token // empty' "$CREDS" 2>/dev/null || true)"
fi

if [ -z "$token" ] || [ "$token" = "null" ]; then
  # No usable token: emit empty headers. The request will 401, which is the
  # clearest signal to run `make mcp-local` (or `make auth-device-k3d`).
  printf '{}\n'
else
  printf '{"Authorization": "Bearer %s"}\n' "$token"
fi
