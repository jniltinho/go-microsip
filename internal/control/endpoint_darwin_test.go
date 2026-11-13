package control

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// tempDir is a directory of the test's own, with a short path: macOS's own temporary folders
// are long, and a socket's path may hold 103 bytes.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "gm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

var hello = Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: ModeWindow, Epoch: 1}

func TestTheControlDirectoryIsInTheApplicationsFolder(t *testing.T) {
	if _, err := RuntimeDir(func(string) string { return "" }); !errors.Is(err, ErrNoRuntimeDir) {
		t.Errorf("no HOME: %v", err)
	}
	if _, err := RuntimeDir(func(string) string { return "relative" }); !errors.Is(err, ErrNoRuntimeDir) {
		t.Errorf("a relative HOME: %v", err)
	}
	want := "/Users/user/Library/Application Support/go-microsip/control"
	if got, err := RuntimeDir(func(string) string { return "/Users/user" }); err != nil || got != want {
		t.Errorf("the control directory: %q, %v", got, err)
	}
	// The owner makes the application's folder when it is not there yet; a client makes nothing.
	home := tempDir(t)
	dir, _ := RuntimeDir(func(string) string { return home })
	if _, err := Dial(dir); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a client before any owner: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "Library")); err == nil {
		t.Error("the client made folders")
	}
	endpoint, err := Listen(dir, hello, &echo{})
	if err != nil {
		t.Fatalf("an owner in a home without the application's folder: %v", err)
	}
	defer endpoint.Close()
	if client, err := Dial(dir); err != nil {
		t.Errorf("a client of that owner: %v", err)
	} else {
		client.Close()
	}
}

func TestASocketPathMacOSCannotNameIsRefused(t *testing.T) {
	base := tempDir(t)
	// The directory's name is padded so that the socket's whole path has the wanted length,
	// with a name of several bytes a character among it.
	at := func(length int) string {
		fixed := len(filepath.Join(base, "é", SocketFile))
		return filepath.Join(base, "é"+strings.Repeat("d", length-fixed))
	}
	fits, long := at(socketPathLimit), at(socketPathLimit+1)
	if got := len(filepath.Join(fits, SocketFile)); got != socketPathLimit {
		t.Fatalf("the test's own path has %d bytes", got)
	}
	endpoint, err := Listen(fits, hello, &echo{})
	if err != nil {
		t.Fatalf("a path of %d bytes: %v", socketPathLimit, err)
	}
	if client, err := Dial(fits); err != nil {
		t.Errorf("a client at %d bytes: %v", socketPathLimit, err)
	} else {
		client.Close()
	}
	endpoint.Close()
	if _, err := Listen(long, hello, &echo{}); !errors.Is(err, ErrPathTooLong) {
		t.Errorf("an owner at %d bytes: %v", socketPathLimit+1, err)
	}
	if _, err := Dial(long); !errors.Is(err, ErrPathTooLong) {
		t.Errorf("a client at %d bytes: %v", socketPathLimit+1, err)
	}
	if _, err := os.Stat(long); err == nil {
		t.Error("the refused owner made its directory")
	}
}

func TestADirectoryWithAnAccessControlEntryIsRefused(t *testing.T) {
	dir := filepath.Join(tempDir(t), dirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := private(dir); err != nil {
		t.Fatalf("a private directory without a list: %v", err)
	}
	// The mode stays 0700; the entry lets everyone list and enter.
	if out, err := exec.Command("/bin/chmod", "+a", "everyone allow list,search", dir).CombinedOutput(); err != nil {
		t.Fatalf("adding an entry: %v: %s", err, out)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the mode after the entry: %v, %v", info.Mode().Perm(), err)
	}
	if _, err := Listen(dir, hello, &echo{}); !errors.Is(err, ErrInsecure) {
		t.Errorf("an owner in a directory with an entry: %v", err)
	}
	if _, err := Dial(dir); !errors.Is(err, ErrInsecure) {
		t.Errorf("a client in a directory with an entry: %v", err)
	}
	if _, err := WriteGrantFile(dir, IssuedGrant{}); !errors.Is(err, ErrInsecure) {
		t.Errorf("a grant file in a directory with an entry: %v", err)
	}
	// The entry removed, the directory is private again.
	if out, err := exec.Command("/bin/chmod", "-N", dir).CombinedOutput(); err != nil {
		t.Fatalf("removing the list: %v: %s", err, out)
	}
	if err := private(dir); err != nil {
		t.Errorf("after the list was removed: %v", err)
	}
}

func TestALinkWhereTheLockGoesIsNotFollowed(t *testing.T) {
	base := tempDir(t)
	dir := filepath.Join(base, dirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(base, "elsewhere")
	if err := os.WriteFile(elsewhere, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(dir, lockFile)); err != nil {
		t.Fatal(err)
	}
	if endpoint, err := Listen(dir, hello, &echo{}); err == nil {
		endpoint.Close()
		t.Error("an owner took a lock through a symbolic link")
	}
	if text, _ := os.ReadFile(elsewhere); string(text) != "kept" {
		t.Errorf("the file the link names was touched: %q", text)
	}
}

func TestTwoStartsAtOnceLeaveOneOwner(t *testing.T) {
	for round := 0; round < 20; round++ {
		dir := filepath.Join(tempDir(t), dirName)
		const starts = 8
		var (
			wait   sync.WaitGroup
			mu     sync.Mutex
			owners []*Endpoint
			others int
		)
		begin := make(chan struct{})
		for i := 0; i < starts; i++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-begin
				endpoint, err := Listen(dir, hello, &echo{})
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					owners = append(owners, endpoint)
				case errors.Is(err, ErrOwned):
					others++
				default:
					t.Errorf("round %d: a start that neither owns nor finds an owner: %v", round, err)
				}
			}()
		}
		close(begin)
		wait.Wait()
		if len(owners) != 1 || others != starts-1 {
			t.Fatalf("round %d: %d owners and %d that found one, of %d starts", round, len(owners), others, starts)
		}
		// The one owner serves, and the socket is its own: no later start took it away.
		if client, err := Dial(dir); err != nil {
			t.Errorf("round %d: a client of the one owner: %v", round, err)
		} else {
			client.Close()
		}
		owners[0].Close()
	}
}
