//go:build linux || darwin

package control

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// echo answers every request with the pid it came from, and counts them.
type echo struct {
	mu    sync.Mutex
	count int
}

func (e *echo) Handle(r Request, pid int) Response {
	e.mu.Lock()
	e.count++
	e.mu.Unlock()
	return OK(r.RequestID, map[string]int{"pid": pid})
}

func (e *echo) handled() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

func statusRequest(id string) Request {
	return Request{SchemaVersion: 1, RequestID: id, Operation: Operation{Op: StatusGet}}
}

// listening is an owner in a runtime directory of the test's own.
func listening(t *testing.T) (dir string, handler *echo, endpoint *Endpoint) {
	t.Helper()
	dir = filepath.Join(tempDir(t), dirName)
	handler = &echo{}
	endpoint, err := Listen(dir, Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: ModeWindow, Epoch: 7}, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = endpoint.Close() })
	return dir, handler, endpoint
}

func TestAClientReachesTheOwnerOfItsSession(t *testing.T) {
	dir, _, _ := listening(t)
	if info, err := os.Lstat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the runtime directory: %v, %v", info.Mode(), err)
	}
	if info, err := os.Lstat(filepath.Join(dir, SocketFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the socket: %v, %v", info.Mode(), err)
	}
	client, err := Dial(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if w := client.Welcome(); w.Protocol != Protocol || w.Version != SchemaVersion || w.Mode != ModeWindow || w.Epoch != 7 {
		t.Errorf("the welcome: %+v", w)
	}
	// The owner is told which process asks: the kernel's word, not the client's.
	r := client.Do(statusRequest("r1"))
	if want := fmt.Sprintf(`{"pid":%d}`, os.Getpid()); r.Status != StatusOK || r.RequestID != "r1" || string(r.Data) != want {
		t.Errorf("the answer: %+v, want data %s", r, want)
	}
	// A request that does not validate is answered by the transport and never reaches the owner.
	bad := statusRequest("r2")
	bad.SchemaVersion = 2
	if r := client.Do(bad); code(r) != Validation || r.RequestID != "r2" {
		t.Errorf("another schema version: %+v", r)
	}
	// Two clients at once are answered independently.
	other, err := Dial(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if a, b := client.Do(statusRequest("a")), other.Do(statusRequest("b")); a.RequestID != "a" || b.RequestID != "b" {
		t.Errorf("two clients: %+v, %+v", a, b)
	}
}

func TestOneOwnerServesASessionAndTheNextTakesOver(t *testing.T) {
	dir, _, first := listening(t)
	welcome := Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: ModeWindow, Epoch: 8}
	if _, err := Listen(dir, welcome, &echo{}); !errors.Is(err, ErrOwned) {
		t.Fatalf("a second owner in the same session: %v", err)
	}
	// The first one still answers.
	client, err := Dial(dir)
	if err != nil || client.Welcome().Epoch != 7 {
		t.Fatalf("the first owner after the second tried: %v", err)
	}
	// A client still connected when the owner leaves is cut off, not left talking to it.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if r := client.Do(statusRequest("late")); r.Error == nil || r.Error.Code != EngineUnavailable {
		t.Errorf("a request on a connection the owner left: %+v", r)
	}
	client.Close()
	if _, err := Dial(dir); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a client after the owner left: %v", err)
	}
	second, err := Listen(dir, welcome, &echo{})
	if err != nil {
		t.Fatalf("the next owner: %v", err)
	}
	defer second.Close()
	if client, err := Dial(dir); err != nil || client.Welcome().Epoch != 8 {
		t.Errorf("a client of the next owner: %v", err)
	}
}

func TestNoOwnerIsUnavailableAndNothingIsStarted(t *testing.T) {
	root := tempDir(t)
	dir := filepath.Join(root, dirName)
	if _, err := Dial(dir); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no runtime directory: %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("the client made the runtime directory")
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(dir); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a runtime directory without a socket: %v", err)
	}
	// A socket nobody listens on, left by an owner that died: unavailable, and the next owner
	// replaces it.
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, SocketFile), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	if _, err := Dial(dir); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a socket nobody listens on: %v", err)
	}
	endpoint, err := Listen(dir, Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: ModeWindow, Epoch: 1}, &echo{})
	if err != nil {
		t.Fatalf("an owner over a socket left behind: %v", err)
	}
	defer endpoint.Close()
	if client, err := Dial(dir); err != nil {
		t.Errorf("a client after the socket was replaced: %v", err)
	} else {
		client.Close()
	}
}

