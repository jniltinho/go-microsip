package control

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// Grant files: the capability of a grant leaves the window in a private file and comes back to a
// client by its path (--grant-file), never as an argument. The capability is never printed.

// maxGrantFile is the largest grant file read.
const maxGrantFile = 512

// Errors of reading a grant file. None of them repeats what the file holds.
var (
	ErrGrantUnreadable = errors.New("the grant file cannot be read")
	ErrGrantNotPrivate = errors.New("the grant file must be a private regular file (mode 0600) of this user")
	ErrNotAGrant       = errors.New("the grant file does not hold a grant")
	ErrNotABootstrap   = errors.New("the file is not a bootstrap grant")
)

// readPrivate reads a file that only this user can read or write: a regular file, not reached
// through a symbolic link, of this user, with no bit for the group or for others, of at most
// maxGrantFile bytes.
func readPrivate(path string, files ports.FileAccess) (string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return "", ErrGrantUnreadable
	}
	if !before.Mode().IsRegular() {
		return "", ErrGrantNotPrivate
	}
	file, err := os.Open(path)
	if err != nil {
		return "", ErrGrantUnreadable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", ErrGrantUnreadable
	}
	if owner, mode := files.Inspect(info); !os.SameFile(before, info) || owner != files.CurrentUser() || fs.FileMode(mode)&0o077 != 0 {
		return "", ErrGrantNotPrivate
	}
	data, err := io.ReadAll(io.LimitReader(file, maxGrantFile+1))
	if err != nil {
		return "", ErrGrantUnreadable
	}
	if len(data) > maxGrantFile {
		return "", ErrNotAGrant
	}
	return string(data), nil
}

// ReadBootstrap reads the bootstrap id and the capability of a file exported from the window
// for the headless service.
func ReadBootstrap(path string, files ports.FileAccess) (bootstrapID, capability string, err error) {
	text, err := readPrivate(path, files)
	if err != nil {
		return "", "", err
	}
	return bootstrap(text)
}

func bootstrap(text string) (bootstrapID, capability string, err error) {
	var fields map[string]string
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &fields) != nil || len(fields) != 2 ||
		!Token(fields["bootstrap_id"], 128) || !Token(fields["capability"], 128) {
		return "", "", ErrNotABootstrap
	}
	return fields["bootstrap_id"], fields["capability"], nil
}

// ReadCapability reads a grant's capability from its private file: one token, or a bootstrap
// file, whose capability is the service's grant.
func ReadCapability(path string, files ports.FileAccess) (string, error) {
	text, err := readPrivate(path, files)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		_, capability, err := bootstrap(text)
		return capability, err
	}
	if token := strings.TrimSpace(text); Token(token, 128) {
		return token, nil
	}
	return "", ErrNotAGrant
}
