package notify

import (
	"runtime"

	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
)

const (
	// toastApp is the name Windows shows the notifications under.
	toastApp = "go-microsip"
	// toastActivator is the class Windows calls back when a button of a notification is pressed,
	// while go-microsip runs. It names no program to start: a notification left in the
	// notification center does nothing once go-microsip is gone.
	toastActivator = "{6F0B1C9E-3D2A-4C57-9A1E-5B7D2F4E8A10}"
)

// shown is this run's notifications; a button of one from an earlier run decides nothing.
var shown = newToasts()

// show registers go-microsip with the notifications of this user, once, and shows the call.
func show(call uint64, caller string) error {
	if err := wintoast.SetAppData(wintoast.AppData{AppID: toastApp, GUID: toastActivator}); err != nil {
		return err
	}
	err := wintoast.Push(toastApp, shown.xml(call, caller))
	if err != nil {
		shown.closed(call)
	}
	return err
}

// serve shows each ringing call as a toast and passes its buttons on.
//
// shortcut: a toast is not taken away when its call is answered, refused or over: it leaves the
// screen by itself, and a button pressed after that decides nothing. Remove it through the
// notification history when the toast library offers that.
func (d *Desktop) serve() {
	runtime.LockOSThread() // the Windows Runtime is initialized for this thread
	wintoast.SetActivationCallback(func(_ string, arguments string, _ []wintoast.UserData) {
		if action, pressed := shown.pressed(arguments); pressed {
			select {
			case d.actions <- action:
			default:
			}
		}
	})
	for r := range d.requests {
		if r.show {
			_ = show(r.call, r.caller) // without notifications the window is the way to answer
		} else {
			shown.closed(r.call)
		}
	}
}
