#!/bin/sh
# Lab call through the softphone's binary with real sound (run inside the lab network, from the
# build image): start PipeWire with its PulseAudio server on a private session bus and run the
# precompiled call test against virtual sound cards.
#   build/qa/audio-call.sh <test binary>     environment: GO_MICROSIP_CALL_BIN, GO_MICROSIP_LAB,
#                                            GO_MICROSIP_PBX, GO_MICROSIP_AUDIO_SECONDS
set -eu
test_binary=$1
HOME=$(mktemp -d)
XDG_RUNTIME_DIR=$(mktemp -d)
export HOME XDG_RUNTIME_DIR test_binary
exec dbus-run-session -- sh -c '
	pipewire >/dev/null 2>&1 &
	p1=$!
	wireplumber >/dev/null 2>&1 &
	p2=$!
	pipewire-pulse >/dev/null 2>&1 &
	p3=$!
	trap "kill $p1 $p2 $p3 2>/dev/null" EXIT
	i=0
	until [ -S "$XDG_RUNTIME_DIR/pulse/native" ]; do
		i=$((i + 1))
		[ "$i" -lt 50 ] || { echo "audio-call: the audio service did not start" >&2; exit 1; }
		sleep 0.2
	done
	"$test_binary" -test.run TestCallThroughTheBinary -test.v -test.timeout 30m
'
