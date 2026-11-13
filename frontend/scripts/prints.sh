#!/bin/sh
# The screenshot matrix: every screen in every skin, driven with agent-browser against the mock
# backend, by keyboard wherever a step is an action. Each screen is also audited as drawn for 7:1
# text contrast and 3:1 edges and icons (contrast.js), the screens of a call state or an error for
# an icon in sight beside the state's label (a state is never told by color alone), and the main
# screens of each skin for the ring of every control that takes the keyboard's focus (focus.js).
# The results go to <output directory>/contrast.txt, states.txt and focus.txt, and any failure
# makes the run fail. The run ends with twelve controls (a ring taken away on a call and in the menu,
# a ring on a transparent control, a ring of a transparent color, an audit pointed at another screen, an icon hidden, an icon and
# a text in a transparent badge, an icon whose stroke is transparent, an icon whose shapes are
# transparent, in the state check and in the contrast audit, a text on a translucent background): each must be reported, or the
# audits prove nothing. Run from the repository root with the dev server up (see
# docs/ui/go-frontend.md):
#   frontend/scripts/prints.sh [base URL] [output directory] [skins...]
set -eu
audit=$(cat "$(dirname "$0")/contrast.js")
focus=$(cat "$(dirname "$0")/focus.js")
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
	abo eval "$audit" >/dev/null # the arithmetic of what an icon paints
	for label in "$@"; do
		found=$(abo eval "[...document.querySelectorAll('svg')].some((s) => { const b = s.getBoundingClientRect(); return b.width > 0 && b.height > 0 && getComputedStyle(s).visibility !== 'hidden' && window.__aaa.icon(s) !== null && s.parentElement.innerText.trim().startsWith('$label'); })")
		if [ "$found" = true ]; then
			echo "$skin/$screen: icon and label \"$label\"" >>"$out/states.txt"
		else
			echo "$skin/$screen: NO ICON beside \"$label\"" | tee -a "$out/states.txt"
		fi
	done
}
# rings <name> <selector>: the focus ring of every control of the screen in sight. The selector is
# what tells the screen apart: it must be there before and after, or the audit looked at another
# screen and is reported as a failure. Shift is pressed first: it tells the browser a person is at
# the keyboard, so focus is shown as the keyboard shows it, and it changes nothing on the page (a
# Tab would close the menu).
rings() {
	abo eval "$audit" >/dev/null
	ab press Shift
	there=$(abo eval "!!document.querySelector('$2')")
	ring=$(abo eval "$focus")
	still=$(abo eval "!!document.querySelector('$2')")
	ring=${ring#\"}
	ring=${ring%\"}
	count=${ring%% *}
	wrong=${ring#* }
	[ "$wrong" != "$ring" ] || wrong=
	if [ "$there" != true ] || [ "$still" != true ]; then
		echo "$skin/$1: RING not the screen it names ($2)" | tee -a "$out/focus.txt"
	elif [ "${count:-0}" -gt 0 ] && [ -z "$wrong" ]; then
		echo "$skin/$1: $count controls show a ring" >>"$out/focus.txt"
	else
		echo "$skin/$1: RING $count controls; $wrong" | tee -a "$out/focus.txt"
	fi
}
click() { ab eval "document.querySelector('$1').click()"; }
keys() { for k in "$@"; do ab press "$k"; done; }

: >"$out/contrast.txt"
: >"$out/states.txt"
: >"$out/focus.txt"
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
	# Agent access: the permissions and the grant in use, then the audit log open.
	ab eval "document.querySelector('[data-testid=agent-access]').scrollIntoView()"
	shot agent-access
	click '[data-testid=agent-access] button[aria-expanded]'
	ab wait 200
	ab eval "document.querySelector('[data-testid=agent-access] [aria-label=\"Audit log\"]').scrollIntoView({ block: 'end' })"
	shot agent-audit

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

	# The ring of every control that takes focus, on the screens that hold the controls.
	scene ""
	rings home 'section[aria-label="Home"]'
	keys Control+l
	rings keypad 'section[aria-label="Keypad"]'
	scene active
	rings call '[aria-label="Call controls"]'
	click '[aria-label="Call controls"] button:nth-child(3)'
	rings call-keypad '[aria-label="Current call"] .key'
	scene incoming
	rings incoming 'dialog[open] .btn-call'
	scene ""
	ab focus '.tab[aria-selected="true"]'
	keys ArrowRight
	rings calls 'section[aria-label="Calls"]'
	keys ArrowRight
	rings contacts 'section[aria-label="Contacts"]'
	ab eval "[...document.querySelectorAll('button')].find((b) => b.innerText.trim() === 'Add').click()"
	rings contact-edit 'form[aria-label="New contact"]'
	scene ""
	ab focus 'button[aria-haspopup="menu"]'
	keys Enter
	rings menu '[role="menu"]'
	keys Enter
	rings settings 'section[aria-label="Settings"]'
	ab eval "document.querySelector('[data-testid=agent-access]').scrollIntoView()"
	rings agent-access '[data-testid="agent-access"]'
	scene approval
	rings approval 'dialog[open]'
done

# Controls: each audit must report the fault it is there for, or it proves nothing. Every control
# starts from a fresh page, with one rule added to it.
skin=control
fault() { # <scene> <css>
	ab open "$base/?scene=$1"
	ab wait .statusbar
	ab wait 400
	ab eval "{ const s = document.createElement('style'); s.textContent = '$2'; document.head.append(s); }"
}
controls=0
caught() { # <what> <file> <pattern>
	if grep -q "$3" "$2"; then
		controls=$((controls + 1))
	else
		echo "prints: CONTROL FAILED: $1 was not reported"
	fi
}
fault muted '.ctl:focus-visible, .btn:focus-visible { outline: none }'
rings ring-taken-away '[aria-label="Call controls"]'
caught "a ring taken away" "$out/focus.txt" '^control/ring-taken-away: RING [0-9]* controls; no ring'
fault muted '.ctl { opacity: 0 }'
rings ring-transparent '[aria-label="Call controls"]'
caught "a ring on a transparent control" "$out/focus.txt" '^control/ring-transparent: RING [0-9]* controls; [0-9.]* ring'
fault muted '.ctl:focus-visible, .btn:focus-visible { outline-color: rgba(0, 0, 0, 0) }'
rings ring-of-a-transparent-color '[aria-label="Call controls"]'
caught "a ring of a transparent color" "$out/focus.txt" '^control/ring-of-a-transparent-color: RING [0-9]* controls; [0-9.]* ring'
fault "" '.menuitem:focus-visible { outline: none !important }'
ab focus 'button[aria-haspopup="menu"]'
keys Enter
rings menu-ring-taken-away '[role="menu"]'
caught "a ring taken away in the menu" "$out/focus.txt" '^control/menu-ring-taken-away: RING [0-9]* controls; no ring'
ab focus 'button[aria-haspopup="menu"]'
rings not-the-menu '[role="menu-that-is-not-there"]'
caught "an audit of another screen" "$out/focus.txt" '^control/not-the-menu: RING not the screen'
fault muted '.badge svg { display: none }'
state icon-hidden "Muted"
caught "a hidden icon" "$out/states.txt" '^control/icon-hidden: NO ICON'
fault muted '.badge { opacity: 0 }'
state icon-in-a-transparent-badge "Muted"
caught "an icon in a transparent badge" "$out/states.txt" '^control/icon-in-a-transparent-badge: NO ICON'
low=$(abo eval "$audit")
if [ "$low" != '""' ]; then
	controls=$((controls + 1))
	echo "control/text-in-a-transparent-badge: BELOW AAA $low" >>"$out/contrast.txt"
else
	echo "prints: CONTROL FAILED: text in a transparent badge was not reported"
fi
fault muted '.badge path { stroke-opacity: 0 }'
state icon-with-a-transparent-stroke "Muted"
caught "an icon whose stroke is transparent" "$out/states.txt" '^control/icon-with-a-transparent-stroke: NO ICON'
fault muted '.badge path { opacity: 0 }'
state icon-with-transparent-shapes "Muted"
caught "an icon whose shapes are transparent" "$out/states.txt" '^control/icon-with-transparent-shapes: NO ICON'
low=$(abo eval "$audit")
if printf '%s' "$low" | grep -q 'icon in'; then
	controls=$((controls + 1))
	echo "control/transparent-shapes: BELOW AAA $low" >>"$out/contrast.txt"
else
	echo "prints: CONTROL FAILED: the contrast of an icon with transparent shapes was not reported"
fi
fault muted '.badge { background: rgba(0, 0, 0, 0.05); color: rgb(255, 255, 255) } :has(> .badge) { background: rgb(255, 255, 255) }'
low=$(abo eval "$audit")
if printf '%s' "$low" | grep -q '[0-9] \\"Muted'; then # the text, not its icon
	controls=$((controls + 1))
	echo "control/text-on-a-translucent-background: BELOW AAA $low" >>"$out/contrast.txt"
else
	echo "prints: CONTROL FAILED: text on a translucent background was not reported"
fi
echo "prints: $out ($(grep -c ': ok$' "$out/contrast.txt") screens pass the contrast audit, $(grep -c 'icon and label' "$out/states.txt") states carry an icon and a label, $(grep -c 'controls show a ring' "$out/focus.txt") screens show a ring on every control, $controls of 12 controls caught)"
[ "$controls" -eq 12 ] && ! grep -v '^control/' "$out/contrast.txt" | grep -q BELOW && ! grep -v '^control/' "$out/states.txt" | grep -q "NO ICON" &&
	! grep -v '^control/' "$out/focus.txt" | grep -q "RING"
