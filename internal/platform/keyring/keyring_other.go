//go:build !linux && !windows

package keyring

import "github.com/jniltinho/go-microsip/internal/core/ports"

// shortcut: no keyring adapter on this platform yet (macOS: the Keychain); a password lives for
// the session. Add the adapter with the platform's build.

// Get reports that there is no keyring.
func (Store) Get(string) (string, bool, error) { return "", false, ports.ErrNoSecretService }

// Set reports that there is no keyring.
func (Store) Set(string, string) error { return ports.ErrNoSecretService }

// Delete reports that there is no keyring.
func (Store) Delete(string) error { return ports.ErrNoSecretService }
