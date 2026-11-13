//go:build unix

// Package lock takes the advisory file lock by which an application keeps one instance per user.
package lock

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// Files implements ports.Locker with flock(2), the lock the Rust candidate takes on its
// instance.lock. The kernel releases it when the process dies.
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
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ports.ErrLocked
		}
		return nil, err
	}
	return func() { file.Close() }, nil // closing the descriptor releases the lock
}
