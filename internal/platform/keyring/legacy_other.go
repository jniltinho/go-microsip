//go:build !linux

package keyring

import "github.com/jniltinho/go-microsip/internal/core/ports"

// shortcut: the Rust application's keyring entries are read on Linux only; its Windows credentials
// have their own naming and are read when the Windows import is taken up (task 6.3).

// Get reports that the Rust application's keyring cannot be read here.
func (Legacy) Get(string) (string, bool, error) { return "", false, ports.ErrNoSecretService }
