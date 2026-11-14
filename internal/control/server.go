package control

import (
	"bufio"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// MaxConnections is how many clients are connected at once.
	MaxConnections = 16
	// PerConnectionRate is the requests per second taken from one connection.
	PerConnectionRate = 10
	// GlobalRate is the requests per second taken from all connections together.
	GlobalRate = 50
	// ReplyDeadline is how long a client waits for the answer to one request.
	ReplyDeadline = 5 * time.Second

	helloDeadline = 2 * time.Second
	// idle is how long a silent connection is kept.
	idle = 10 * time.Minute
)

// Handler answers the requests of a connection: the Owner.
type Handler interface {
	Handle(r Request, pid int) Response
}

// server is the owner's side of the transport, whatever the listener: the handshake, the limits
// and one request at a time for each connection.
type server struct {
	welcome Welcome
	handler Handler
	// peer says who is at the other end; a connection that is not this user's is closed before
	// a byte of it is read.
	peer func(net.Conn) (pid int, ours bool)

	connections atomic.Int32
	mu          sync.Mutex
	window      int64 // the second the global count is of
	used        int
	open        map[net.Conn]struct{} // the connections being served
	stopped     bool
}

// start serves the listener in the background.
func (s *server) start(listener net.Listener) *server {
	go s.serve(listener)
	return s
}

// stop ends every connection: a client still connected when the owner leaves is not left
// talking to it.
func (s *server) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	for conn := range s.open {
		_ = conn.Close()
	}
}

// track notes a connection as open; false when the server has stopped.
func (s *server) track(conn net.Conn, open bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !open {
		delete(s.open, conn)
		return true
	}
	if s.stopped {
		return false
	}
	if s.open == nil {
		s.open = map[net.Conn]struct{}{}
	}
	s.open[conn] = struct{}{}
	return true
}

// serve accepts connections until the listener is closed.
func (s *server) serve(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		pid, ours := s.peer(conn)
		if !ours {
			_ = conn.Close()
			continue
		}
		if s.connections.Add(1) > MaxConnections {
			s.connections.Add(-1)
			_ = conn.SetWriteDeadline(time.Now().Add(ReplyDeadline))
			_ = WriteMessage(conn, Failure("", fail(Busy, "too many control connections")))
			_ = conn.Close()
			continue
		}
		go func() {
			defer s.connections.Add(-1)
			defer conn.Close()
			if s.track(conn, true) {
				defer s.track(conn, false)
				s.connection(conn, pid)
			}
		}()
	}
}

// allowed counts one request in the global one-second window.
func (s *server) allowed(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if second := now.Unix(); second != s.window {
		s.window, s.used = second, 0
	}
	s.used++
	return s.used <= GlobalRate
}

func (s *server) connection(conn net.Conn, pid int) {
	reader := bufio.NewReader(conn)
	write := func(message any) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(ReplyDeadline))
		return WriteMessage(conn, message) == nil
	}
	_ = conn.SetReadDeadline(time.Now().Add(helloDeadline))
	var hello Hello
	if ReadMessage(reader, &hello) != nil || hello != CurrentHello() || !write(s.welcome) {
		return
	}
	window, used := time.Now(), 0
	for {
		_ = conn.SetReadDeadline(time.Now().Add(idle))
		var request Request
		err := ReadMessage(reader, &request)
		if now := time.Now(); now.Sub(window) >= time.Second {
			window, used = now, 0
		}
		used++
		var response Response
		switch {
		case errors.Is(err, errTooLong):
			return // the stream is not at a line's start any more
		case errors.Is(err, ErrInvalidMessage):
			// A line that is not a request, within the size limit: it is answered, counted like
			// any request, and the connection stays.
			if used > PerConnectionRate {
				time.Sleep(100 * time.Millisecond)
			}
			response = Failure("", invalid("malformed request"))
		case err != nil:
			return
		case used > PerConnectionRate || !s.allowed(time.Now()):
			response = Failure(request.RequestID, fail(RateLimited, "too many requests"))
		case request.Validate() != nil:
			response = Failure(request.RequestID, request.Validate())
		default:
			// shortcut: the owner answers on this goroutine, with no deadline of the server's
			// own; the client gives up after ReplyDeadline. Put a queue with a deadline here if
			// the owner ever waits on something slower than its database.
			response = s.handler.Handle(request, pid)
		}
		if !write(response) {
			return
		}
	}
}

