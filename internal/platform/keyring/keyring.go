// Package keyring keeps passwords in the user's keyring, the store the operating system encrypts
// with the user's login: the freedesktop Secret Service on Linux (GNOME Store, KWallet) and the
// Credential Manager on Windows. Nothing here writes a password to a file or logs one.
package keyring

import (
	"errors"
	"strings"
	"unicode"
)

// application marks go-microsip's entries in the keyring.
const application = "go-microsip"

// maxRef bounds a reference.
const maxRef = 256

// Errors of a keyring. None of them holds a password.
var (
	// ErrLocked: the keyring is locked. Unlocking it needs the desktop's own prompt, which is
	// never started from here: the password then lives for the session only.
	ErrLocked = errors.New("the keyring is locked; unlock it in the desktop's keyring and try again")
	// ErrTimeout: the keyring did not answer in time.
	ErrTimeout = errors.New("the keyring did not answer in time")
	// ErrInvalidRef: the reference is empty, too long or holds a control character.
	ErrInvalidRef = errors.New("invalid keyring reference")
)

// legacyApplication marks the Rust application's entries, which an import reads.
const legacyApplication = "rust-microsip"

// Store implements ports.Secrets on the keyring of the session.
type Store struct{}

// Legacy reads the entries the Rust application left in the keyring, for the import of its
// password. It never writes or deletes.
type Legacy struct{}

func validRef(ref string) error {
	if strings.TrimSpace(ref) == "" || len(ref) > maxRef || strings.ContainsFunc(ref, unicode.IsControl) {
		return ErrInvalidRef
	}
	return nil
}