func TestAnEndpointThatIsNotPrivateIsRefused(t *testing.T) {
	welcome := Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: ModeWindow, Epoch: 1}
	open := filepath.Join(tempDir(t), dirName)
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(open, welcome, &echo{}); !errors.Is(err, ErrInsecure) {
		t.Errorf("an owner in a directory others can enter: %v", err)
	}
	if _, err := Dial(open); !errors.Is(err, ErrInsecure) {
		t.Errorf("a client in a directory others can enter: %v", err)
	}
	// Something that is not a socket where the socket goes.
	dir := filepath.Join(tempDir(t), dirName)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SocketFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(dir, welcome, &echo{}); !errors.Is(err, ErrInsecure) {
		t.Errorf("an owner over a file that is not a socket: %v", err)
	}
	if _, err := Dial(dir); !errors.Is(err, ErrInsecure) {
		t.Errorf("a client of a file that is not a socket: %v", err)
	}
	// A symbolic link where the directory goes.
	link := filepath.Join(tempDir(t), dirName)
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(link); !errors.Is(err, ErrInsecure) {
		t.Errorf("a client through a symbolic link: %v", err)
	}
}

// raw is a connection to the socket that speaks for itself.
func raw(t *testing.T, dir string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("unix", filepath.Join(dir, SocketFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	return conn, bufio.NewReader(conn)
}

func TestTheOtherApplicationIsNotAnswered(t *testing.T) {
	dir, handler, _ := listening(t)
	// A client of the Rust candidate's protocol gets no welcome: the connection ends.
	conn, reader := raw(t, dir)
	if err := WriteMessage(conn, Hello{Protocol: "rust-microsip-control", Version: 1}); err != nil {
		t.Fatal(err)
	}
	var welcome Welcome
	if err := ReadMessage(reader, &welcome); err == nil {
		t.Errorf("the other protocol's hello was welcomed: %+v", welcome)
	}
	// And a request sent without a hello is not answered either.
	conn, reader = raw(t, dir)
	if err := WriteMessage(conn, statusRequest("r1")); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := ReadMessage(reader, &response); err == nil || handler.handled() != 0 {
		t.Errorf("a request without a hello: %+v, %d handled", response, handler.handled())
	}
	// A client of go-microsip that finds the other application's owner at its socket gives up.
	foreign := filepath.Join(tempDir(t), dirName)
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(foreign, SocketFile))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var hello Hello
			_ = ReadMessage(bufio.NewReader(conn), &hello)
			_ = WriteMessage(conn, Welcome{Protocol: "rust-microsip-control", Version: 1, Mode: ModeWindow, Epoch: 3})
			conn.Close()
		}
	}()
	if _, err := Dial(foreign); !errors.Is(err, ErrProtocol) {
		t.Errorf("an owner of the other protocol: %v", err)
	}
}

func TestTheTransportBoundsWhatAClientCanSend(t *testing.T) {
	dir, handler, _ := listening(t)
	conn, reader := raw(t, dir)
	read := func() Response {
		t.Helper()
		var r Response
		if err := ReadMessage(reader, &r); err != nil {
			t.Fatalf("no answer: %v", err)
		}
		return r
	}
	if err := WriteMessage(conn, CurrentHello()); err != nil {
		t.Fatal(err)
	}
	var welcome Welcome
	if err := ReadMessage(reader, &welcome); err != nil {
		t.Fatal(err)
	}
	// A line that is not a request is answered, and the connection stays.
	for _, line := range []string{"not json\n", `{"schema_version":1,"request_id":"r","idempotency_key":null,"operation":{"op":"status_get"},"extra":1}` + "\n"} {
		if _, err := conn.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
		if r := read(); code(r) != Validation || r.RequestID != "" || r.Error.Message != "malformed request" {
			t.Errorf("a malformed line: %+v", r)
		}
	}
	if handler.handled() != 0 {
		t.Error("a malformed line reached the owner")
	}
	// Ten requests a second from one connection, the two malformed ones included: the rest is
	// refused without reaching the owner.
	limited := 0
	for i := range 12 {
		if err := WriteMessage(conn, statusRequest(fmt.Sprintf("r%d", i))); err != nil {
			t.Fatal(err)
		}
		if r := read(); code(r) == RateLimited {
			limited++
		}
	}
	if limited != 4 || handler.handled() != 8 {
		t.Errorf("%d of 12 requests limited, %d handled; want 4 and 8", limited, handler.handled())
	}
	// A line over the size limit ends the connection.
	conn, reader = raw(t, dir)
	_ = WriteMessage(conn, CurrentHello())
	_ = ReadMessage(reader, &welcome)
	go func() { _, _ = conn.Write([]byte(strings.Repeat("x", MaxMessage+10) + "\n")) }()
	var response Response
	if err := ReadMessage(reader, &response); err == nil {
		t.Errorf("a line over the limit was answered: %+v", response)
	}
}

