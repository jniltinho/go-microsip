//go:build linux || windows

package keyring

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// These tests talk to a real keyring: the Secret Service of a session bus on Linux (the build
// image starts GNOME Store for them: make go-keyring), the Credential Manager on Windows (the
// Windows VM runs them in its desktop session). GO_MICROSIP_KEYRING_TEST says which case the
// session offers: "keyring" (unlocked), or "none" (a bus with no Secret Service).

const testRef = "go-microsip/test-0123456789abcdef"

func TestKeyringStoresReplacesAndDeletes(t *testing.T) {
	if os.Getenv("GO_MICROSIP_KEYRING_TEST") != "keyring" {
		t.Skip("set GO_MICROSIP_KEYRING_TEST=keyring in a session with an unlocked keyring (make go-keyring)")
	}
	var k Store
	t.Cleanup(func() { _ = k.Delete(testRef) })
	if err := k.Delete(testRef); err != nil {
		t.Fatalf("deleting what is not there: %v", err)
	}
	if _, ok, err := k.Get(testRef); ok || err != nil {
		t.Fatalf("before anything was stored: found %v, error %v", ok, err)
	}
	const first, second = "fictional-first-password", "fictional sécond passwörd \"with\" 'quotes'"
	if err := k.Set(testRef, first); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := k.Get(testRef); !ok || err != nil || got != first {
		t.Fatalf("after storing: found %v, error %v, the password differs: %v", ok, err, got != first)
	}
	if err := k.Set(testRef, second); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := k.Get(testRef); !ok || err != nil || got != second {
		t.Fatalf("after replacing: found %v, error %v, the password differs: %v", ok, err, got != second)
	}
	// Another reference is another entry.
	if _, ok, err := k.Get(testRef + "-other"); ok || err != nil {
		t.Errorf("another reference: found %v, error %v", ok, err)
	}
	if err := k.Delete(testRef); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := k.Get(testRef); ok || err != nil {
		t.Fatalf("after deleting: found %v, error %v", ok, err)
	}
	for _, bad := range []string{"", "   ", "a\nb", strings.Repeat("r", maxRef+1)} {
		if _, _, err := k.Get(bad); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("reference %q: %v, want ErrInvalidRef", bad, err)
		}
		if err := k.Set(bad, "x"); !errors.Is(err, ErrInvalidRef) {
			t.Errorf("storing under %q: %v, want ErrInvalidRef", bad, err)
		}
	}
}

// Without a keyring every call says so, and no error repeats a password.
func TestNoKeyringIsReported(t *testing.T) {
	if os.Getenv("GO_MICROSIP_KEYRING_TEST") != "none" {
		t.Skip("set GO_MICROSIP_KEYRING_TEST=none in a session without a Secret Service (make go-keyring)")
	}
	var k Store
	const password = "fictional-password-that-must-not-appear"
	_, ok, getErr := k.Get(testRef)
	for name, err := range map[string]error{"Get": getErr, "Set": k.Set(testRef, password), "Delete": k.Delete(testRef)} {
		if !errors.Is(err, ports.ErrNoSecretService) {
			t.Errorf("%s without a keyring: %v, want ErrNoSecretService", name, err)
		}
		if err != nil && strings.Contains(err.Error(), password) {
			t.Errorf("%s: the error repeats the password", name)
		}
	}
	if ok {
		t.Error("a password was found without a keyring")
	}
}

// A session bus that accepts the connection and then says nothing does not hold the caller: the
// operation gives up after its bound, connecting included.
func TestASilentBusDoesNotHoldTheCaller(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("GO_MICROSIP_KEYRING_TEST") != "silent" {
		t.Skip("set GO_MICROSIP_KEYRING_TEST=silent with DBUS_SESSION_BUS_ADDRESS at a socket that never answers (make go-keyring)")
	}
	var k Store
	start := time.Now()
	_, _, err := k.Get(testRef)
	took := time.Since(start)
	if !errors.Is(err, ErrTimeout) && !errors.Is(err, ports.ErrNoSecretService) {
		t.Errorf("a bus that says nothing: %v, want a timeout", err)
	}
	if took > 7*time.Second {
		t.Errorf("the caller waited %v for a bus that says nothing", took.Round(time.Millisecond))
	}
}

// A stored password is the user's, not the application folder's: one run leaves it in the
// keyring ("leave"), the application's folder is removed, and a run from another copy finds it
// ("find") and removes it. The VM check does the removing in between (task 6.3).
func TestAStoredPasswordOutlivesTheRunThatLeftIt(t *testing.T) {
	const ref, password = "go-microsip/test-left-behind-0123456789", "fictional-left-behind-password"
	var k Store
	switch os.Getenv("GO_MICROSIP_KEYRING_TEST") {
	case "leave":
		if err := k.Set(ref, password); err != nil {
			t.Fatal(err)
		}
	case "find":
		t.Cleanup(func() { _ = k.Delete(ref) })
		if got, ok, err := k.Get(ref); err != nil || !ok || got != password {
			t.Fatalf("the password left by the run before: found %v, error %v, the same %v", ok, err, got == password)
		}
	default:
		t.Skip("set GO_MICROSIP_KEYRING_TEST=leave, then find (the VM check)")
	}
}
