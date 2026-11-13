package control

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// tempDir is a directory of the test's own.
func tempDir(t *testing.T) string { return t.TempDir() }

func TestTheRuntimeDirectoryIsTheSessionsOwn(t *testing.T) {
	if _, err := RuntimeDir(func(string) string { return "" }); !errors.Is(err, ErrNoRuntimeDir) {
		t.Errorf("no XDG_RUNTIME_DIR: %v", err)
	}
	if _, err := RuntimeDir(func(string) string { return "relative" }); !errors.Is(err, ErrNoRuntimeDir) {
		t.Errorf("a relative XDG_RUNTIME_DIR: %v", err)
	}
	if got, err := RuntimeDir(func(string) string { return "/run/user/1000" }); err != nil || got != "/run/user/1000/go-microsip" {
		t.Errorf("the runtime directory: %q, %v", got, err)
	}
}

// macOS limits a socket's path to 103 bytes and refuses a longer one before anything is made;
// that check is macOS's own. Linux, whose limit is higher, keeps taking such a path.
func TestASocketPathPastTheMacOSLimitIsStillTakenOnLinux(t *testing.T) {
	base := tempDir(t)
	dir := filepath.Join(base, strings.Repeat("d", 104-len(filepath.Join(base, "", SocketFile))-1))
	if got := len(filepath.Join(dir, SocketFile)); got != 104 {
		t.Skipf("the test's own folder gives a path of %d bytes", got)
	}
	endpoint, err := Listen(dir, Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: ModeWindow, Epoch: 1}, &echo{})
	if err != nil {
		t.Fatalf("an owner at 104 bytes: %v", err)
	}
	defer endpoint.Close()
	if client, err := Dial(dir); err != nil {
		t.Errorf("a client at 104 bytes: %v", err)
	} else {
		client.Close()
	}
}
