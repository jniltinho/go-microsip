//go:build linux || windows

package secrets

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// These tests talk to a real keyring: the Secret Service of a session bus on Linux (the build
// image starts GNOME Keyring for them: make go-secrets), the Credential Manager on Windows (the
// Windows VM runs them in its desktop session). GO_MICROSIP_SECRETS_TEST says which case the
// session offers: "keyring" (unlocked), or "none" (a bus with no Secret Service).

const testRef = "go-microsip/test-0123456789abcdef"

func TestKeyringStoresReplacesAndDeletes(t *testing.T) {
	if os.Getenv("GO_MICROSIP_SECRETS_TEST") != "keyring" {
		t.Skip("set GO_MICROSIP_SECRETS_TEST=keyring in a session with an unlocked keyring (make go-secrets)")
	}
	var k Keyring
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
	if os.Getenv("GO_MICROSIP_SECRETS_TEST") != "none" {
		t.Skip("set GO_MICROSIP_SECRETS_TEST=none in a session without a Secret Service (make go-secrets)")
	}
	var k Keyring
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
