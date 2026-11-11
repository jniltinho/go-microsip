package memory

import (
	"sync"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// Locks is a ports.Locker without an operating system: one lock per path, held until unlocked.
type Locks struct {
	mu   sync.Mutex
	held map[string]bool
}

// Lock takes the lock of path, or fails with ports.ErrLocked.
func (l *Locks) Lock(path string) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[path] {
		return nil, ports.ErrLocked
	}
	if l.held == nil {
		l.held = map[string]bool{}
	}
	l.held[path] = true
	return func() {
		l.mu.Lock()
		delete(l.held, path)
		l.mu.Unlock()
	}, nil
}

// Held reports whether the lock of path is taken.
func (l *Locks) Held(path string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held[path]
}

// Keyring is a ports.Secrets held in memory.
type Keyring struct {
	// Unavailable makes every call fail with ports.ErrNoSecretService.
	Unavailable bool

	mu      sync.Mutex
	entries map[string]string
}

// Get returns the password stored under ref.
func (k *Keyring) Get(ref string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Unavailable {
		return "", false, ports.ErrNoSecretService
	}
	password, ok := k.entries[ref]
	return password, ok, nil
}

// Set stores the password under ref.
func (k *Keyring) Set(ref, password string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Unavailable {
		return ports.ErrNoSecretService
	}
	if k.entries == nil {
		k.entries = map[string]string{}
	}
	k.entries[ref] = password
	return nil
}

// Delete removes the entry under ref.
func (k *Keyring) Delete(ref string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.Unavailable {
		return ports.ErrNoSecretService
	}
	delete(k.entries, ref)
	return nil
}

// Refs lists the references that hold a password, for tests that check nothing was left behind.
func (k *Keyring) Refs() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	refs := make([]string, 0, len(k.entries))
	for ref := range k.entries {
		refs = append(refs, ref)
	}
	return refs
}
