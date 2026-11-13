package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
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
func registrar(t *testing.T) (port int) { return lab(t, 486) }

// ringingRegistrar accepts every REGISTER and lets every call ring without an answer.
func ringingRegistrar(t *testing.T) (port int) { return lab(t, 180) }

// sent is how the softphone named itself in the last request of each method that each lab PBX
// received: "<port> <method>" to "<method> contact <user> product <User-Agent>".
var sent sync.Map

// heard counts the REGISTER requests each lab PBX received, by its port.
var heard sync.Map

// registrations is how many REGISTER requests the lab PBX on port received.
func registrations(port int) int64 {
	n, _ := heard.LoadOrStore(port, new(atomic.Int64))
	return n.(*atomic.Int64).Load()
}

func lab(t *testing.T, invite int) (port int) { return labWith(t, invite, true) }

// labWith is lab; without keys its answer does not agree to telephone-event.
func labWith(t *testing.T, invite int, keys bool) (port int) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	port = probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	sent.Delete(fmt.Sprintf("%d REFER answered", port)) // an earlier PBX of this process may have had the port
	ua, err := sipgo.NewUA()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	said := func(req *gosip.Request) {
		who := req.Method.String() + " "
		if c := req.Contact(); c != nil {
			who += "contact " + c.Address.User
		}
		if ua := req.GetHeader("User-Agent"); ua != nil {
			who += " product " + ua.Value()
		}
		sent.Store(fmt.Sprintf("%d %s", port, req.Method), who)
	}
	srv.OnRegister(func(req *gosip.Request, tx gosip.ServerTransaction) {
		said(req)
		n, _ := heard.LoadOrStore(port, new(atomic.Int64))
		n.(*atomic.Int64).Add(1)
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	srv.OnInvite(func(req *gosip.Request, tx gosip.ServerTransaction) {
		said(req)
		if invite == 200 { // answered, with audio that nobody listens to
			rtp, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				return
			}
			t.Cleanup(func() { rtp.Close() })
			sdp := fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=fmtp:101 0-16\r\na=sendrecv\r\n", rtp.LocalAddr().(*net.UDPAddr).Port)
			if !keys {
				sdp = fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\na=sendrecv\r\n", rtp.LocalAddr().(*net.UDPAddr).Port)
			}
			res := gosip.NewResponseFromRequest(req, 200, "OK", []byte(sdp))
			res.AppendHeader(gosip.NewHeader("Content-Type", "application/sdp"))
			res.AppendHeader(gosip.NewHeader("Contact", fmt.Sprintf("<sip:lab@%s;transport=tcp>", addr)))
			_ = tx.Respond(res)
			return
		}
		_ = tx.Respond(gosip.NewResponseFromRequest(req, invite, "Lab", nil))
		if invite < 200 { // ringing: the caller gives up, and the transaction ends with 487
			<-tx.Done()
		}
	})
	srv.OnBye(func(req *gosip.Request, tx gosip.ServerTransaction) {
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	srv.OnRefer(func(req *gosip.Request, tx gosip.ServerTransaction) { // accepted, and no NOTIFY ever follows
		if tx.Respond(gosip.NewResponseFromRequest(req, 202, "Accepted", nil)) == nil {
			sent.Store(fmt.Sprintf("%d REFER answered", port), "202")
		}
	})
	srv.OnCancel(func(req *gosip.Request, tx gosip.ServerTransaction) {
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
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

// Without a window too, a database that cannot be opened does not stop the softphone (design
// D15): it warns that the history is unavailable, registers, and leaves the file as it is.
func TestRunGoesOnWithoutADatabase(t *testing.T) {
	o, db := options(t, registrar(t))
	if err := os.MkdirAll(filepath.Dir(db), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db, []byte("this is not a database\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var warnings []string
	o.Warn = func(message string) { mu.Lock(); warnings = append(warnings, message); mu.Unlock() }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	registered := false
	err := Run(ctx, o, RunOptions{}, func(ev domain.Event) {
		if r, ok := ev.(domain.RegistrationChanged); ok && r.State.Status == domain.Registered {
			registered = true
			cancel()
		}
	})
	if err != nil || !registered {
		t.Fatalf("run over a database that cannot be opened: registered %v, %v", registered, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.ContainsFunc(warnings, func(w string) bool { return strings.Contains(w, "history storage unavailable") }) {
		t.Errorf("no warning says that the history is unavailable: %q", warnings)
	}
	if left, err := os.ReadFile(db); err != nil || string(left) != "this is not a database\n" {
		t.Errorf("the file that is not a database was touched: %q, %v", left, err)
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

// The lock is tied to the database and to nothing else: a desktop session and one without a
// runtime directory take the same lock, so there is one softphone per database. A lock that exists
// and cannot be taken stops the run instead of running unprotected.
func TestTheInstanceLockBelongsToTheDatabase(t *testing.T) {
	o, db := options(t, registrar(t))
	locks := &memory.Locks{}
	o.Locks = locks
	desktop := maps.Clone(o.Env)
	desktop["XDG_RUNTIME_DIR"] = t.TempDir()
	if lock := InstanceLock(o.Env); lock != filepath.Join(filepath.Dir(db), "instance.lock") || lock != InstanceLock(desktop) {
		t.Fatalf("lock files %q and %q", lock, InstanceLock(desktop))
	}
	held, err := locks.Lock(InstanceLock(o.Env))
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o, RunOptions{}, func(domain.Event) {}); !errors.Is(err, ErrRunning) {
		t.Errorf("a second softphone: %v", err)
	}
	held()
	o.Locks = brokenLocks{}
	if err := Run(context.Background(), o, RunOptions{}, func(domain.Event) {}); err == nil || !strings.Contains(err.Error(), "instance lock") {
		t.Errorf("a lock that cannot be taken: %v", err)
	}
}

// brokenLocks fails every lock for a reason other than "held".
type brokenLocks struct{}

func (brokenLocks) Lock(string) (func(), error) { return nil, errors.New("read-only file system") }

// Being told to stop while the call it placed is not answered yet is not a failure.
func TestStoppingBeforeTheAnswerIsNotAFailure(t *testing.T) {
	o, _ := options(t, ringingRegistrar(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := Run(ctx, o, RunOptions{Call: "1002"}, func(ev domain.Event) {
		if c, ok := ev.(domain.CallChanged); ok && !c.Call.Phase.IsFinal() {
			cancel() // an interrupt while it dials or rings
		}
	})
	if err != nil {
		t.Errorf("run: %v, want a clean stop", err)
	}
}

// A reader of the events that has stalled keeps neither the call nor the softphone: the engine
// stops when told to, and the run ends shortly after.
func TestAStalledReaderDoesNotKeepTheSoftphoneRunning(t *testing.T) {
	o, _ := options(t, registrar(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stalled := make(chan struct{})
	t.Cleanup(func() { close(stalled) })
	var warned atomic.Bool
	o.Warn = func(string) { warned.Store(true) }
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- Run(ctx, o, RunOptions{}, func(ev domain.Event) {
			if r, ok := ev.(domain.RegistrationChanged); ok && r.State.Status == domain.Registered {
				cancel()
				<-stalled // the output pipe is full and nobody reads it
			}
		})
	}()
	select {
	case err := <-done:
		if err != nil || !warned.Load() {
			t.Errorf("run: %v, warned %v", err, warned.Load())
		}
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("the run took %v to end", took)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the softphone kept running behind a stalled reader")
	}
}

// An import from the Rust application that was interrupted is undone at the next start, whatever
// session that start runs in: undoing it needs go-microsip's own files only.
func TestStartUndoesAnInterruptedImportInAnySession(t *testing.T) {
	o, db := options(t, 1)
	o.Locks = &memory.Locks{}
	delete(o.Env, "GO_MICROSIP_SIP_PASSWORD") // the run ends right after the recovery
	dir := filepath.Dir(db)
	staging, journal := filepath.Join(dir, "import-staging"), filepath.Join(dir, "import.journal")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte(`{"step":"staging","had_db":false,"had_config":false,"new_config":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, has := o.Env["XDG_RUNTIME_DIR"]; has {
		t.Fatal("this start must have no runtime directory")
	}
	if err := Run(context.Background(), o, RunOptions{}, func(domain.Event) {}); !errors.Is(err, ErrNoPassword) {
		t.Fatalf("run: %v", err)
	}
	for _, left := range []string{staging, journal} {
		if _, err := os.Stat(left); err == nil {
			t.Errorf("%s is still there: the interrupted import was not undone", filepath.Base(left))
		}
	}
	// A build without a lock never starts beside an unfinished import.
	if err := os.WriteFile(journal, []byte(`{"step":"staging"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, locks := range map[string]ports.Locker{"a platform without the lock": unsupportedLocks{}, "a build without a locker": nil} {
		o.Locks = locks
		if err := Run(context.Background(), o, RunOptions{}, func(domain.Event) {}); err == nil || !strings.Contains(err.Error(), "interrupted") {
			t.Errorf("%s, beside an unfinished import: %v", name, err)
		}
		if _, err := Import(o, journal, true); err == nil {
			t.Errorf("%s: a configuration import ran beside an unfinished import", name)
		}
	}
}

// unsupportedLocks is a platform that has no instance lock yet.
type unsupportedLocks struct{}

func (unsupportedLocks) Lock(string) (func(), error) {
	return nil, fmt.Errorf("not here: %w", errors.ErrUnsupported)
}

// Stored settings are replaced only by the one go-microsip of the database: not beside a running
// softphone and not beside an import, and the lock is held until the store is closed.
func TestConfigImportNeedsTheInstanceLock(t *testing.T) {
	o, db := options(t, 1)
	locks := &memory.Locks{}
	o.Locks = locks
	file := filepath.Join(t.TempDir(), "import.toml")
	if err := os.WriteFile(file, []byte("schema = 1\n[account]\nusername = \"1009\"\ndomain = \"pbx.invalid\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := locks.Lock(InstanceLock(o.Env))
	if err != nil {
		t.Fatal(err)
	}
	var refused *ImportError
	if _, err := Import(o, file, true); !errors.As(err, &refused) || !errors.Is(err, ErrRunning) {
		t.Errorf("import beside a running go-microsip: %v", err)
	}
	if _, err := os.Stat(db); err == nil {
		t.Error("a refused import created the database")
	}
	held()
	if _, err := Import(o, file, true); err != nil {
		t.Fatalf("import: %v", err)
	}
	if locks.Held(InstanceLock(o.Env)) {
		t.Error("the lock is still held after the import")
	}
}

// The history of a call comes from what its snapshots say, not from which of them were seen: a
// call whose "established" snapshot was merged away is still stored as answered, with its times.
func TestHistoryDoesNotDependOnSeeingTheCallEstablished(t *testing.T) {
	o, db := options(t, 1)
	j := openJournal(o, db)
	started := time.Now().Add(-10 * time.Second).UnixMilli()
	snap := domain.CallSnapshot{ID: 1, Direction: domain.Outgoing, Remote: "1002", Phase: domain.CallPhase{Kind: domain.Ringing}, StartedMS: started}
	j.record(domain.CallChanged{Call: snap})
	snap.Phase, snap.Secure = domain.EndedWith(domain.EndFailed), true
	snap.AnsweredMS, snap.EndedMS = started+2_000, started+9_500
	j.record(domain.CallChanged{Call: snap}) // straight from ringing to the end
	snap = domain.CallSnapshot{ID: 2, Direction: domain.Incoming, Remote: "1003", Phase: domain.EndedWith(domain.EndCanceled), StartedMS: started, EndedMS: started + 1_000}
	j.record(domain.CallChanged{Call: snap}) // a call seen only at its end, never answered
	j.close()

	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	calls, err := s.RecentCalls(10, store.AllCalls, "")
	if err != nil || len(calls) != 2 {
		t.Fatalf("history %v, %v", calls, err)
	}
	byRemote := map[string]store.CallRecord{}
	for _, c := range calls {
		byRemote[c.Call.Remote] = c.Call
	}
	answered, missed := byRemote["1002"], byRemote["1003"]
	if answered.Outcome != store.Answered || answered.DurationMS == nil || *answered.DurationMS != 7_500 ||
		answered.AnsweredUTC == nil || *answered.AnsweredUTC != (started+2_000)/1000 || answered.EndedUTC == nil || !answered.Secure {
		t.Errorf("the answered call: %+v", answered)
	}
	if missed.Outcome != store.Missed || missed.AnsweredUTC != nil || missed.DurationMS != nil || missed.StartedUTC != started/1000 {
		t.Errorf("the unanswered call: %+v", missed)
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
	cases := map[string]map[string]string{
		"/d/go-microsip/go-microsip.sqlite3":              {"XDG_DATA_HOME": "/d", "HOME": "/h"},
		"/h/.local/share/go-microsip/go-microsip.sqlite3": {"XDG_DATA_HOME": "relative", "HOME": "/h"},
		"": {"HOME": "relative"},
	}
	// macOS has one folder for an application's files and no XDG convention.
	if runtime.GOOS == "darwin" {
		cases = map[string]map[string]string{
			"/h/Library/Application Support/go-microsip/go-microsip.sqlite3": {"XDG_DATA_HOME": "/d", "HOME": "/h"},
			"": {"XDG_DATA_HOME": "/d", "HOME": "relative"},
		}
	}
	for want, env := range cases {
		if got := StorePath(env); got != want {
			t.Errorf("%v: %q, want %q", env, got, want)
		}
	}
}
