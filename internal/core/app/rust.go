package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/rustimport"
)

// rustName is the directory and file name the Rust application uses.
const rustName = "rust-microsip"

// ErrNoRuntimeDirectory means XDG_RUNTIME_DIR does not name an absolute directory, so the instance
// locks that keep both applications out during an import cannot be taken.
var ErrNoRuntimeDirectory = errors.New("no runtime directory (XDG_RUNTIME_DIR) is available")

// ErrImportUnsupported means this build cannot take the locks the import needs.
var ErrImportUnsupported = errors.New("the import from rust-microsip is not available in this build")

// InstanceLock is go-microsip's own lock file: instance.lock beside the database, or empty when
// there is no data directory. It is tied to the database and to nothing else, so every way of
// starting go-microsip that would use the same database takes the same lock, whatever the session
// it runs in. The lock is an advisory one that the system releases when the process dies: a file
// that stays behind locks nothing.
func InstanceLock(env map[string]string) string {
	if db := StorePath(env); db != "" {
		return filepath.Join(filepath.Dir(db), "instance.lock")
	}
	return ""
}

// importer locates go-microsip's own files, which is all that undoing an import needs, and the
// Rust application's when this session can reach them. The Rust application's paths follow its own
// rules: the same bases as go-microsip's, with its name, and its lock in the runtime directory and
// nowhere else.
func (o Options) importer() (*rustimport.Importer, error) {
	c := o.base()
	if o.Locks == nil {
		return nil, ErrImportUnsupported
	}
	db := StorePath(c.Env)
	if db == "" {
		return nil, ErrNoDataDirectory
	}
	target := c.DefaultConfigPath()
	i := &rustimport.Importer{
		Paths: rustimport.Paths{Config: target, DB: db, Lock: InstanceLock(c.Env)},
		Locks: o.Locks, Files: o.Files, Secrets: o.Secrets, Legacy: o.LegacySecrets,
	}
	if dir := c.Env["XDG_RUNTIME_DIR"]; filepath.IsAbs(dir) && target != "" && runtime.GOOS != "windows" {
		i.Paths.SourceConfig = filepath.Join(filepath.Dir(filepath.Dir(target)), rustName, filepath.Base(target))
		i.Paths.SourceDB = filepath.Join(filepath.Dir(filepath.Dir(db)), rustName, rustName+".sqlite3")
		i.Paths.SourceLock = filepath.Join(dir, rustName, "instance.lock")
	}
	return i, nil
}

// exclusive makes this process the one go-microsip of its database: it takes the instance lock and
// undoes an import that was interrupted, before anything reads or writes the database. release
// frees the lock; the caller keeps it for as long as it uses the database.
//
// A build or a session that has no lock runs unprotected and says so, but never beside an
// unfinished import. A lock that exists and cannot be taken is an error.
func (o Options) exclusive() (release func(), err error) {
	release = func() {}
	i, err := o.importer()
	if err != nil { // no lock in this build, or no data directory: nothing to protect or to undo
		o.warn("single-instance protection is off: this build or this session has no place for the instance lock")
		return release, nil
	}
	unlock, err := o.Locks.Lock(i.Paths.Lock)
	switch {
	case errors.Is(err, ports.ErrLocked):
		return release, ErrRunning
	case errors.Is(err, errors.ErrUnsupported):
		if i.Pending() {
			return release, errors.New("an import from rust-microsip was interrupted and cannot be undone without the instance lock")
		}
		o.warn("single-instance protection is off: " + err.Error())
		return release, nil
	case err != nil:
		return release, fmt.Errorf("the instance lock could not be taken: %w", err)
	}
	if err := i.RecoverLocked(); errors.Is(err, rustimport.ErrCredentialCleanup) {
		o.warn(err.Error()) // the files are settled; the keyring is tried again at the next start
	} else if err != nil {
		unlock()
		return release, err
	}
	return unlock, nil
}

// ImportRust previews what an import from the Rust application (rust-microsip) would bring and,
// with apply, brings it: the configuration file, and the database with contacts, history and saved
// settings, replace go-microsip's, which are kept as backups. With credential the password is
// copied from the Rust application's keyring too. The Rust application's files are only read, and
// it must not be running. Nothing is ever imported without this call.
func ImportRust(o Options, apply, credential bool) (string, error) {
	i, err := o.importer()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return "", ErrImportUnsupported
	}
	if i.Paths.SourceLock == "" {
		// Without its runtime directory the Rust application cannot be kept out while its files
		// are copied.
		return "", ErrNoRuntimeDirectory
	}
	preview, err := i.Preview()
	if err != nil {
		return "", err
	}
	text := preview.String()
	if !apply {
		return text, nil
	}
	if err := i.Apply(preview, credential); err != nil {
		return "", &ImportError{Preview: text, Err: err}
	}
	return text + "Imported from rust-microsip. Its files were not modified.\n", nil
}
