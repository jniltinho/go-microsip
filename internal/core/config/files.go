package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
)

const (
	// maxConfigBytes bounds TOML and .env files.
	maxConfigBytes = 64 << 10
	// maxSecretBytes bounds password files.
	maxSecretBytes = 4 << 10
	// MaxCABytes is the largest accepted PEM CA bundle.
	MaxCABytes = 4 << 20
)

func ioErr(path string, err error) error { return &Error{Kind: IO, Path: path, Err: err} }

func insecure(path string, problem FileProblem) error {
	return &Error{Kind: InsecureFile, Path: path, Problem: problem}
}

// loaded is a file's text and the description of the handle it was read from.
type loaded struct {
	text string
	info os.FileInfo
}

// readBounded reads a regular file of at most limit bytes that group and others cannot write and
// that the current user or root owns. It returns nil when the file does not exist.
func readBounded(path string, limit int64, rejectSymlink bool, files ports.FileAccess) (*loaded, error) {
	// The type is checked before opening, so a FIFO or a device never blocks the read.
	stat := os.Stat
	if rejectSymlink {
		stat = os.Lstat
	}
	before, err := stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, ioErr(path, err)
	}
	if before.Mode()&fs.ModeSymlink != 0 {
		return nil, insecure(path, Symlink)
	}
	if !before.Mode().IsRegular() {
		return nil, insecure(path, NotRegular)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ioErr(path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, ioErr(path, err)
	}
	if !os.SameFile(before, info) { // replaced between the check and the open
		if rejectSymlink {
			return nil, insecure(path, Symlink)
		}
		return nil, insecure(path, NotRegular)
	}
	owner, mode := files.Inspect(info)
	if mode&0o022 != 0 {
		return nil, insecure(path, Writable)
	}
	if owner != files.CurrentUser() && owner != 0 {
		return nil, insecure(path, ForeignOwner)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, ioErr(path, err)
	}
	if int64(len(data)) > limit {
		return nil, &Error{Kind: FileTooLarge, Path: path}
	}
	if !utf8.Valid(data) {
		return nil, &Error{Kind: NotUTF8, Path: path}
	}
	return &loaded{text: string(data), info: info}, nil
}

// readRequired is readBounded for a file the user named: it must exist.
func readRequired(path string, limit int64, rejectSymlink bool, files ports.FileAccess) (*loaded, error) {
	l, err := readBounded(path, limit, rejectSymlink, files)
	if err == nil && l == nil {
		err = &Error{Kind: FileMissing, Path: path}
	}
	return l, err
}

// ReadCAFile reads the PEM CA bundle that decides which TLS servers are trusted.
//
// It must be a regular file of at most MaxCABytes, owned by the current user or root and not
// writable by group or others: whoever can change it can intercept TLS and the media keys carried
// inside. A caller must not fall back to other trust roots when a configured bundle fails.
func ReadCAFile(path string, files ports.FileAccess) ([]byte, error) {
	l, err := readRequired(path, MaxCABytes, false, files)
	if err != nil {
		return nil, err
	}
	return []byte(l.text), nil
}

// requirePrivate checks a secret-bearing file: owned by the current user, no group or other bits.
func requirePrivate(path string, info os.FileInfo, files ports.FileAccess) error {
	owner, mode := files.Inspect(info)
	if owner != files.CurrentUser() {
		return insecure(path, NotOwned)
	}
	if mode&0o077 != 0 {
		return insecure(path, NotPrivate)
	}
	return nil
}

// readPasswordFile reads a private password file that is not a symlink; one trailing newline is
// removed.
func readPasswordFile(path string, files ports.FileAccess) (domain.Secret, error) {
	l, err := readRequired(path, maxSecretBytes, true, files)
	if err != nil {
		return domain.Secret{}, err
	}
	if err := requirePrivate(path, l.info, files); err != nil {
		return domain.Secret{}, err
	}
	secret := l.text
	if rest, ok := strings.CutSuffix(secret, "\n"); ok {
		secret = strings.TrimSuffix(rest, "\r")
	}
	if secret == "" {
		return domain.Secret{}, &Error{Kind: EmptyPassword, Path: path}
	}
	return domain.NewSecret(secret), nil
}

// Save writes the configuration as TOML to path, atomically.
//
// Missing parent directories are created with mode 0700; existing ones are left as they are. The
// text goes to a 0600 temporary file in the same directory, is synced, renamed over path, and the
// directory is synced. The previous file is replaced entirely: its comments, its ordering and any
// literal password are not kept. No secret is ever written. On failure the previous file stays.
func Save(config Stored, path string) error {
	text, err := config.TOML()
	if err != nil {
		return err
	}
	dir, name := filepath.Split(path)
	if name == "" {
		return ioErr(path, fs.ErrInvalid)
	}
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ioErr(path, err)
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d-%d.tmp", name, os.Getpid(), time.Now().UnixNano()))
	write := func() error {
		file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, err = file.WriteString(text)
		if err == nil {
			err = file.Sync()
		}
		if cerr := file.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
		if runtime.GOOS == "windows" {
			return nil // a directory cannot be synced there
		}
		d, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	}
	if err := write(); err != nil {
		_ = os.Remove(tmp)
		return ioErr(path, err)
	}
	return nil
}
