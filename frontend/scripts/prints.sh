#!/bin/sh
# The screenshot matrix: every screen in every skin, driven with agent-browser against the mock
# backend, by keyboard wherever a step is an action. Each screen is also audited for text contrast
# as drawn (contrast.js: 7:1 in light and dark, 4.5:1 in the other skins); the results go to
# <output directory>/contrast.txt and any
# failure makes the run fail. Run from the repository root with the dev server up (see
# docs/ui/go-frontend.md):
#   frontend/scripts/prints.sh [base URL] [output directory] [skins...]
set -eu
audit=$(cat "$(dirname "$0")/contrast.js")
base=${1:-http://127.0.0.1:5173}
out=$(mkdir -p "${2:-docs/prints/go}" && cd "${2:-docs/prints/go}" && pwd)
[ $# -ge 2 ] && shift 2 || shift $#
skins=${*:-light dark xp macos ubuntu win98}

ab() { agent-browser "$@" >/dev/null; }
# Loads a mock scene (empty: the idle, registered phone) with the current skin already stored.
scene() {
	ab open "$base/?scene=$1"
	ab wait .statusbar
	ab wait 400
}
shot() {
	ab wait 250
	ab screenshot "$out/$skin/$1.png"
	low=$(agent-browser eval "$audit")
	if [ "$low" = '""' ]; then
		echo "$skin/$1: ok" >>"$out/contrast.txt"
	else
		echo "$skin/$1: BELOW THE FLOOR $low" | tee -a "$out/contrast.txt"
	fi
}
click() { ab eval "document.querySelector('$1').click()"; }
keys() { for k in "$@"; do ab press "$k"; done; }

: >"$out/contrast.txt"
ab set viewport 360 560
for skin in $skins; do
	mkdir -p "$out/$skin"
	ab open "$base/"
	ab eval "localStorage.setItem('go-microsip.mock.theme','$skin');localStorage.setItem('go-microsip.skin','$skin')"

	# Home and its search, then the keypad and a whole call by keyboard: Ctrl+L, type, Enter, DTMF,
	# Escape.
	scene ""
	shot home
	ab keyboard type "a"
	shot home-search
	keys Control+l
	shot dialpad
	ab keyboard type "1002"
	shot dialpad-number
	keys Enter
	ab wait 900
	shot call-ringing
	ab wait 1600
	keys 5 "#"
	shot call-active
	keys Escape
	ab wait 600

	scene muted
	shot call-muted
	scene held
	shot call-held
	scene active
	click '[aria-label="Call controls"] button:nth-child(3)'
	shot call-keypad
	click '[aria-label="Call controls"] button:nth-child(4)'
	ab keyboard type "1003"
	shot call-transfer
	scene transfer-failed
	shot call-transfer-failed
	scene incoming
	shot incoming
	scene hostile
	shot incoming-hostile-name

	# Calls: the tab by arrow key, a row by Enter, then the confirmation.
	scene ""
	ab focus '.tab[aria-selected="true"]'
	keys ArrowRight
	shot calls
	ab focus '.row'
	keys ArrowDown Enter
	shot calls-selected
	click 'section[aria-label="Calls"] > div:last-child button'
	shot confirm

	scene ""
	ab focus '.tab[aria-selected="true"]'
	keys ArrowRight ArrowRight
	shot contacts
	click '.row'
	shot contacts-selected
	click 'section[aria-label="Contacts"] .grid button'
	shot contact-edit

	# Menu and settings by keyboard.
	scene ""
	ab focus 'button[aria-haspopup="menu"]'
	keys Enter
	shot menu
	keys Enter
	shot settings
	ab eval "document.querySelector('section[aria-label=Settings] > div:last-child').scrollTop = 9999"
	shot settings-more

	scene approval
	shot approval
	scene register-failed
	shot error-registration
	scene audio
	shot error-audio
	scene engine-failed
	shot error-engine
	scene no-engine
	shot unavailable
	scene ""
	ab focus 'button[aria-haspopup="menu"]'
	keys Enter End Enter
	shot stopped
done
echo "prints: $out ($(grep -c ': ok$' "$out/contrast.txt") screens pass the contrast audit)"
! grep -q BELOW "$out/contrast.txt"