// An owner with no room answers and closes without reading: the hello a client then writes finds
// the connection closed, and the refusal is still what the client must report.
func TestARefusalThatArrivesBeforeTheHelloIsWrittenIsStillBusy(t *testing.T) {
	socket := filepath.Join(tempDir(t), SocketFile)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	refused := make(chan struct{})
	go func() {
		defer close(refused)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = WriteMessage(conn, Failure("", fail(Busy, "too many control connections")))
		_ = conn.Close()
	}()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	<-refused // the owner has answered and closed; nothing was written to it yet
	if _, err := handshake(conn); !errors.Is(err, ErrBusy) {
		t.Errorf("a refusal that came before the hello: %v, want ErrBusy", err)
	}
}

func TestConnectionsAreBoundedAndAStrangerIsNotRead(t *testing.T) {
	dir, _, _ := listening(t)
	var clients []*Client
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	for i := range MaxConnections {
		c, err := Dial(dir)
		if err != nil {
			t.Fatalf("client %d: %v", i+1, err)
		}
		clients = append(clients, c)
	}
	if _, err := Dial(dir); !errors.Is(err, ErrBusy) {
		t.Errorf("one client over the limit: %v", err)
	}
	// A client that leaves makes room; the owner and the others are not touched.
	clients[0].Close()
	clients = clients[1:]
	var again *Client
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var err error
		if again, err = Dial(dir); err == nil || time.Now().After(deadline) {
			break
		}
	}
	if again == nil {
		t.Fatal("no room after a client left")
	}
	clients = append(clients, again)
	if r := clients[0].Do(statusRequest("still")); r.Status != StatusOK {
		t.Errorf("another client after one left: %+v", r)
	}

	// The process at the other end, as the kernel names it.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if _, ours := peer(a); ours {
		t.Error("a connection that is not a Unix socket passed for this user's")
	}
	// A connection the kernel does not give this user is closed before a byte of it is read.
	strangers := filepath.Join(tempDir(t), SocketFile)
	listener, err := net.Listen("unix", strangers)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	handler := &echo{}
	go (&server{welcome: Welcome{Protocol: Protocol, Version: SchemaVersion}, handler: handler,
		peer: func(net.Conn) (int, bool) { return 0, false }}).serve(listener)
	conn, err := net.Dial("unix", strangers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_ = WriteMessage(conn, CurrentHello())
	var welcome Welcome
	if err := ReadMessage(bufio.NewReader(conn), &welcome); err == nil || handler.handled() != 0 {
		t.Errorf("a stranger was welcomed: %+v", welcome)
	}
}

// stuck is an owner that takes a request and does not answer until it is released.
type stuck struct {
	asked   atomic.Int32
	release chan struct{}
}

func (s *stuck) Handle(r Request, _ int) Response {
	s.asked.Add(1)
	<-s.release
	return OK(r.RequestID, nil)
}

// An owner that does not answer in time is a timeout for the client, which the command line
// turns into status 5, and the client does not send the request again on its own: the owner was
// asked once, and whether it took effect is for operation_get to tell.
func TestAnOwnerThatDoesNotAnswerIsATimeoutAskedOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the reply deadline")
	}
	dir := filepath.Join(tempDir(t), dirName)
	owner := &stuck{release: make(chan struct{})}
	endpoint, err := Listen(dir, Welcome{Protocol: Protocol, Version: SchemaVersion, Mode: ModeWindow, Epoch: 7}, owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(owner.release); _ = endpoint.Close() })
	client, err := Dial(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	key := "k-timeout"
	began := time.Now()
	r := client.Do(Request{SchemaVersion: 1, RequestID: "r-timeout", IdempotencyKey: &key, Operation: Operation{Op: DNDSet, Enabled: true}})
	if r.Error == nil || r.Error.Code != Timeout || r.Error.Code.ExitCode() != 5 {
		t.Fatalf("an owner that does not answer: %+v (%+v)", r, r.Error)
	}
	if waited := time.Since(began); waited < ReplyDeadline || waited > ReplyDeadline+3*time.Second {
		t.Errorf("the client gave up after %v, want about the reply deadline (%v)", waited, ReplyDeadline)
	}
	time.Sleep(time.Second)
	if asked := owner.asked.Load(); asked != 1 {
		t.Errorf("the owner was asked %d times, want once", asked)
	}
}
