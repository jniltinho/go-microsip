//go:build !linux && !windows

package notify

// No notification adapter on this platform: requests are read and dropped, and the window,
// which comes in sight when a call rings, is the way to answer.
func (d *Desktop) serve() {
	for range d.requests {
	}
}
