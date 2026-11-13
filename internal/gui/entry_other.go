//go:build !linux && !windows

package gui

// installDesktopEntry: nothing to install on this platform, which takes the icon from the app
// bundle.
func installDesktopEntry([]byte) (bool, error) { return false, nil }
