#!/bin/sh
# Screenshots of the window (run inside the build container): the application on Xvfb with a window
# manager, captured with ImageMagick.
#   build/qa/prints.sh <binary> <output directory>
set -eu
bin=$1
out=$2
mkdir -p "$out"
XDG_RUNTIME_DIR=$(mktemp -d)
HOME=$(mktemp -d) # the desktop entry the application installs stays in the container
export XDG_RUNTIME_DIR HOME DISPLAY=:58
Xvfb :58 -screen 0 480x680x24 >/dev/null 2>&1 &
xvfb=$!
trap 'kill $xvfb 2>/dev/null || :' EXIT
sleep 1
dbus-run-session -- sh -c '
	openbox >/dev/null 2>&1 &
	"$0" >/dev/null 2>&1 &
	app=$!
	sleep 6
	import -window root "$1/window.png"
	kill $app 2>/dev/null
' "$bin" "$out"
echo "prints: $out/window.png"
