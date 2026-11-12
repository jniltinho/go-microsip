package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests run in the Windows VM (build/vm/check.ps1): the transport's own claims on the
// system it is for.

type counted struct {
	mu    sync.Mutex
	count int
}

func (c *counted) Handle(r Request, pid int) Response {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
	return OK(r.RequestID, map[string]int{"pid": pid})
}

func TestAClientReachesTheOwnerOnWindows(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "control")
	welcome := Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: ModeWindow, Epoch: 7}
	handler := &counted{}
	if _, err := Dial(dir); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a client without an owner: %v", err)
	}
	endpoint, err := Listen(dir, welcome, handler)
	if err != nil {
		t.Fatal(err)
	}
	client, err := Dial(dir)
	if err != nil {
		t.Fatalf("a client of the owner: %v", err)
	}
	if w := client.Welcome(); w != welcome {
		t.Errorf("the welcome: %+v", w)
	}
	// The owner is told which process asks, by the system and not by the client.
	r := client.Do(Request{SchemaVersion: 1, RequestID: "r1", Operation: Operation{Op: StatusGet}})
	if want := fmt.Sprintf(`{"pid":%d}`, os.Getpid()); r.Status != StatusOK || string(r.Data) != want {
		t.Errorf("the answer: %+v, want data %s", r, want)
	}
	if pid, ours := peer(clientConn(t, client)); !ours || pid != os.Getpid() {
		t.Errorf("the client's view of the owner: process %d, this user's %v", pid, ours)
	}
	// One owner at a time; the next takes over once the first is gone.
	if _, err := Listen(dir, welcome, handler); !errors.Is(err, ErrOwned) {
		t.Errorf("a second owner: %v", err)
	}
	client.Close()
	if err := endpoint.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(dir); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a client after the owner left: %v", err)
	}
	// A socket file left behind by an owner that died is replaced.
	if err := os.WriteFile(filepath.Join(dir, SocketFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(dir); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a client of a file nobody listens on: %v", err)
	}
	welcome.Epoch = 8
	next, err := Listen(dir, welcome, handler)
	if err != nil {
		t.Fatalf("the next owner: %v", err)
	}
	defer next.Close()
	again, err := Dial(dir)
	if err != nil || again.Welcome().Epoch != 8 {
		t.Fatalf("a client of the next owner: %v", err)
	}
	defer again.Close()
	// The other application's hello gets no welcome, and a request without a hello no answer.
	for name, first := range map[string]any{"the other protocol": Hello{Protocol: "rust-microsip-control", Version: 1},
		"a request without a hello": Request{SchemaVersion: 1, RequestID: "r", Operation: Operation{Op: StatusGet}}} {
		conn, err := net.Dial("unix", filepath.Join(dir, SocketFile))
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		_ = WriteMessage(conn, first)
		var answer json.RawMessage
		if err := ReadMessage(bufio.NewReader(conn), &answer); err == nil {
			t.Errorf("%s was answered: %s", name, answer)
		}
		conn.Close()
	}
	if got, err := RuntimeDir(func(string) string { return `C:\Users\someone\AppData\Local` }); err != nil || got != `C:\Users\someone\AppData\Local\go-microsip\control` {
		t.Errorf("the runtime directory: %q, %v", got, err)
	}
	if _, err := RuntimeDir(func(string) string { return "" }); !errors.Is(err, ErrNoRuntimeDir) {
		t.Errorf("no LOCALAPPDATA: %v", err)
	}
}

// clientConn is the connection under a client.
func clientConn(t *testing.T, c *Client) net.Conn {
	t.Helper()
	return c.conn
}

func TestGrantFilesOnWindows(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "control", "grants")
	grant := IssuedGrant{ID: "grant-1-abc", Capability: strings.Repeat("a", 64)}
	path, err := WriteGrantFile(dir, grant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGrantFile(dir, grant); err == nil {
		t.Error("a grant file was written over")
	}
	if got, err := ReadCapability(path, anyone{}); err != nil || got != grant.Capability {
		t.Errorf("the capability read back: %v", err)
	}
	boot, err := WriteGrantFile(dir, IssuedGrant{ID: "grant-1-def", Capability: strings.Repeat("b", 64), BootstrapID: "boot-1-xyz"})
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := ReadBootstrap(boot, anyone{}); err != nil || id != "boot-1-xyz" {
		t.Errorf("the bootstrap read back: %q, %v", id, err)
	}
	if _, err := ReadCapability(filepath.Join(dir, "missing"), anyone{}); !errors.Is(err, ErrGrantUnreadable) {
		t.Errorf("a missing grant file: %v", err)
	}
}

// anyone is the file adapter of Windows, which reports no owner and no mode: a grant file's
// privacy there is that of the user's profile directory.
type anyone struct{}

func (anyone) CurrentUser() uint32                      { return 0 }
func (anyone) Inspect(os.FileInfo) (owner, mode uint32) { return 0, 0o600 }
