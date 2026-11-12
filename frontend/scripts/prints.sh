#!/bin/sh
# The screenshot matrix: every screen in every skin, driven with agent-browser against the mock
# backend, by keyboard wherever a step is an action. Each screen is also audited as drawn for 7:1
# text contrast and 3:1 edges and icons (contrast.js), and the screens of a call state or an error
# for an icon beside the state's label (a state is never told by color alone); the results go to
# <output directory>/contrast.txt and states.txt, and any failure makes the run fail. Run from the repository root with the dev server up (see
# docs/ui/go-frontend.md):
#   frontend/scripts/prints.sh [base URL] [output directory] [skins...]
set -eu
audit=$(cat "$(dirname "$0")/contrast.js")
base=${1:-http://127.0.0.1:5173}
out=$(mkdir -p "${2:-docs/prints/go}" && cd "${2:-docs/prints/go}" && pwd)
[ $# -ge 2 ] && shift 2 || shift $#
skins=${*:-light dark xp macos ubuntu win98 matrix}

# Scroll bars are part of a skin and take room: draw them, as the real window does (headless
# Chromium hides them unless told at launch, so run this in a browser session of its own).
abo() { agent-browser --hide-scrollbars false "$@"; }
ab() { abo "$@" >/dev/null; }
# Loads a mock scene (empty: the idle, registered phone) with the current skin already stored.
scene() {
	ab open "$base/?scene=$1"
	ab wait .statusbar
	ab wait 400
}
shot() {
	ab wait 250
	ab screenshot "$out/$skin/$1.png"
	low=$(abo eval "$audit")
	if [ "$low" = '""' ]; then
		echo "$skin/$1: ok" >>"$out/contrast.txt"
	else
		echo "$skin/$1: BELOW AAA $low" | tee -a "$out/contrast.txt"
	fi
}
# A state is told by an icon and its label together: the label must start the text of an element
# that holds an icon directly.
state() {
	screen=$1
	shift
	for label in "$@"; do
		found=$(abo eval "[...document.querySelectorAll('svg')].some((s) => s.parentElement.innerText.trim().startsWith('$label'))")
		if [ "$found" = true ]; then
			echo "$skin/$screen: icon and label \"$label\"" >>"$out/states.txt"
		else
			echo "$skin/$screen: NO ICON beside \"$label\"" | tee -a "$out/states.txt"
		fi
	done
}
click() { ab eval "document.querySelector('$1').click()"; }
keys() { for k in "$@"; do ab press "$k"; done; }

: >"$out/contrast.txt"
: >"$out/states.txt"
ab set viewport 360 560
# Still pictures: a backdrop that moves (matrix) is drawn at rest, as with reduced motion.
ab set media light reduced-motion
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
	state call-ringing "Ringing"
	ab wait 1600
	keys 5 "#"
	shot call-active
	state call-active "Connected" "Encrypted (SRTP)"
	keys Escape
	ab wait 600

	scene muted
	shot call-muted
	state call-muted "Muted" "Connected"
	scene held
	shot call-held
	state call-held "On hold"
	scene active
	click '[aria-label="Call controls"] button:nth-child(3)'
	shot call-keypad
	click '[aria-label="Call controls"] button:nth-child(4)'
	ab keyboard type "1003"
	shot call-transfer
	scene transfer-failed
	shot call-transfer-failed
	state call-transfer-failed "Transfer failed"
	scene incoming
	shot incoming
	state incoming "Incoming call" "Encrypted (SRTP)"
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
	state error-registration "Failed: rejected (403)"
	scene audio
	shot error-audio
	state error-audio "No audio device"
	scene engine-failed
	shot error-engine
	state error-engine "The phone engine stopped"
	scene no-engine
	shot unavailable
	scene ""
	ab focus 'button[aria-haspopup="menu"]'
	keys Enter End Enter
	shot stopped
done
ab set media light
echo "prints: $out ($(grep -c ': ok$' "$out/contrast.txt") screens pass the contrast audit, $(grep -c 'icon and label' "$out/states.txt") states carry an icon and a label)"
! grep -q BELOW "$out/contrast.txt" && ! grep -q "NO ICON" "$out/states.txt"
