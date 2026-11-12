//go:build !linux && !windows

package secrets

import "github.com/jniltinho/go-microsip/internal/core/ports"

// shortcut: no keyring adapter on this platform yet (macOS: the Keychain); a password lives for
// the session. Add the adapter with the platform's build.

// Get reports that there is no keyring.
func (Keyring) Get(string) (string, bool, error) { return "", false, ports.ErrNoSecretService }

// Set reports that there is no keyring.
func (Keyring) Set(string, string) error { return ports.ErrNoSecretService }

// Delete reports that there is no keyring.
func (Keyring) Delete(string) error { return ports.ErrNoSecretService }
