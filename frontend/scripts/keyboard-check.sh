#!/bin/sh
# Keyboard and behavior walkthrough of the window against the mock backend, driven with
# agent-browser: every step is a key press, every check reads the page. Run with the dev server up:
#   frontend/scripts/keyboard-check.sh [base URL]
# The dev server restarts when a file under frontend/ changes, and the page reloads with it: do not
# edit anything there while this runs, and wait a few seconds after an edit.
set -eu
base=${1:-http://127.0.0.1:5173}
failed=0

ab() { agent-browser "$@" >/dev/null; }
keys() { for k in "$@"; do ab press "$k"; done; }
scene() {
	ab open "$base/?scene=$1"
	ab wait .statusbar
	ab wait 300
}
# check <description> <JavaScript expression that must be true>
check() {
	if [ "$(agent-browser eval "Boolean($2)")" = true ]; then
		echo "PASS  $1"
	else
		echo "FAIL  $1"
		failed=1
	fi
}
# reach <description> <JavaScript expression>: press Tab until it is true, 25 times at most; the
# control is then reached by the keyboard alone, not put in focus by the script.
reach() {
	i=0
	while [ "$(agent-browser eval "Boolean($2)")" != true ] && [ "$i" -lt 25 ]; do
		ab press Tab
		i=$((i + 1))
	done
	check "$1" "$2"
}
focused='document.activeElement'
call='document.querySelector("[aria-label=\"Current call\"]")'

ab set viewport 360 560
ab set media light
ab open "$base/"
ab eval "localStorage.clear()"

scene ""
keys Tab
check "Tab focuses a control and its focus ring is visible" \
	"$focused !== document.body && getComputedStyle($focused).outlineStyle !== 'none' && parseFloat(getComputedStyle($focused).outlineWidth) >= 1"
keys Control+l
check "Ctrl+L focuses the number entry" "$focused.id === 'number'"
ab keyboard type "1002"
keys Enter
ab wait 900
check "Enter dials: the call is ringing" "$call.innerText.includes('Ringing')"
ab wait 1600
check "the call is connected, shown with a label" "$call.innerText.includes('Connected')"
keys 5
check "a digit during the call is DTMF, not typed" "!document.getElementById('number')"
keys Escape
ab wait 600
check "Escape hangs up" "!$call && document.getElementById('number').value === '1002'"

keys Control+l Tab
check "Tab moves from the number to the keypad" "$focused.classList.contains('key')"
keys Space
check "Space presses the focused key" "document.getElementById('number').value === '10021'"

ab focus '.tab[aria-selected="true"]'
keys ArrowRight
check "an arrow key moves to the Calls tab" "document.querySelector('.tab[aria-selected=true]').innerText === 'Calls'"
ab focus '.row'
keys ArrowDown
check "arrow keys move in the history list" "$focused === document.querySelectorAll('.row')[1]"
keys Enter Tab
check "Enter opens a row and Tab reaches its actions" "$focused.innerText === 'Call'"
keys Tab
keys Enter
# The second row is the 35-minute-old call to 1002 (the first is the call this script just made).
check "the row's Edit puts the number in the dialpad without dialing" \
	"document.getElementById('number').value === '1002' && !$call"

# The history's search, and the Contacts tab: list, favorites, a contact's actions, the editor and
# the search, each reached with Tab and arrows from where the keyboard is.
ab focus '.tab[aria-selected="true"]'
keys ArrowRight # the row's Edit went to the dialpad: back to Calls
reach "Tab reaches the history search" "$focused.getAttribute('aria-label') === 'Search calls'"
ab keyboard type "1003"
check "typing in it narrows the history" \
	"[...document.querySelectorAll('.row')].length > 0 && [...document.querySelectorAll('.row')].every((r) => r.innerText.includes('1003') || r.innerText.includes('Bruno'))"
ab focus '.tab[aria-selected="true"]'
keys ArrowRight
check "an arrow key moves to the Contacts tab" "document.querySelector('.tab[aria-selected=true]').innerText === 'Contacts'"
reach "Tab reaches a contact's favorite mark" "($focused.getAttribute('aria-label') || '').startsWith('Favorite: ')"
marked="document.querySelector('[aria-label=\"Favorite: Ana Lima\"]').getAttribute('aria-pressed')"
was=$(agent-browser eval "$marked")
keys Space
ab wait 200
check "Space marks or unmarks the favorite" "$marked !== '$was'"
ab focus '[aria-label="Contact list"] .row'
keys ArrowDown
check "arrow keys move in the contact list" "$focused === document.querySelectorAll('[aria-label=\"Contact list\"] .row')[1]"
keys Enter
reach "Enter opens a contact and Tab reaches its Call" "$focused.innerText.trim().startsWith('Call')"
reach "Tab reaches the contact search" "$focused.getAttribute('aria-label') === 'Search contacts'"
ab keyboard type "bru"
check "typing in it narrows the contacts" "document.querySelectorAll('[aria-label=\"Contact list\"] .row').length === 1"
reach "Tab reaches Add" "$focused.innerText.trim() === 'Add'"
keys Enter
check "Add opens the editor with the focus in the name" \
	"!!document.querySelector('form[aria-label=\"New contact\"]') && $focused.tagName === 'INPUT' && $focused.closest('label').innerText.includes('Name')"
ab keyboard type "Dora Paz"
keys Tab
ab keyboard type "1005"
keys Enter
ab wait 300
check "Enter saves the contact and leaves the editor" "!document.querySelector('form[aria-label=\"New contact\"]')"

ab focus 'button[aria-haspopup="menu"]'
keys Enter
check "Enter opens the menu on its first item" "$focused.innerText.trim() === 'Settings'"
keys ArrowDown
check "arrow keys move in the menu" "$focused.innerText.includes('Do not disturb')"
keys Escape
check "Escape closes the menu and returns the focus" \
	"!document.querySelector('[role=menu]') && $focused.getAttribute('aria-haspopup') === 'menu'"
keys Enter Enter
check "the menu opens Settings" "!!document.querySelector('section[aria-label=Settings]')"

# Skins: live, remembered, System follows the desktop.
ab focus 'input[name="skin"][value="win98"]'
keys Space
check "choosing a skin applies at once" "document.documentElement.dataset.theme === 'win98'"
keys Escape
check "Escape leaves Settings" "!document.querySelector('section[aria-label=Settings]')"
scene active
check "the skin is remembered after a reload" "document.documentElement.dataset.theme === 'win98'"
ab focus 'button[aria-haspopup="menu"]'
keys Enter Enter
ab focus 'input[name="skin"][value="system"]'
keys Space
check "a skin change during a call keeps the call" "document.documentElement.dataset.theme === 'light'"
keys Escape
check "the call is still shown" "$call.innerText.includes('Connected')"
ab set media dark
ab wait 300
check "System follows the desktop to dark without a reload" "document.documentElement.dataset.theme === 'dark'"
ab set media light
ab wait 300
check "and back to light" "document.documentElement.dataset.theme === 'light'"

scene incoming
check "an incoming call opens with the focus on Answer" "$focused.innerText.trim() === 'Answer'"
keys Tab Tab Tab
check "Tab stays inside the dialog" "!!$focused.closest('dialog')"
keys Escape
ab wait 300
check "Escape rejects the call" "!document.querySelector('dialog[open]') && !$call"

scene hostile
check "a hostile caller name is literal text" \
	"!document.querySelector('dialog img, dialog b') && document.querySelector('dialog').innerText.includes('<img src=x onerror=')"
keys Enter
ab wait 300
check "Enter answers" "$call.innerText.includes('Connected') && !$call.querySelector('img, b')"

scene approval
check "an agent request opens with the focus on Deny and Allow disarmed" \
	"$focused.innerText.trim() === 'Deny' && [...document.querySelectorAll('dialog button')].find((b) => b.innerText.trim() === 'Allow').disabled"
ab wait 1200
check "Allow arms after a second" \
	"![...document.querySelectorAll('dialog button')].find((b) => b.innerText.trim() === 'Allow').disabled"
keys Escape
ab wait 300
check "Escape denies: no call is placed" "!document.querySelector('dialog[open]') && !$call"

scene muted
check "a muted call shows an icon and a label" \
	"[...$call.querySelectorAll('.badge')].some((b) => b.innerText.trim() === 'Muted' && b.querySelector('svg'))"
scene held
check "a held call shows an icon and a label" "$call.querySelector('[role=status]').innerText.trim() === 'On hold' && !!$call.querySelector('[role=status] svg')"
scene no-engine
check "without an engine the window says so and offers no call control" \
	"document.body.innerText.includes('Phone engine unavailable') && !document.querySelector('.key, .btn-call')"

ab eval "localStorage.clear()"
[ "$failed" = 0 ] && echo "keyboard-check: all passed" || { echo "keyboard-check: FAILED" >&2; exit 1; }
