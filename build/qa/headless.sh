#!/bin/sh
# Headless check (run inside the build container): with no display variables, the binary prints
# its version, the doctor report, answers an MCP initialize + tools/list handshake on stdio,
# refuses a password-like option, and validates, exports and imports a configuration.
#   build/qa/headless.sh <binary>
set -eu
bin=$1
# timeout(1) is not on macOS: the same limit with what both systems have.
limit() {
	seconds=$1
	shift
	if command -v timeout >/dev/null 2>&1; then
		timeout "$seconds" "$@"
	else
		perl -e 'alarm shift; exec @ARGV' "$seconds" "$@"
	fi
}
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
} | limit 20 "$bin" mcp)
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
# The configuration commands work from files alone: a temporary home, a fictional account.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
printf 'schema = 1\n[account]\nusername = "1001"\ndomain = "pbx.invalid"\n' >"$tmp/c.toml"
chmod 600 "$tmp/c.toml"
export HOME="$tmp" XDG_DATA_HOME="$tmp/data" XDG_CONFIG_HOME="$tmp/config"
"$bin" --config "$tmp/c.toml" config validate
"$bin" --config "$tmp/c.toml" config export | grep -q 'username = "1001"'
"$bin" --config "$tmp/c.toml" doctor | grep -q 'account.username = "1001" \[toml\]'
"$bin" config import "$tmp/c.toml" --apply | grep -q "Imported into the local database"
"$bin" doctor --json | grep -q '"source": "sqlite"'
status=0
"$bin" --config "$tmp/c.toml" run >/dev/null 2>"$tmp/err" || status=$?
if [ "$status" -ne 1 ] || ! grep -q "no password is available" "$tmp/err"; then
	echo "headless: run without a password exited $status: $(cat "$tmp/err")" >&2
	exit 1
fi
echo "headless: version, doctor, MCP handshake, password-argument refusal and configuration commands OK"
