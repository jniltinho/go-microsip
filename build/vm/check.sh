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
pactl info | grep -E '^Server (Name|Version)' | sed 's/^/vm-check: /'
GO_MICROSIP_AUDIO_TEST=1 GO_MICROSIP_AUDIO_SECONDS=$seconds /dist/g0/audio.test \
	-test.v -test.timeout 20m -test.run 'TestPulseLoopback|TestNoService' 2>&1 |
	grep -E 'pulse_linux_test|^(--- |PASS|FAIL|ok)' | sed 's/^/vm-check: audio: /'
echo "vm-check: audio adapter OK on the distribution's audio service"

# 3. The window on the distribution's own compositor: GNOME Shell, Wayland, headless.
log=$work/shell.log
gnome-shell --headless --wayland --wayland-display g0-wayland --virtual-monitor 1280x800 >"$log" 2>&1 &
shell=$!
trap 'kill $shell 2>/dev/null || :' EXIT
i=0
until [ -S "$XDG_RUNTIME_DIR/g0-wayland" ]; do
	i=$((i + 1))
	if [ "$i" -gt 100 ]; then
		echo "vm-check: FAIL GNOME Shell did not start" >&2
		tail -n 20 "$log" >&2
		exit 1
	fi
	sleep 0.3
done
gnome-shell --version | sed 's/^/vm-check: /'
if WAYLAND_DISPLAY=g0-wayland XDG_SESSION_TYPE=wayland timeout 150 "$work/app.AppImage" --self-test >"$work/app.log" 2>&1; then
	echo "vm-check: window opened on GNOME/Wayland and the page called the backend"
else
	echo "vm-check: FAIL self-test exit $?" >&2
	tail -n 25 "$work/app.log" >&2
	exit 1
fi
echo "vm-check: PASS $PRETTY_NAME"
