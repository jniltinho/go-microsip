package notify

import (
	"runtime"
	"syscall"
	"unsafe"

	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
	"github.com/go-ole/go-ole"
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

// The Windows Runtime interfaces forget uses, which the toast library does not offer: the
// notification manager's history (IToastNotificationManagerStatics2) and the history itself
// (IToastNotificationHistory), by their identifiers and the place of the method in each.
var (
	iidManagerHistory = ole.NewGUID("{7AB93C52-0E48-4750-BA9D-1A4113981847}")
	iidHistory        = ole.NewGUID("{5CADDC63-01D3-4C97-986F-0533483FEE14}")
)

const (
	getHistory  = 6  // IToastNotificationManagerStatics2.get_History
	clearWithID = 12 // IToastNotificationHistory.ClearWithId
)

// method is the address of the nth method of a Windows Runtime object.
func method(o *ole.IInspectable, n int) uintptr {
	return *(*uintptr)(unsafe.Add(unsafe.Pointer(o.RawVTable), uintptr(n)*unsafe.Sizeof(uintptr(0))))
}

// forget takes go-microsip's notifications away, from the screen and from the notification
// center: a call that was answered in the window, refused or is over has nothing left to
// decide, and its toast would otherwise stay, with the keyboard, offering Answer. All of them
// go: the softphone has one call at a time.
func forget() error {
	manager, err := ole.RoGetActivationFactory("Windows.UI.Notifications.ToastNotificationManager", iidManagerHistory)
	if err != nil {
		return err
	}
	defer manager.Release()
	var unknown *ole.IUnknown
	if hr, _, _ := syscall.SyscallN(method(manager, getHistory), uintptr(unsafe.Pointer(manager)), uintptr(unsafe.Pointer(&unknown))); hr != 0 {
		return ole.NewError(hr)
	}
	defer unknown.Release()
	history, err := unknown.QueryInterface(iidHistory)
	if err != nil {
		return err
	}
	defer history.Release()
	app, err := ole.NewHString(toastApp)
	if err != nil {
		return err
	}
	defer func() { _ = ole.DeleteHString(app) }()
	if hr, _, _ := syscall.SyscallN(method((*ole.IInspectable)(unsafe.Pointer(history)), clearWithID), uintptr(unsafe.Pointer(history)), uintptr(app)); hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

// serve shows each ringing call as a toast, takes the toast away when the call stops ringing,
// and passes its buttons on.
func (d *Desktop) serve() {
	// The Windows Runtime is initialized for this thread by the toast library, once, when it
	// shows its first toast, and it takes a thread that is initialized already for a failure:
	// nothing here initializes it. A toast is only ever taken away after one was shown.
	runtime.LockOSThread()
	wintoast.SetActivationCallback(func(_ string, arguments string, _ []wintoast.UserData) {
		if action, pressed := shown.pressed(arguments); pressed {
			select {
			case d.actions <- action:
			default:
			}
		}
	})
	stale := false // a toast that Windows did not take away when asked
	for r := range d.requests {
		// Before the next toast is shown, never after: forgetting takes all of them away.
		if stale || (!r.show && shown.closed(r.call)) {
			stale = forget() != nil // a toast that stays is a nuisance, not a reason to stop
		}
		if r.show {
			_ = show(r.call, r.caller) // without notifications the window is the way to answer
		}
	}
}
