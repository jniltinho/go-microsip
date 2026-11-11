#!/bin/sh
# Lab call placed from the softphone's window (run inside the lab network, from the build image):
# a display with a window manager, PipeWire with its PulseAudio server on a private session bus,
# and the precompiled test that drives the window with the keyboard and the mouse.
#   build/qa/window-call.sh <test binary>     environment: GO_MICROSIP_CALL_BIN, GO_MICROSIP_LAB,
#                                             GO_MICROSIP_PBX, GO_MICROSIP_AUDIO_SECONDS
set -eu
test_binary=$1
HOME=$(mktemp -d)
XDG_RUNTIME_DIR=$(mktemp -d)
DISPLAY=:59
export HOME XDG_RUNTIME_DIR DISPLAY test_binary
# Large enough for the window at twice its size (the 200 % desktop of the scale check).
Xvfb "$DISPLAY" -screen 0 1024x1400x24 >/dev/null 2>&1 &
xvfb=$!
trap 'kill $xvfb 2>/dev/null || :' EXIT
sleep 1
dbus-run-session -- sh -c '
	openbox >/dev/null 2>&1 &
	p0=$!
	pipewire >/dev/null 2>&1 &
	p1=$!
	wireplumber >/dev/null 2>&1 &
	p2=$!
	pipewire-pulse >/dev/null 2>&1 &
	p3=$!
	trap "kill $p0 $p1 $p2 $p3 2>/dev/null" EXIT
	i=0
	until [ -S "$XDG_RUNTIME_DIR/pulse/native" ]; do
		i=$((i + 1))
		[ "$i" -lt 50 ] || { echo "window-call: the audio service did not start" >&2; exit 1; }
		sleep 0.2
	done
	"$test_binary" -test.run TestCallThroughTheWindow -test.v -test.timeout 30m
'
