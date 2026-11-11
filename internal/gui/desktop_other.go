//go:build !linux

package gui

// installDesktopEntry is Linux-only: Windows and macOS take the icon from the executable / app bundle.
func installDesktopEntry([]byte) (bool, error) { return false, nil }
