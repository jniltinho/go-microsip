package control

import (
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// What is Linux's own of the endpoint: the session's runtime directory, and SO_PEERCRED.

// RuntimeDir is where the control endpoint of this session lives; getenv reads the environment.
func RuntimeDir(getenv func(string) string) (string, error) {
	if dir := getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(dir) {
		return filepath.Join(dir, dirName), nil
	}
	return "", ErrNoRuntimeDir
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

// place has nothing to prepare or to refuse on Linux: the session's runtime directory exists.
func place(string, bool) error { return nil }

// exposed: on Linux the mode bits say all there is to say of who can enter a directory here.
func exposed(string) error { return nil }
