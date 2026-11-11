package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// own is a ports.FileAccess that owns every file.
type own struct{}

func (own) CurrentUser() uint32 { return 1000 }
func (own) Inspect(info os.FileInfo) (owner, mode uint32) {
	return 1000, uint32(info.Mode().Perm())
}

// registrar is a minimal loopback registrar: it accepts every REGISTER and refuses every call.
func registrar(t *testing.T) (port int) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	port = probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	ua, err := sipgo.NewUA()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	srv.OnRegister(func(req *gosip.Request, tx gosip.ServerTransaction) {
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	srv.OnInvite(func(req *gosip.Request, tx gosip.ServerTransaction) {
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 486, "Busy Here", nil))
	})
	srv.OnAck(func(*gosip.Request, gosip.ServerTransaction) {})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ua.Close() })
	ready := make(chan struct{})
	go func() {
		_ = srv.ListenAndServe(context.WithValue(ctx, sipgo.ListenReadyCtxKey, sipgo.ListenReadyCtxValue(ready)), "tcp", addr)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the registrar did not start")
	}
	return port
}

func options(t *testing.T, port int) (Options, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1001\"\ndomain = \"127.0.0.1\"\nport = %d\ntransport = \"tcp\"\nexpires = 60\n", port)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	return Options{ConfigPath: file, Files: own{}, Env: map[string]string{"XDG_DATA_HOME": data, "GO_MICROSIP_SIP_PASSWORD": "lab"}},
		filepath.Join(data, "go-microsip", "go-microsip.sqlite3")
}

// Without a window the softphone registers, stays until it is told to stop, then unregisters and
// reports Stopped last.
func TestRunRegistersAndStopsWhenAsked(t *testing.T) {
	o, db := options(t, registrar(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var events []domain.Event
	err := Run(ctx, o, RunOptions{}, func(ev domain.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		if r, ok := ev.(domain.RegistrationChanged); ok && r.State.Status == domain.Registered {
			cancel() // what an interrupt does
		}
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("events %v", events)
	}
	if _, stopped := events[len(events)-1].(domain.Stopped); !stopped {
		t.Errorf("last event %T, want Stopped", events[len(events)-1])
	}
	if r, ok := events[len(events)-2].(domain.RegistrationChanged); !ok || r.State.Status != domain.Disabled {
		t.Errorf("the account was not unregistered before the end: %v", events)
	}
	if info, err := os.Stat(db); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("history database: %v %v", info, err)
	}
}

// A call the softphone was asked to place and that is refused ends the run with an error, and is
// in the history with the outcome the user would see.
func TestRunJournalsTheCallItPlaced(t *testing.T) {
	o, db := options(t, registrar(t))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := Run(ctx, o, RunOptions{Call: "1002"}, func(domain.Event) {})
	if !errors.Is(err, ErrCall) {
		t.Fatalf("run: %v, want the refused call", err)
	}
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	calls, err := s.RecentCalls(10, store.AllCalls, "")
	if err != nil || len(calls) != 1 {
		t.Fatalf("history %v, %v", calls, err)
	}
	c := calls[0].Call
	if c.Remote != "1002" || c.Direction != domain.Outgoing || c.Outcome != store.BusyPeer || c.EndedUTC == nil || c.AnsweredUTC != nil {
		t.Errorf("journaled call %+v", c)
	}
}

// One softphone per user: while one runs, a second one refuses to start.
func TestRunIsASingleInstance(t *testing.T) {
	o, _ := options(t, registrar(t))
	o.Locks = &memory.Locks{}
	o.Env["XDG_RUNTIME_DIR"] = t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var second error
	err := Run(ctx, o, RunOptions{}, func(ev domain.Event) {
		if r, ok := ev.(domain.RegistrationChanged); ok && r.State.Status == domain.Registered {
			second = Run(context.Background(), o, RunOptions{}, func(domain.Event) {})
			cancel()
		}
	})
	if err != nil || !errors.Is(second, ErrRunning) {
		t.Errorf("first run: %v; second run: %v, want the refusal", err, second)
	}
	// The lock goes with the run.
	again, stop := context.WithCancel(context.Background())
	stop()
	if err := Run(again, o, RunOptions{}, func(domain.Event) {}); err != nil {
		t.Errorf("a run after the first one ended: %v", err)
	}
}

func TestRunNeedsAPasswordAndARegistrar(t *testing.T) {
	o, db := options(t, registrar(t))
	delete(o.Env, "GO_MICROSIP_SIP_PASSWORD")
	if err := Run(context.Background(), o, RunOptions{}, func(domain.Event) {}); !errors.Is(err, ErrNoPassword) {
		t.Errorf("without a password: %v", err)
	}
	if _, err := os.Stat(db); err == nil {
		t.Error("a refused run created the database")
	}
	// Nothing listens here: with a call to place, the first failed registration ends the run.
	o, _ = options(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Run(ctx, o, RunOptions{Call: "1002"}, func(domain.Event) {}); !errors.Is(err, ErrRegistration) {
		t.Errorf("unreachable registrar: %v", err)
	}
}

func TestStorePath(t *testing.T) {
	for want, env := range map[string]map[string]string{
		"/d/go-microsip/go-microsip.sqlite3":              {"XDG_DATA_HOME": "/d", "HOME": "/h"},
		"/h/.local/share/go-microsip/go-microsip.sqlite3": {"XDG_DATA_HOME": "relative", "HOME": "/h"},
		"": {"HOME": "relative"},
	} {
		if got := StorePath(env); got != want {
			t.Errorf("%v: %q, want %q", env, got, want)
		}
	}
}
