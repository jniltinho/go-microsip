#!/bin/sh
# What the page asks the network for, one skin at a time, driven with agent-browser against the
# mock backend's development server: every request must go to the page's own origin, and the font
# files a skin asks for and the faces it loaded are listed. This is the browser's log on the
# machine that runs it; the built window with no network at all is `make go-offline-skins`.
#   frontend/scripts/network-log.sh [base URL] [output file]
set -eu
base=${1:-http://127.0.0.1:5173}
out=${2:-docs/prints/go/network.txt}
: >"$out"
# urls: the address of each request, one per line. foreign: those of another origin than the
# page's, judged by where the address starts, not by what it contains.
urls() { grep -oE "https?://[^ ]+" || :; }
foreign() { awk -v own="$base/" 'index($0, own) != 1'; }
# The check checks itself: an address elsewhere that carries the page's own in its query is foreign.
if [ "$(printf 'https://assets.example/font.ttf?from=%s/\n%s/src/main.ts\n' "$base" "$base" | foreign | wc -l)" != 1 ]; then
	echo "network-log: the origin check does not tell a foreign address from the page's own" >&2
	exit 1
fi
failed=0
for skin in light dark xp macos ubuntu win98 matrix; do
	agent-browser open "$base/" >/dev/null
	agent-browser eval "localStorage.setItem('go-microsip.mock.theme','$skin');localStorage.setItem('go-microsip.skin','$skin')" >/dev/null
	agent-browser network requests --clear >/dev/null 2>&1
	agent-browser open "$base/?scene=active" >/dev/null
	agent-browser wait .statusbar >/dev/null
	agent-browser wait 800 >/dev/null
	reqs=$(agent-browser network requests 2>&1)
	total=$(printf '%s\n' "$reqs" | urls | wc -l)
	outside=$(printf '%s\n' "$reqs" | urls | foreign | wc -l)
	files=$(printf '%s\n' "$reqs" | grep -o "fonts/[A-Za-z-]*\.ttf" | sort -u | tr '\n' ' ')
	drawn=$(agent-browser eval "document.documentElement.dataset.theme")
	loaded=$(agent-browser eval "[...new Set([...document.fonts].filter((f) => f.status === 'loaded').map((f) => f.family.replace(/\"/g, '') + ' ' + f.weight))].sort().join(', ')")
	echo "$skin: drawn as $drawn, $total requests, $outside outside the page's own origin; font files requested: ${files:-none}; faces loaded: $loaded" | tee -a "$out"
	[ "$outside" = 0 ] && [ "$drawn" = "\"$skin\"" ] && [ -n "$files" ] || failed=1
done
[ "$failed" -eq 0 ] || { echo "network-log: FAILED"; exit 1; }
echo "network-log: every request of every skin goes to the page's own origin"
