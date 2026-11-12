package secrets

import (
	"errors"
	"os"
	"testing"

	"github.com/godbus/dbus/v5"
)

// A locked keyring is never unlocked from here (that needs the desktop's own prompt): reading and
// storing say it is locked, and the prompt the keyring offers is dismissed.
func TestALockedKeyringIsReportedAndNotPrompted(t *testing.T) {
	if os.Getenv("GO_MICROSIP_SECRETS_TEST") != "keyring" {
		t.Skip("set GO_MICROSIP_SECRETS_TEST=keyring in a session with an unlocked keyring (make go-secrets)")
	}
	var k Keyring
	const ref = testRef + "-locked"
	if err := k.Set(ref, "fictional-password"); err != nil {
		t.Fatal(err)
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	var locked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := conn.Object(service, servicePath).Call(serviceIface+".Lock", 0, []dbus.ObjectPath{defaultAlias}).Store(&locked, &prompt); err != nil {
		t.Fatalf("locking the default collection: %v", err)
	}
	if len(locked) == 0 {
		t.Skipf("the keyring did not lock its default collection without a prompt (%s): the locked case cannot be provoked here", prompt)
	}
	if _, ok, err := k.Get(ref); ok || !errors.Is(err, ErrLocked) {
		t.Errorf("reading from a locked keyring: found %v, error %v, want ErrLocked", ok, err)
	}
	if err := k.Set(ref, "another-fictional-password"); !errors.Is(err, ErrLocked) {
		t.Errorf("storing in a locked keyring: %v, want ErrLocked", err)
	}
}
