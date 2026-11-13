package keyring

import (
	"errors"
	"os"
	"testing"
	"time"
)

// On a desktop's own session, as it is found (the VM check, build/vm/check.sh): whatever state
// the desktop's keyring is in, the adapter answers within its time and starts no prompt. The
// answer is logged, not judged: a session logged in without a password has a locked keyring, or
// none yet.
func TestTheDesktopsKeyringAsFound(t *testing.T) {
	if os.Getenv("GO_MICROSIP_KEYRING_TEST") != "desktop" {
		t.Skip("set GO_MICROSIP_KEYRING_TEST=desktop in a desktop session (make go-vm)")
	}
	var k Store
	start := time.Now()
	err := k.Set(testRef, "fictional-desktop-password")
	took := time.Since(start)
	switch {
	case err == nil:
		got, ok, err := k.Get(testRef)
		if err != nil || !ok || got != "fictional-desktop-password" {
			t.Errorf("stored, then read: found %v, error %v", ok, err)
		}
		if err := k.Delete(testRef); err != nil {
			t.Errorf("stored, then removed: %v", err)
		}
		t.Logf("the desktop's keyring, as found, stored, returned and removed a password (%v)", took.Round(time.Millisecond))
	case errors.Is(err, ErrLocked):
		t.Logf("the desktop's keyring, as found, is locked: said so in %v, no prompt left open", took.Round(time.Millisecond))
	default:
		t.Logf("the desktop's keyring, as found, did not take a password: %v (%v)", err, took.Round(time.Millisecond))
	}
	if took > 6*time.Second {
		t.Errorf("the answer took %v, over the adapter's bound of 5 s", took)
	}
}
