package keyring

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// The Credential Manager: generic credentials of the user, named "go-microsip:<ref>", which
// Windows encrypts with the user's logon. They are outside the application's folder, so they
// survive its removal.
var (
	advapi32   = windows.NewLazySystemDLL("advapi32.dll")
	credRead   = advapi32.NewProc("CredReadW")
	credWrite  = advapi32.NewProc("CredWriteW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

// credential is CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func target(ref string) (*uint16, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(application + ":" + ref)
	if err != nil {
		return nil, ErrInvalidRef
	}
	return name, nil
}

// failure maps a Credential Manager error; none holds a password.
func failure(err error) error {
	if errors.Is(err, windows.ERROR_NO_SUCH_LOGON_SESSION) {
		return ports.ErrNoSecretService // a session without a credential store (a service, a network logon)
	}
	return err
}

// Get returns the password stored under ref; ok is false when there is none.
func (Store) Get(ref string) (password string, ok bool, err error) {
	name, err := target(ref)
	if err != nil {
		return "", false, err
	}
	var found *credential
	if r, _, e := credRead.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&found))); r == 0 {
		if errors.Is(e, windows.ERROR_NOT_FOUND) {
			return "", false, nil
		}
		return "", false, failure(e)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(found)))
	return string(unsafe.Slice(found.CredentialBlob, found.CredentialBlobSize)), true, nil
}

// Set stores or replaces the password under ref.
func (Store) Set(ref, password string) error {
	name, err := target(ref)
	if err != nil {
		return err
	}
	user, _ := windows.UTF16PtrFromString(application)
	blob := []byte(password)
	c := credential{Type: credTypeGeneric, TargetName: name, Persist: credPersistLocalMachine, UserName: user, CredentialBlobSize: uint32(len(blob))}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	if r, _, e := credWrite.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		return failure(e)
	}
	return nil
}

// Delete removes the entry; removing one that does not exist is not an error.
func (Store) Delete(ref string) error {
	name, err := target(ref)
	if err != nil {
		return err
	}
	if r, _, e := credDelete.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0); r == 0 && !errors.Is(e, windows.ERROR_NOT_FOUND) {
		return failure(e)
	}
	return nil
}
