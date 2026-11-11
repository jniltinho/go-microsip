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
if [ "$audio" -ne 0 ]; then
	echo "vm-check: FAIL audio exit $audio" >&2
	exit 1
fi
echo "vm-check: audio adapter OK on the distribution's audio service"

# 3. The window in the distribution's own desktop session (GDM logs the user in automatically; the
# provisioner set that up). The session's display variables are taken from the session itself.
i=0
until session=$(pgrep -u "$(id -u)" -f gnome-session-binary | head -n 1) && [ -n "$session" ]; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		echo "vm-check: FAIL no desktop session" >&2
		exit 1
	fi
	sleep 2
done
sleep 10 # the shell finishes starting
eval "$(tr '\0' '\n' <"/proc/$session/environ" | grep -E '^(DISPLAY|XAUTHORITY|WAYLAND_DISPLAY|XDG_SESSION_TYPE|XDG_CURRENT_DESKTOP)=' | sed "s/^/export /")"
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
# Leave the application open for the screenshot the host takes of the VM's screen.
(setsid "$work/app.AppImage" >/dev/null 2>&1 &)
sleep 6
echo "vm-check: PASS $PRETTY_NAME"
