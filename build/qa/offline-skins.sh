#!/bin/sh
# Offline start of every skin (run inside the build container started with --network none): the
# window opens under Xvfb with each skin saved as the setting, and its own report of what it shows
# (--ui-trace) must name that skin, the whole home screen and the skin's bundled font as loaded. A
# skin that needed a font, an image or a stylesheet from the network would have nothing to fetch
# it with. The built page is also scanned for an address outside itself.
#   build/qa/offline-skins.sh <binary> [directory for a picture of each skin]
set -eu
bin=$1
out=${2:-}
others=$(ls /sys/class/net | grep -v '^lo$' || :)
if [ -n "$others" ]; then
	echo "offline-skins: this container has a network ($others); start it with --network none" >&2
	exit 1
fi
# Nothing in the built page is loaded from outside it: no remote url() (a font's included), script,
# sheet or image, and no imported sheet. (An address in a licence comment loads nothing.)
dist=$(dirname "$0")/../../internal/gui/dist
if grep -rnoE "url\([\"']?(https?:)?//[^)]*|(src|href)=[\"'](https?:)?//[^\"']*|@import[^;]*" \
	"$dist" --include='*.css' --include='*.html' >&2; then
	echo "offline-skins: the built page names something outside itself" >&2
	exit 1
fi
HOME=$(mktemp -d)
XDG_RUNTIME_DIR=$(mktemp -d)
DISPLAY=:58
export HOME XDG_RUNTIME_DIR DISPLAY
Xvfb "$DISPLAY" -screen 0 800x800x24 >/dev/null 2>&1 &
xvfb=$!
trap 'kill $xvfb 2>/dev/null || :' EXIT
sleep 1
[ -z "$out" ] || mkdir -p "$out"
failed=0
# The bundled family each skin draws with (frontend/src/fonts/README.md). The build image has no
# Ubuntu font, so that skin loads its bundled one too.
font_of() {
	case $1 in
	xp) echo "DejaVu Sans" ;;
	win98) echo "Liberation Sans" ;;
	matrix) echo "JetBrains Mono" ;;
	*) echo "Inter" ;;
	esac
}
for skin in light dark xp macos ubuntu win98 matrix; do
	font=$(font_of "$skin")
	dir=$(mktemp -d)
	printf 'schema = 1\n[ui]\ntheme = "%s"\n' "$skin" >"$dir/config.toml"
	chmod 600 "$dir/config.toml"
	trace=$dir/ui-trace.jsonl
	XDG_DATA_HOME=$dir/data XDG_CONFIG_HOME=$dir/config dbus-run-session -- \
		"$bin" --config "$dir/config.toml" --ui-trace "$trace" >"$dir/log" 2>&1 &
	pid=$!
	drawn=no
	i=0
	while [ "$i" -lt 150 ]; do
		# The page's report: this skin drawn, its bundled font loaded, and the four tabs of the home
		# screen in its text.
		if grep '"page"' "$trace" 2>/dev/null | grep "\"theme\":\"$skin\"" | grep "\"skin\":\"$skin\"" |
			grep -E "\"fonts\":\[[^]]*\"$font\"" |
			grep 'Home' | grep 'Calls' | grep 'Contacts' | grep -q 'Keypad'; then
			drawn=yes
			break
		fi
		i=$((i + 1))
		sleep 0.2
	done
	if [ "$drawn" = yes ] && [ -n "$out" ]; then
		sleep 0.5
		import -window root -trim +repage "$out/$skin.png"
	fi
	kill -TERM "$pid" 2>/dev/null || :
	wait "$pid" 2>/dev/null || :
	if [ "$drawn" = yes ]; then
		echo "offline-skins: $skin drawn with no network, in $font"
	else
		echo "offline-skins: $skin NOT drawn" >&2
		tail -n 5 "$dir/log" >&2
		failed=1
	fi
done
exit "$failed"
