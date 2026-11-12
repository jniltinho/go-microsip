// Package lock takes the advisory file lock by which an application keeps one instance per user.
package lock

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// Files implements ports.Locker with LockFileEx on the lock file: exclusive and without waiting.
// The system releases it when the process dies.
type Files struct{}

// Lock takes the lock file at path without waiting.
func (Files) Lock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	// The first byte stands for the whole file: every instance locks the same range.
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, new(windows.Overlapped))
	if err != nil {
		file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return nil, ports.ErrLocked
		}
		return nil, err
	}
	return func() { file.Close() }, nil // closing the handle releases the lock
}
