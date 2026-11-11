#!/bin/sh
# Headless check (run inside the build container): with no display variables, the binary prints
# its version, the doctor report and answers an MCP initialize + tools/list handshake on stdio.
#   build/qa/headless.sh <binary>
set -eu
bin=$1
unset DISPLAY WAYLAND_DISPLAY WAYLAND_SOCKET XDG_RUNTIME_DIR DBUS_SESSION_BUS_ADDRESS
"$bin" version
"$bin" doctor --json
# The client keeps its end open while it waits for the answers, as a real one does.
out=$({
	printf '%s\n' \
		'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"headless-check","version":"0"}}}' \
		'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
		'{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
	sleep 2
} | timeout 20 "$bin" mcp)
echo "$out" | grep -q '"serverInfo":{[^}]*"name":"go-microsip"' || { echo "headless: no initialize result" >&2; exit 1; }
echo "$out" | grep -q '"name":"status_get"' || { echo "headless: tools/list did not list status_get" >&2; exit 1; }
echo "headless: version, doctor and MCP handshake OK"
