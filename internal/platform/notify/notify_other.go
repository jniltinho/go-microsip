//go:build !linux

package notify

// shortcut: no notification adapter on this platform yet (Windows toasts arrive with task 6.3):
// requests are read and dropped, and the window, which comes in sight when a call rings, is the
// way to answer.
func (d *Desktop) serve() {
	for range d.requests {
	}
}
