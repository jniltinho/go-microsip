#!/bin/sh
# Run inside a desktop VM as the login user: the milestone artifacts on a clean distribution
# desktop. Prints one "vm-check:" line per step and exits non-zero at the first failure.
#   /vagrant/check.sh <version> [audio seconds]
set -eu
version=$1
seconds=${2:-300}
appimage=/dist/go-microsip-$version-x86_64.AppImage
. /etc/os-release
echo "vm-check: $PRETTY_NAME, kernel $(uname -r)"

# The system libraries the window needs must come with the desktop itself.
for pkg in libgtk-4-1 libwebkitgtk-6.0-4; do
	dpkg-query -W -f='vm-check: ${Package} ${Version} (${db:Status-Abbrev})\n' "$pkg"
done
if apt-mark showmanual | grep -qxE 'libgtk-4-1|libwebkitgtk-6.0-4'; then
	echo "vm-check: FAIL a window library was installed by hand, not by the desktop" >&2
	exit 1
fi

work=$(mktemp -d)
cp "$appimage" "$work/app.AppImage"
chmod +x "$work/app.AppImage"
export APPIMAGE_EXTRACT_AND_RUN=1 # no FUSE needed

# 1. Headless modes with no display at all.
(
	unset DISPLAY WAYLAND_DISPLAY
	"$work/app.AppImage" version
	"$work/app.AppImage" doctor --json | tr -d '\n '
	echo
)
echo "vm-check: headless version and doctor OK"

# 2. The distribution's own audio session (PipeWire with its PulseAudio server).
systemctl --user start pipewire.socket pipewire-pulse.socket wireplumber.service 2>/dev/null || :
audio=0
GO_MICROSIP_AUDIO_TEST=1 GO_MICROSIP_AUDIO_SECONDS=$seconds /dist/g0/audio.test \
	-test.v -test.timeout 20m -test.run 'TestPulseLoopback|TestNoService' >"$work/audio.log" 2>&1 || audio=$?
grep -E 'pulse_linux_test|^(--- |PASS|FAIL|ok|panic)' "$work/audio.log" | sed 's/^/vm-check: audio: /'
failed=
if [ "$audio" -ne 0 ]; then
	echo "vm-check: audio: FAILED (exit $audio); the window is still checked"
	failed="audio"
fi
[ -n "$failed" ] || echo "vm-check: audio adapter OK on the distribution's audio service"

# 3. The window in the distribution's own desktop session (GDM logs the user in automatically; the
# provisioner set that up). The session's display variables are taken from the session itself.
i=0
until session=$(pgrep -u "$(id -u)" -x gnome-shell | head -n 1) && [ -n "$session" ]; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		echo "vm-check: FAIL no desktop session" >&2
		exit 1
	fi
	sleep 2
done
sleep 10 # the shell finishes starting
eval "$(tr '\0' '\n' <"/proc/$session/environ" | grep -E '^(DISPLAY|XAUTHORITY|WAYLAND_DISPLAY|XDG_SESSION_TYPE|XDG_CURRENT_DESKTOP|DBUS_SESSION_BUS_ADDRESS)=' | sed "s/^/export /")"
echo "vm-check: desktop session: $XDG_CURRENT_DESKTOP on $XDG_SESSION_TYPE, $(gnome-shell --version)"

# The shipped AppImage must open its window: as it starts by default, or with --safe-gpu, which a VM
# without 3D acceleration may need. The cgo window backend is tried only to tell a backend problem
# from a system one; it never makes the check pass.
passed=
try() {
	label=$1
	shift
	if timeout -k 5 60 "$@" --self-test >"$work/app.log" 2>&1; then
		echo "vm-check: window: $label: opened and the page called the backend"
		return 0
	fi
	echo "vm-check: window: $label: FAILED (exit $?)"
	tail -n 8 "$work/app.log" | sed 's/^/vm-check:   /'
	return 1
}
if try "AppImage" "$work/app.AppImage"; then
	passed="the default start"
elif try "AppImage --safe-gpu" "$work/app.AppImage" --safe-gpu; then
	passed="--safe-gpu only"
fi
if [ -x /dist/go-microsip-cgo ]; then
	try "diagnostic: cgo backend" /dist/go-microsip-cgo || :
	try "diagnostic: cgo backend --safe-gpu" /dist/go-microsip-cgo --safe-gpu || :
fi
if [ -z "$passed" ]; then
	echo "vm-check: FAIL the AppImage did not open its window" >&2
	exit 1
fi
echo "vm-check: the AppImage window passed with $passed"

# 4. The "System" skin follows the desktop (task 4.6): the window starts on System with a profile of
# its own, the desktop is switched between dark and light, and what the page draws is read from the
# window's own record (--ui-trace). Two seconds is the limit; the record is written every 250 ms.
trace=$work/ui-trace.jsonl
drawn() { grep '"page"' "$trace" 2>/dev/null | tail -n 1 | sed -n 's/.*"theme":"\([a-z0-9]*\)".*/\1/p'; }
follows() { # <desktop color scheme> <skin the page must draw>: prints the milliseconds it took
	gsettings set org.gnome.desktop.interface color-scheme "$1"
	start=$(date +%s%N)
	while [ "$(drawn)" != "$2" ]; do
		[ $((($(date +%s%N) - start) / 1000000)) -lt 6000 ] || return 1
		sleep 0.1
	done
	echo $((($(date +%s%N) - start) / 1000000))
}
gsettings set org.gnome.desktop.interface color-scheme default
GO_MICROSIP_UI_THEME=system XDG_DATA_HOME="$work/data" XDG_CONFIG_HOME="$work/config" \
	"$work/app.AppImage" --ui-trace "$trace" >"$work/system.log" 2>&1 &
system=$!
i=0
until grep -q '"skin":"system"' "$trace" 2>/dev/null; do
	i=$((i + 1))
	[ "$i" -lt 150 ] || break
	sleep 0.2
done
if dark=$(follows prefer-dark dark) && light=$(follows default light) && [ "$dark" -le 2000 ] && [ "$light" -le 2000 ]; then
	echo "vm-check: system skin: followed the desktop to dark in $dark ms and back to light in $light ms"
else
	echo "vm-check: system skin: FAILED: the page draws \"$(drawn)\" (to dark: ${dark:-no} ms, to light: ${light:-no} ms; the limit is 2000)"
	failed="$failed system-skin"
fi
gsettings set org.gnome.desktop.interface color-scheme default
kill "$system" 2>/dev/null || :
# Leave the application open for the screenshot the host takes of the VM's screen.
(setsid "$work/app.AppImage" >/dev/null 2>&1 &)
sleep 6
if [ -n "$failed" ]; then
	echo "vm-check: FAIL $PRETTY_NAME: $failed"
	exit 1
fi
echo "vm-check: PASS $PRETTY_NAME"
