// Package notify shows the desktop's notification of a call that rings, with Answer and Reject
// where the desktop offers buttons, and of a call that was missed: org.freedesktop.Notifications on Linux, as the Rust candidate
// (LIN-01), and a toast on Windows. The caller's name is the other party's text and is shown as plain text.
package notify

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// maxCallerChars bounds the caller's text in a notification.
const maxCallerChars = 64

// queue is how many requests wait for the desktop; more are dropped rather than held.
const queue = 8

// callerText makes the other party's text safe to show: control characters and the characters
// that reorder text are removed, the length is bounded, and what a notification server would read
// as markup is escaped. Nothing left means an unknown caller.
func callerText(caller string) string {
	var out strings.Builder
	n := 0
	for _, c := range caller {
		if unicode.IsControl(c) || c == '‎' || c == '‏' || (c >= '‪' && c <= '‮') || (c >= '⁦' && c <= '⁩') {
			continue
		}
		if n == maxCallerChars {
			break
		}
		n++
		switch c {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		default:
			out.WriteRune(c)
		}
	}
	if strings.TrimSpace(out.String()) == "" {
		return "Unknown caller"
	}
	return out.String()
}

// toasts is the Windows notifications of this run whose buttons still decide a call. A
// notification outlives its call and the run that showed it, and a call's number starts again
// with every run: a button counts only when it names this run and a call that still rings.
type toasts struct {
	mu      sync.Mutex
	run     string
	ringing map[uint64]bool
}

// newToasts names this run with system randomness.
func newToasts() *toasts {
	name := make([]byte, 8)
	_, _ = rand.Read(name) // never fails (crypto/rand)
	return &toasts{run: hex.EncodeToString(name), ringing: map[uint64]bool{}}
}

// xml is the notification of a ringing call: the caller, already made plain text, and two
// buttons whose arguments name the run and the call. The call rings from here on.
func (t *toasts) xml(call uint64, caller string) string {
	t.mu.Lock()
	t.ringing[call] = true
	t.mu.Unlock()
	return fmt.Sprintf(`<toast launch="show:%[1]s:%[2]d" duration="long"><visual><binding template="ToastGeneric">`+
		`<text>Incoming call</text><text>%[3]s</text></binding></visual><actions>`+
		`<action content="Answer" arguments="answer:%[1]s:%[2]d" activationType="foreground"/>`+
		`<action content="Reject" arguments="reject:%[1]s:%[2]d" activationType="background"/></actions></toast>`, t.run, call, caller)
}

// closed ends what the call's notification can decide, and reports whether the call had one
// that no button of was pressed: that one is still shown.
func (t *toasts) closed(call uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	shown := t.ringing[call]
	delete(t.ringing, call)
	return shown
}

// pressed reads the arguments of a pressed button back into the call and the decision, once per
// call. A click on the notification itself, a notification of another run or of a call that no
// longer rings, or anything else, is not a decision.
func (t *toasts) pressed(arguments string) (ports.CallNoticeAction, bool) {
	part := strings.Split(arguments, ":")
	if len(part) != 3 || part[1] != t.run || (part[0] != "answer" && part[0] != "reject") {
		return ports.CallNoticeAction{}, false
	}
	call, err := strconv.ParseUint(part[2], 10, 64)
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil || !t.ringing[call] {
		return ports.CallNoticeAction{}, false
	}
	delete(t.ringing, call)
	return ports.CallNoticeAction{Call: call, Answer: part[0] == "answer"}, true
}

// missedXML is the notification of a call nobody answered: the caller, already made plain text,
// and no button.
func missedXML(caller string) string {
	return `<toast><visual><binding template="ToastGeneric"><text>Missed call</text><text>` + caller +
		`</text></binding></visual></toast>`
}

// request is one thing asked of the desktop: show the ringing call, say that it was missed, or,
// with neither, take its notification away.
type request struct {
	call   uint64
	caller string
	show   bool
	missed bool
}

// Desktop implements ports.CallNotice on the session's notification service.
type Desktop struct {
	requests chan request
	actions  chan ports.CallNoticeAction
}

// New starts the notifier. It connects when the first call rings; a session without a
// notification service shows nothing.
func New() *Desktop {
	d := &Desktop{requests: make(chan request, queue), actions: make(chan ports.CallNoticeAction, queue)}
	go d.serve()
	return d
}

// Incoming shows the notification of the ringing call.
func (d *Desktop) Incoming(call uint64, caller string) {
	select {
	case d.requests <- request{call: call, caller: callerText(caller), show: true}:
	default: // the desktop is not keeping up: the window still shows the call
	}
}

// Close takes the call's notification away.
func (d *Desktop) Close(call uint64) {
	select {
	case d.requests <- request{call: call}:
	default:
	}
}

// Missed replaces the call's notification with one that says who called.
func (d *Desktop) Missed(call uint64, caller string) {
	select {
	case d.requests <- request{call: call, caller: callerText(caller), missed: true}:
	default:
	}
}

// Actions delivers the buttons pressed.
func (d *Desktop) Actions() <-chan ports.CallNoticeAction { return d.actions }