// Client is a connection to the owner.
type Client struct {
	conn    net.Conn
	reader  *bufio.Reader
	welcome Welcome
}

// Errors of reaching an owner.
var (
	// ErrUnavailable: no go-microsip is running in this session. None is started.
	ErrUnavailable = errors.New("no go-microsip instance is running")
	// ErrInsecure: the runtime directory, the socket or the process at its other end is not this
	// user's alone.
	ErrInsecure = errors.New("the control endpoint is not private to the current user")
	// ErrProtocol: what answers speaks another protocol or version.
	ErrProtocol = errors.New("the running instance speaks an incompatible control protocol")
	// ErrBusy: the owner has too many control connections.
	ErrBusy = errors.New("the running instance is busy; try again")
	// ErrNoRuntimeDir: XDG_RUNTIME_DIR does not name an absolute directory, and there is no
	// private place to fall back to.
	ErrNoRuntimeDir = errors.New("XDG_RUNTIME_DIR is not set to an absolute path")
	// ErrOwned: another go-microsip already serves agent control in this session.
	ErrOwned = errors.New("another go-microsip serves agent control in this session")
	// ErrUnsupported: this build has no control transport for the operating system.
	ErrUnsupported = errors.New("agent control is not available on this system in this build")
)

// handshake sends the hello on a connection whose other end was already checked.
func handshake(conn net.Conn) (*Client, error) {
	_ = conn.SetDeadline(time.Now().Add(ReplyDeadline + time.Second))
	// An owner with no room answers and closes without reading the hello: the write may then
	// fail while the refusal waits to be read, so what was received decides before the write.
	unsent := WriteMessage(conn, CurrentHello())
	reader := bufio.NewReader(conn)
	var first struct {
		Welcome
		// The fields of a Response: a refusal comes in place of the welcome.
		SchemaVersion uint32     `json:"schema_version"`
		RequestID     string     `json:"request_id"`
		Status        Status     `json:"status"`
		OperationID   *string    `json:"operation_id"`
		Data          any        `json:"data"`
		Error         *ErrorBody `json:"error"`
	}
	if err := ReadMessage(reader, &first); err != nil {
		return nil, ErrProtocol
	}
	switch {
	case first.Status == StatusError:
		return nil, ErrBusy
	case unsent != nil || first.Protocol != Protocol || first.Version != SchemaVersion:
		return nil, ErrProtocol
	}
	return &Client{conn: conn, reader: reader, welcome: first.Welcome}, nil
}

// Welcome is the owner's answer to the hello.
func (c *Client) Welcome() Welcome { return c.welcome }

// Do sends one request and waits for its answer. When the owner does not answer in time the
// response is a timeout: the request may still take effect, so ask operation_get before trying
// again, and with the same key.
func (c *Client) Do(r Request) Response {
	_ = c.conn.SetDeadline(time.Now().Add(ReplyDeadline + time.Second))
	if err := WriteMessage(c.conn, r); err != nil {
		return Failure(r.RequestID, fail(EngineUnavailable, "the owner is stopping"))
	}
	var response Response
	err := ReadMessage(c.reader, &response)
	var timeout net.Error
	switch {
	case errors.As(err, &timeout) && timeout.Timeout():
		return Failure(r.RequestID, fail(Timeout, "the owner did not answer in time; query the operation before retrying"))
	case err != nil:
		return Failure(r.RequestID, fail(EngineUnavailable, "the owner is stopping"))
	}
	return response
}

// Close ends the connection. The owner and the calls it holds are not touched.
func (c *Client) Close() error { return c.conn.Close() }
