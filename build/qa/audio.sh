#!/bin/sh
# Audio adapter check (run inside the build container): start PipeWire with its PulseAudio server
# on a private session bus and run the adapter conformance test against virtual sound cards.
#   build/qa/audio.sh [seconds]      default 10; the G0 threshold run is 300
set -eu
XDG_RUNTIME_DIR=$(mktemp -d)
export XDG_RUNTIME_DIR GO_MICROSIP_AUDIO_TEST=1 GO_MICROSIP_AUDIO_SECONDS="${1:-10}"
exec dbus-run-session -- sh -c '
	pipewire >/dev/null 2>&1 &
	p1=$!
	wireplumber >/dev/null 2>&1 &
	p2=$!
	pipewire-pulse >/dev/null 2>&1 &
	p3=$!
	trap "kill $p1 $p2 $p3 2>/dev/null" EXIT
	i=0
	until pactl info >/dev/null 2>&1; do
		i=$((i + 1))
		[ "$i" -lt 50 ] || { echo "audio: the audio service did not start" >&2; exit 1; }
		sleep 0.2
	done
	pactl info | grep -E "^Server (Name|Version)"
	CGO_ENABLED=0 go test -count=1 -v -timeout 20m -run "TestPulseLoopback|TestNoService|TestRing" ./internal/platform/audio
'
