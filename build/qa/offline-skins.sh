#!/bin/sh
# Offline start of every skin (run inside the build container started with --network none): the
# window opens under Xvfb with each skin saved as the setting, and its own report of what it shows
# (--ui-trace) must name that skin, the four destinations of the home screen, every face of the
# skin's bundled font as loaded and none as failed. A skin that needed a font, an image or a
# stylesheet from the network would have nothing to fetch it with. The built page is also scanned
# for an address outside itself. This is a start-up check of one screen per skin, not of every
# screen: the screenshot matrix (frontend/scripts/prints.sh) covers those.
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
if ! [ -f "$dist/index.html" ] || ! ls "$dist"/assets/*.css >/dev/null 2>&1; then
	echo "offline-skins: the built page is not in $dist (run make go-frontend): nothing to scan" >&2
	exit 1
fi
scan=0
grep -rnoE "url\([\"']?(https?:)?//[^)]*|(src|href)=[\"'](https?:)?//[^\"']*|@import[^;]*" \
	"$dist" --include='*.css' --include='*.html' >&2 || scan=$?
case $scan in
1) ;; # nothing found
0)
	echo "offline-skins: the built page names something outside itself" >&2
	exit 1
	;;
*)
	echo "offline-skins: the built page could not be scanned (grep exit $scan)" >&2
	exit 1
	;;
esac
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
# The bundled faces each skin draws its home screen with, as "family weight", separated by "|"
# (frontend/src/fonts/README.md): the regular and the heavier one, or the one variable face. The
# build image has no Ubuntu font, so that skin loads its bundled one too.
faces_of() {
	case $1 in
	xp) echo "DejaVu Sans 400|DejaVu Sans 700" ;;
	win98) echo "Liberation Sans 400|Liberation Sans 700" ;;
	matrix) echo "JetBrains Mono 400|JetBrains Mono 600" ;;
	*) echo "Inter 100 900" ;;
	esac
}
# drawn_in <report line> <faces>: every face is in the report's list of loaded fonts.
drawn_in() {
	rest=$2
	while [ -n "$rest" ]; do
		face=${rest%%|*}
		[ "$face" = "$rest" ] && rest= || rest=${rest#*|}
		printf '%s' "$1" | grep -qE "\"fonts\":\[[^]]*\"$face\"" || return 1
	done
}
for skin in light dark xp macos ubuntu win98 matrix; do
	font=$(faces_of "$skin")
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
		# The page's last report: this skin drawn, every face of its bundled font loaded and none
		# failed, and the four destinations of the home screen in its text.
		report=$(grep '"page"' "$trace" 2>/dev/null | grep "\"theme\":\"$skin\"" | grep "\"skin\":\"$skin\"" |
			grep '"fontErrors":\[\]' | grep 'Home' | grep 'Calls' | grep 'Contacts' | grep 'Keypad' | tail -n 1 || :)
		if [ -n "$report" ] && drawn_in "$report" "$font"; then
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
		echo "offline-skins: $skin drawn with no network, in $font, no font failed"
	else
		echo "offline-skins: $skin NOT drawn" >&2
		tail -n 5 "$dir/log" >&2
		failed=1
	fi
done
exit "$failed"
