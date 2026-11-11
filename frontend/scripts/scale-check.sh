#!/bin/sh
# The window at 100, 150 and 200 % in every skin against the mock backend, driven with agent-browser.
# A scaled desktop gives the window more device pixels and the page the same layout; here the
# viewport grows with the scale and the page is zoomed by it, which is the same for the layout.
# Every check reads the page: nothing wider than the window, no control off its side. Run with the
# dev server up:
#   frontend/scripts/scale-check.sh [base URL] [directory for screenshots]
set -eu
base=${1:-http://127.0.0.1:5173}
out=${2:-}
failed=0

ab() { agent-browser "$@" >/dev/null; }
whole='document.documentElement.scrollWidth <= innerWidth + 1 && [...document.querySelectorAll("button, input, select, [role=status]")].every((e) => { const r = e.getBoundingClientRect(); return r.width === 0 || (r.left >= -1 && r.right <= innerWidth + 1); })'
check() { # <description>
	if [ "$(agent-browser eval "Boolean($whole)")" = true ]; then
		echo "PASS  $1"
	else
		echo "FAIL  $1"
		failed=1
	fi
}

ab open "$base/"
for scale in 100 150 200; do
	ab set viewport $((360 * scale / 100)) $((560 * scale / 100))
	for skin in light dark xp macos ubuntu win98; do
		ab eval "localStorage.setItem('go-microsip.skin', '$skin')"
		ab open "$base/?scene=active"
		ab wait .statusbar
		ab eval "document.documentElement.style.zoom = '$scale%'"
		ab wait 300
		check "$scale % $skin: the dialpad with a call"
		[ -z "$out" ] || [ "$scale" != 150 ] || ab screenshot "$out/scale-150-$skin-call.png"
		ab eval "[...document.querySelectorAll('[role=tab]')].find((t) => t.innerText.trim() === 'Contacts').click()"
		ab wait 200
		check "$scale % $skin: the contacts"
		ab eval "[...document.querySelectorAll('button')].find((b) => b.innerText.trim() === 'Menu').click()"
		ab wait 200
		ab eval "[...document.querySelectorAll('[role=menuitem]')].find((b) => b.innerText.trim() === 'Settings').click()"
		ab wait 200
		check "$scale % $skin: the settings"
	done
done
ab eval "localStorage.clear()"
[ "$failed" -eq 0 ] && echo "scale-check: all passed" || { echo "scale-check: FAILED"; exit 1; }
