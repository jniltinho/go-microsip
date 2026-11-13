package control

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

// What is macOS's own of the endpoint. There is no runtime directory of the session: the socket,
// the lock and the grant files live in the application's folder, as on Windows. The kernel is
// asked for the peer with LOCAL_PEERCRED and LOCAL_PEERPID. Two checks exist only here: the
// length of the socket's path, which macOS limits, and access-control entries, which let someone
// into a directory whose mode bits say nobody.

// ErrPathTooLong: the socket would have a path longer than macOS accepts.
var ErrPathTooLong = errors.New("the control socket's path is longer than the 103 bytes macOS allows: the home folder's path is too long")

// socketPathLimit is what a Unix socket's path may hold here: sockaddr_un has 104 bytes and the
// last is the terminator.
const socketPathLimit = 103

// RuntimeDir is where the control endpoint of this user lives; getenv reads the environment.
func RuntimeDir(getenv func(string) string) (string, error) {
	if home := getenv("HOME"); filepath.IsAbs(home) {
		return filepath.Join(home, "Library", "Application Support", dirName, "control"), nil
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
	var cred *unix.Xucred
	if raw.Control(func(fd uintptr) {
		if cred, err = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED); err == nil {
			// Who it is decides; which process is for the record, and 0 says it is not known.
			pid, _ = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
		}
	}) != nil || err != nil {
		return 0, false
	}
	return pid, cred.Uid == uint32(os.Geteuid())
}

// place refuses a directory whose socket macOS could not name, and, for the owner, makes the
// application's folder the control directory goes in: nothing but go-microsip makes it.
func place(dir string, owner bool) error {
	if len(filepath.Join(dir, SocketFile)) > socketPathLimit {
		return ErrPathTooLong
	}
	if !owner {
		return nil
	}
	return os.MkdirAll(filepath.Dir(dir), 0o700)
}

var acl struct {
	once  sync.Once
	get   func(path string, kind int32) uintptr
	entry func(list uintptr, which int32, entry *uintptr) int32
	free  func(object uintptr) int32
}

// exposed refuses a directory that has an access-control list with an entry in it: an entry can
// let another user in whatever the mode bits say. Whose entry it is, is not looked at: the
// control directory needs none. Without the system's library to ask, the answer is no.
func exposed(dir string) error {
	acl.once.Do(func() {
		system, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&acl.get, system, "acl_get_file")
		purego.RegisterLibFunc(&acl.entry, system, "acl_get_entry")
		purego.RegisterLibFunc(&acl.free, system, "acl_free")
	})
	if acl.get == nil {
		return ErrInsecure
	}
	const (
		extended = 0x100 // ACL_TYPE_EXTENDED
		first    = 0     // ACL_FIRST_ENTRY
	)
	list := acl.get(dir, extended)
	if list == 0 {
		return nil // no list at all
	}
	defer acl.free(list)
	var found uintptr
	if acl.entry(list, first, &found) == 0 {
		return ErrInsecure
	}
	return nil
}
