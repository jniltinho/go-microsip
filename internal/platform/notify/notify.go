// Package notify shows the desktop's notification of a call that rings, with Answer and Reject
// where the desktop offers buttons: org.freedesktop.Notifications on Linux, as the Rust candidate
// (LIN-01), and a toast on Windows. The caller's name is the other party's text and is shown as plain text.
package notify

import (
	"fmt"
	"strconv"
	"strings"
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

// toastXML is the Windows notification of a ringing call: the caller, already made plain text,
// and two buttons whose arguments name the call.
func toastXML(call uint64, caller string) string {
	return fmt.Sprintf(`<toast launch="show:%[1]d" duration="long"><visual><binding template="ToastGeneric">`+
		`<text>Incoming call</text><text>%[2]s</text></binding></visual><actions>`+
		`<action content="Answer" arguments="answer:%[1]d" activationType="foreground"/>`+
		`<action content="Reject" arguments="reject:%[1]d" activationType="background"/></actions></toast>`, call, caller)
}

// toastAction reads the arguments of a pressed button back into the call and the decision; a
// click on the notification itself, or anything else, is not a decision.
func toastAction(arguments string) (ports.CallNoticeAction, bool) {
	kind, id, _ := strings.Cut(arguments, ":")
	call, err := strconv.ParseUint(id, 10, 64)
	if err != nil || (kind != "answer" && kind != "reject") {
		return ports.CallNoticeAction{}, false
	}
	return ports.CallNoticeAction{Call: call, Answer: kind == "answer"}, true
}

// request is one thing asked of the desktop.
type request struct {
	call   uint64
	caller string // empty: close the call's notification
	show   bool
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

// Actions delivers the buttons pressed.
func (d *Desktop) Actions() <-chan ports.CallNoticeAction { return d.actions }
