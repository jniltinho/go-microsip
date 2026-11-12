package control

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// The endpoint on Linux: a socket in a directory of the session's runtime directory that only
// this user can enter, named after go-microsip, so that it never meets the Rust candidate's.
// Both ends ask the kernel who the other is (SO_PEERCRED).

const (
	dirName  = "go-microsip"
	lockFile = "control.lock"
)

// RuntimeDir is where the control endpoint of this session lives.
func RuntimeDir(env map[string]string) (string, error) {
	if dir := env["XDG_RUNTIME_DIR"]; filepath.IsAbs(dir) {
		return filepath.Join(dir, dirName), nil
	}
	return "", ErrNoRuntimeDir
}

// owned reports whether the file is this user's.
func owned(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

// private checks that dir is a directory of this user that nobody else can enter.
func private(dir string) error {
	info, err := os.Lstat(dir)
	switch {
	case err != nil:
		return err
	case !info.IsDir() || !owned(info) || info.Mode().Perm()&0o077 != 0:
		return ErrInsecure
	}
	return nil
}

// peer asks the kernel who is at the other end of a Unix socket.
func peer(conn net.Conn) (pid int, ours bool) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, false
	}
	var cred *unix.Ucred
	if raw.Control(func(fd uintptr) { cred, err = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }) != nil || err != nil {
		return 0, false
	}
	return int(cred.Pid), cred.Uid == uint32(os.Geteuid())
}

// Endpoint is the owner's end: the socket it listens on and the lock that makes it the one owner
// of agent control in the session.
type Endpoint struct {
	listener net.Listener
	lock     *os.File
}

// Listen makes this process the owner of agent control in the session and serves the handler's
// answers until Close. The lock of the runtime directory decides who that is: the first
// go-microsip to take it. ErrOwned when another one has it; that one keeps serving, and this
// window runs without agent control.
func Listen(dir string, welcome Welcome, handler Handler) (*Endpoint, error) {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	if err := private(dir); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrOwned
		}
		return nil, err
	}
	// With the lock held, a socket left here is of an owner that is gone.
	socket := filepath.Join(dir, SocketFile)
	switch info, err := os.Lstat(socket); {
	case err == nil && info.Mode().Type() == fs.ModeSocket && owned(info):
		err = os.Remove(socket)
		fallthrough
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		if err != nil {
			_ = lock.Close()
			return nil, err
		}
	case err == nil:
		_ = lock.Close()
		return nil, ErrInsecure
	}
	listener, err := net.Listen("unix", socket)
	if err == nil {
		err = os.Chmod(socket, 0o600)
	}
	if err != nil {
		if listener != nil {
			_ = listener.Close()
		}
		_ = lock.Close()
		return nil, err
	}
	go (&server{welcome: welcome, handler: handler, peer: peer}).serve(listener)
	return &Endpoint{listener: listener, lock: lock}, nil
}

// Close stops accepting, removes the socket and gives the lock up. Connections already open end
// when their clients do.
func (e *Endpoint) Close() error {
	err := e.listener.Close()
	_ = e.lock.Close()
	return err
}

// Dial connects to the owner of the session's runtime directory. It never starts one:
// ErrUnavailable when nobody listens.
func Dial(dir string) (*Client, error) {
	missing := func(err error) bool {
		return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
	}
	switch err := private(dir); {
	case missing(err):
		return nil, ErrUnavailable
	case err != nil:
		return nil, err
	}
	socket := filepath.Join(dir, SocketFile)
	switch info, err := os.Lstat(socket); {
	case missing(err):
		return nil, ErrUnavailable
	case err != nil:
		return nil, err
	case info.Mode().Type() != fs.ModeSocket || !owned(info):
		return nil, ErrInsecure
	}
	conn, err := net.Dial("unix", socket)
	switch {
	case missing(err):
		return nil, ErrUnavailable
	case err != nil:
		return nil, err
	}
	if _, ours := peer(conn); !ours {
		_ = conn.Close()
		return nil, ErrInsecure
	}
	client, err := handshake(conn)
	if err != nil {
		_ = conn.Close()
	}
	return client, err
}

// GrantsDir is where the files of this session's grants are written: in the private runtime
// directory, gone with the session.
func GrantsDir(runtimeDir string) string { return filepath.Join(runtimeDir, "grants") }

// WriteGrantFile writes an issued grant to a new file in dir, which is created for this user
// alone; the file is this user's alone too (0600) and is never written over. It returns the
// file's path.
func WriteGrantFile(dir string, grant IssuedGrant) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := private(dir); err != nil {
		return "", err
	}
	name, content := grant.File()
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", err
	}
	err = file.Chmod(0o600) // whatever the umask made of it
	if err == nil {
		_, err = file.WriteString(content)
	}
	if err == nil {
		err = file.Sync()
	}
	if closing := file.Close(); err == nil {
		err = closing
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
