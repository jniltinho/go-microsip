//go:build !linux

package gui

import (
	"errors"
	"unsafe"
)

// installDesktopEntry is Linux-only: Windows and macOS take the icon from the executable / app bundle.
func installDesktopEntry([]byte) (bool, error) { return false, nil }

// setWindowIconName is Linux-only, for the same reason.
func setWindowIconName(string) {}

// guardExpected: no guard exists for this platform's webview; the content security policy alone
// confines the page, by the owner's decision of 2026-11-11.
const guardExpected = false

// guardNavigation: the toolkit gives no way to refuse a navigation on this platform yet. The
// content security policy still applies.
func guardNavigation(unsafe.Pointer) bool { return false }

// navigationsBlocked is 0 without a guard.
func navigationsBlocked() int64 { return 0 }

// listeningSockets is not implemented on this platform.
func listeningSockets() ([]string, error) { return nil, errors.ErrUnsupported }
