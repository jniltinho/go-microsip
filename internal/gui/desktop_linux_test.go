package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Same cases as the Rust app's writes_refreshes_and_respects_foreign_entries.
func TestInstallDesktop(t *testing.T) {
	root := t.TempDir()
	home, sys := filepath.Join(root, "home"), filepath.Join(root, "sys")
	icon := []byte("png")
	path := filepath.Join(home, desktopEntry)
	install := func(exe string) bool {
		t.Helper()
		ok, err := installDesktop(home, []string{sys}, exe, icon)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	if !install("/opt/a/go-microsip") {
		t.Fatal("first install wrote nothing")
	}
	text, _ := os.ReadFile(path)
	if !strings.Contains(string(text), `Exec="/opt/a/go-microsip" %u`) || !strings.Contains(string(text), "StartupWMClass="+appID) {
		t.Fatalf("entry: %s", text)
	}
	if b, _ := os.ReadFile(filepath.Join(home, desktopIcon)); string(b) != "png" {
		t.Fatal("icon not written")
	}
	// same executable: nothing to do; moved executable: refreshed
	if install("/opt/a/go-microsip") || !install("/opt/b/go-microsip") {
		t.Fatal("refresh")
	}
	// an entry without the marker belongs to someone else
	_ = os.WriteFile(path, []byte("[Desktop Entry]\nExec=go-microsip\n"), 0o644)
	if install("/opt/c/go-microsip") {
		t.Fatal("foreign entry overwritten")
	}
	// a system entry (package install) wins
	_ = os.Remove(path)
	_ = writeAtomic(filepath.Join(sys, desktopEntry), []byte("x"))
	if install("/opt/c/go-microsip") {
		t.Fatal("wrote next to a system entry")
	}
	// a path that would need escaping in Exec is skipped
	if _, ok := desktopText(`/opt/a b/$x`); ok {
		t.Fatal("unsafe Exec path accepted")
	}
}
