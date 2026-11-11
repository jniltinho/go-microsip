#!/bin/sh
# Window check (run inside the build container): under Xvfb and a private session bus, the window
# opens, the embedded page calls Go, and --self-test exits 0.
#   build/qa/self-test.sh <binary> [extra flags]
set -eu
bin=$1
shift
XDG_RUNTIME_DIR=$(mktemp -d)
export XDG_RUNTIME_DIR
Xvfb :57 -screen 0 1280x800x24 >/dev/null 2>&1 &
xvfb=$!
trap 'kill $xvfb 2>/dev/null || :' EXIT
sleep 1
log=$(mktemp)
if DISPLAY=:57 dbus-run-session timeout 120 "$bin" --self-test "$@" >"$log" 2>&1; then
	echo "self-test: window opened and the page called the backend"
else
	rc=$?
	echo "self-test: exit $rc" >&2
	grep -m20 -nE '^(SIG|fatal error|panic|goroutine [0-9]+ \[running)|Gtk-|Gdk-|WebKit|EGL|MESA|bwrap|rror' "$log" >&2 || :
	tail -n 15 "$log" >&2
	exit 1
fi
