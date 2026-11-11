#!/bin/sh
# Headless check (run inside the build container): with no display variables, the binary prints
# its version, the doctor report, answers an MCP initialize + tools/list handshake on stdio, and
# refuses a password-like option.
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
# A password-like option is refused with the usage status, before anything parses or echoes it.
for args in "--password s3cret-marker" "doctor --sip-pass=s3cret-marker" "mcp -pass s3cret-marker"; do
	status=0
	# shellcheck disable=SC2086 # the arguments are a word list
	refusal=$("$bin" $args 2>&1) || status=$?
	if [ "$status" -ne 2 ] || echo "$refusal" | grep -q "s3cret-marker" ||
		! echo "$refusal" | grep -q "passwords are not accepted on the command line"; then
		echo "headless: \`$args\` exited $status: $refusal" >&2
		exit 1
	fi
done
echo "headless: version, doctor, MCP handshake and password-argument refusal OK"
