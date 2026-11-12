package control

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The endpoint on Windows: a socket of the Unix-domain family, which Windows has too, in a
// directory of the user's local application data. Both ends ask the system which process is at
// the other end and whose it is: a peer of another user is not served and not trusted. The
// directory is inside the user's profile, which other users cannot enter.
//
// Design D10 named a per-user named pipe here. The same-user rule it is there for is kept with
// the transport Linux uses, so that the server, the client and their limits are one piece of code.

const (
	dirName  = "go-microsip"
	lockFile = "control.lock"
	// sioAFUnixGetPeerPID asks a Unix-domain socket for the process at its other end
	// (SIO_AF_UNIX_GETPEERPID).
	sioAFUnixGetPeerPID = 0x58000100
)

// RuntimeDir is where the control endpoint of this user lives; getenv reads the environment.
func RuntimeDir(getenv func(string) string) (string, error) {
	if dir := getenv("LOCALAPPDATA"); filepath.IsAbs(dir) {
		return filepath.Join(dir, dirName, "control"), nil
	}
	return "", ErrNoRuntimeDir
}

// userOf returns the user a process runs as.
func userOf(pid uint32) (*windows.SID, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid.Copy()
}

// peer asks the system which process is at the other end of the socket, and whether it runs as
// this user.
func peer(conn net.Conn) (pid int, ours bool) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, false
	}
	var other, returned uint32
	if raw.Control(func(fd uintptr) {
		err = windows.WSAIoctl(windows.Handle(fd), sioAFUnixGetPeerPID, nil, 0, (*byte)(unsafe.Pointer(&other)), uint32(unsafe.Sizeof(other)), &returned, nil, 0)
	}) != nil || err != nil || other == 0 {
		return 0, false
	}
	theirs, err := userOf(other)
	if err != nil {
		return 0, false
	}
	mine, err := userOf(windows.GetCurrentProcessId())
	if err != nil {
		return 0, false
	}
	return int(other), windows.EqualSid(theirs, mine)
}

// Endpoint is the owner's end: the socket it listens on and the lock that makes it the one owner
// of agent control for this user.
type Endpoint struct {
	listener net.Listener
	lock     *os.File
	server   *server
}

// Listen makes this process the owner of agent control for this user and serves the handler's
// answers until Close. The lock of the directory decides who that is: the first go-microsip to
// take it. ErrOwned when another one has it.
func Listen(dir string, welcome Welcome, handler Handler) (*Endpoint, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped)); err != nil {
		_ = lock.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return nil, ErrOwned
		}
		return nil, err
	}
	// With the lock held, a socket left here is of an owner that is gone.
	socket := filepath.Join(dir, SocketFile)
	if err := os.Remove(socket); err != nil && !errors.Is(err, fs.ErrNotExist) {
		_ = lock.Close()
		return nil, err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	serving := (&server{welcome: welcome, handler: handler, peer: peer}).start(listener)
	return &Endpoint{listener: listener, lock: lock, server: serving}, nil
}

// Close stops accepting, ends the connections that are open, removes the socket and gives the
// lock up.
func (e *Endpoint) Close() error {
	err := e.listener.Close()
	e.server.stop()
	_ = e.lock.Close()
	return err
}

// Dial connects to the owner of this user's endpoint. It never starts one: ErrUnavailable when
// nobody listens.
func Dial(dir string) (*Client, error) {
	socket := filepath.Join(dir, SocketFile)
	if _, err := os.Lstat(socket); err != nil {
		return nil, ErrUnavailable
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		// A socket file nobody listens on is what an owner that died leaves behind.
		return nil, ErrUnavailable
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

// GrantsDir is where the files of this session's grants are written.
func GrantsDir(runtimeDir string) string { return filepath.Join(runtimeDir, "grants") }

// WriteGrantFile writes an issued grant to a new file in dir and returns its path; the file is
// never written over. shortcut: the file takes the permissions of the user's profile directory
// (the user, the system and administrators) and is given no access list of its own; write one
// if grant files are ever kept outside the profile.
func WriteGrantFile(dir string, grant IssuedGrant) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name, content := grant.File()
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, err = file.WriteString(content)
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
