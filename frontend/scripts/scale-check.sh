#!/bin/sh
# The window at 100, 150 and 200 % in every skin against the mock backend, driven with agent-browser.
# A scaled desktop gives the window more device pixels and the page the same layout; here the
# viewport grows with the scale and the page is zoomed by it, which is the same for the layout.
# Every check reads the page: nothing wider than the window, no control off its side, no control
# outside a scrolling list above or below the window or under something else, no key or caller
# text cut. The last pass is the minimum window, 320 by 480, at 100 %. Run with the dev
# server up:
#   frontend/scripts/scale-check.sh [base URL] [directory for screenshots]
set -eu
base=${1:-http://127.0.0.1:5173}
out=${2:-}
failed=0

# Scroll bars are part of a skin and take room: draw them, as the real window does (headless
# Chromium hides them unless told at launch, so run this in a browser session of its own).
abo() { agent-browser --hide-scrollbars false "$@"; }
ab() { abo "$@" >/dev/null; }
whole='(() => { const modal = document.querySelector("dialog[open]"); const scrolled = (e) => { for (let p = e.parentElement; p; p = p.parentElement) { const s = getComputedStyle(p); if (/(auto|scroll)/.test(s.overflowY) && p.scrollHeight > p.clientHeight + 1) return true; } return false; }; return document.documentElement.scrollWidth <= innerWidth + 1 && [...document.querySelectorAll("button, input, select, [role=status]")].every((e) => { const r = e.getBoundingClientRect(); if (r.width === 0) return true; if (r.left < -1 || r.right > innerWidth + 1) return false; if ((modal && !modal.contains(e)) || scrolled(e)) return true; if (r.top < -1 || r.bottom > innerHeight + 1) return false; const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2); return !top || e.contains(top) || top.contains(e); }) && [...document.querySelectorAll(".key, .overflow-hidden")].every((e) => e.scrollHeight <= e.clientHeight + 1 && e.clientHeight >= 24); })()'
# The mock keeps the saved skin in its own key and the page a copy for its first paint: both are
# set, and every check also requires the skin it is named for to be the one drawn.
wear() { ab eval "localStorage.setItem('go-microsip.mock.theme', '$1'); localStorage.setItem('go-microsip.skin', '$1')"; }
check() { # <description>
	if [ "$(abo eval "Boolean($whole) && document.documentElement.dataset.theme === '$skin' && document.documentElement.dataset.skin === '$skin'")" = true ]; then
		echo "PASS  $1"
	else
		echo "FAIL  $1"
		failed=1
	fi
}

ab open "$base/"
for scale in 100 150 200; do
	ab set viewport $((360 * scale / 100)) $((560 * scale / 100))
	for skin in light dark xp macos ubuntu win98 matrix; do
		wear "$skin"
		ab open "$base/?scene=active"
		ab wait .statusbar
		ab eval "document.documentElement.style.zoom = '$scale%'"
		ab wait 300
		check "$scale % $skin: the call"
		[ -z "$out" ] || [ "$scale" != 150 ] || ab screenshot "$out/scale-150-$skin-call.png"
		ab eval "[...document.querySelectorAll('[role=tab]')].find((t) => t.innerText.trim() === 'Contacts').click()"
		ab wait 200
		check "$scale % $skin: the contacts"
		ab eval "document.querySelector('button[aria-haspopup=menu]').click()"
		ab wait 200
		ab eval "[...document.querySelectorAll('[role=menuitem]')].find((b) => b.innerText.trim() === 'Settings').click()"
		ab wait 200
		check "$scale % $skin: the settings"
		ab eval "[...document.querySelectorAll('[role=tab]')].find((t) => t.innerText.trim() === 'Home').click()"
		ab wait 200
		check "$scale % $skin: home"
		ab open "$base/"
		ab wait .statusbar
		ab eval "document.documentElement.style.zoom = '$scale%'"
		ab press Control+l
		ab wait 200
		check "$scale % $skin: the keypad"
	done
done
ab set viewport 320 480
for skin in light dark xp macos ubuntu win98 matrix; do
	wear "$skin"
	ab open "$base/?scene=muted"
	ab wait .statusbar
	ab wait 300
	check "320x480 $skin: the call"
	ab eval "document.querySelector('[aria-label=\"Call controls\"] button:nth-child(3)').click()"
	ab wait 200
	check "320x480 $skin: the DTMF keys in the call"
	ab open "$base/?scene=incoming"
	ab wait .statusbar
	ab wait 300
	check "320x480 $skin: the incoming call"
	ab open "$base/"
	ab wait .statusbar
	ab wait 300
	check "320x480 $skin: home"
	ab press Control+l
	ab keyboard type "1002"
	ab wait 200
	check "320x480 $skin: the keypad"
done
ab set viewport 360 560
ab eval "localStorage.clear()"
[ "$failed" -eq 0 ] && echo "scale-check: all passed" || { echo "scale-check: FAILED"; exit 1; }
