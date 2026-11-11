// Package lock takes the advisory file lock by which an application keeps one instance per user.
package lock

import (
	"errors"
	"fmt"
)

// Files implements ports.Locker on Windows.
//
// shortcut: not implemented; the Windows single instance and the import from the Rust application
// on Windows arrive with the Windows adapters (M4). Until then nothing that needs the lock runs.
type Files struct{}

// Lock reports that the lock is not available on Windows yet.
func (Files) Lock(string) (func(), error) {
	return nil, fmt.Errorf("the instance lock is not implemented on Windows yet: %w", errors.ErrUnsupported)
}
