//go:build !linux

package control

// The control transport of the other systems is not built yet: Windows gets its per-user named
// pipe with the Windows adapters (task 6.3 of migrate-to-go-wails).

// Endpoint is the owner's end of the transport.
type Endpoint struct{}

// RuntimeDir is where the control endpoint of this session lives.
func RuntimeDir(map[string]string) (string, error) { return "", ErrUnsupported }

// Listen makes this process the owner of agent control in the session.
func Listen(string, Welcome, Handler) (*Endpoint, error) { return nil, ErrUnsupported }

// Close stops serving.
func (*Endpoint) Close() error { return nil }

// Dial connects to the owner of the session's runtime directory.
func Dial(string) (*Client, error) { return nil, ErrUnsupported }
