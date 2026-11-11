package app

import (
	"errors"
	"path/filepath"
	"runtime"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/rustimport"
)

// rustName is the directory and file name the Rust application uses.
const rustName = "rust-microsip"

// ErrNoRuntimeDirectory means XDG_RUNTIME_DIR does not name an absolute directory, so the instance
// locks that keep both applications out during an import cannot be taken.
var ErrNoRuntimeDirectory = errors.New("no runtime directory (XDG_RUNTIME_DIR) is available")

// ErrImportUnsupported means this build cannot take the locks the import needs.
var ErrImportUnsupported = errors.New("the import from rust-microsip is not available in this build")

// InstanceLock is go-microsip's own lock file: <XDG_RUNTIME_DIR>/go-microsip/instance.lock, or,
// in a session without a runtime directory, instance.lock beside the database. The lock is an
// advisory one that the system releases when the process dies, so a file that stays behind locks
// nothing. Empty when there is neither directory.
func InstanceLock(env map[string]string) string {
	if dir := env["XDG_RUNTIME_DIR"]; filepath.IsAbs(dir) {
		return filepath.Join(dir, config.DirName, "instance.lock")
	}
	if db := StorePath(env); db != "" {
		return filepath.Join(filepath.Dir(db), "instance.lock")
	}
	return ""
}

// rustImporter locates both applications' data. The Rust application's paths follow its own rules:
// the same bases as go-microsip's, with its name.
func (o Options) rustImporter() (*rustimport.Importer, error) {
	c := o.base()
	if o.Locks == nil || runtime.GOOS == "windows" {
		return nil, ErrImportUnsupported
	}
	db, own := StorePath(c.Env), InstanceLock(c.Env)
	if db == "" {
		return nil, ErrNoDataDirectory
	}
	// The Rust application keeps its lock in the runtime directory and nowhere else: without one
	// it cannot be kept out while its files are copied.
	runtimeDir := c.Env["XDG_RUNTIME_DIR"]
	if !filepath.IsAbs(runtimeDir) {
		return nil, ErrNoRuntimeDirectory
	}
	target := c.DefaultConfigPath()
	return &rustimport.Importer{
		Paths: rustimport.Paths{
			SourceConfig: filepath.Join(filepath.Dir(filepath.Dir(target)), rustName, filepath.Base(target)),
			SourceDB:     filepath.Join(filepath.Dir(filepath.Dir(db)), rustName, rustName+".sqlite3"),
			SourceLock:   filepath.Join(runtimeDir, rustName, "instance.lock"),
			Config:       target, DB: db, Lock: own,
		},
		Locks: o.Locks, Files: o.Files, Secrets: o.Secrets, Legacy: o.LegacySecrets,
	}, nil
}

// ImportRust previews what an import from the Rust application (rust-microsip) would bring and,
// with apply, brings it: the configuration file, and the database with contacts, history and saved
// settings, replace go-microsip's, which are kept as backups. With credential the password is
// copied from the Rust application's keyring too. The Rust application's files are only read, and
// it must not be running. Nothing is ever imported without this call.
func ImportRust(o Options, apply, credential bool) (string, error) {
	i, err := o.rustImporter()
	if err != nil {
		return "", err
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

// recoverImportLocked undoes an import from the Rust application that was interrupted, so the
// database and the configuration file in use belong together. The caller holds go-microsip's
// instance lock; every start calls it before anything is read.
func recoverImportLocked(o Options) error {
	i, err := o.rustImporter()
	if err != nil {
		return nil // nothing could have started an import here
	}
	return i.RecoverLocked()
}

// recoverImport is recoverImportLocked for a command that does not hold the instance lock. While
// go-microsip runs, it has done the recovery itself; what must not happen is writing beside an
// import that is still in progress or was left unfinished.
func recoverImport(o Options) error {
	i, err := o.rustImporter()
	if err != nil {
		return nil
	}
	err = i.Recover()
	if errors.Is(err, rustimport.ErrRunning) {
		if i.Pending() {
			return errors.New("an import from rust-microsip is in progress or was interrupted; close go-microsip and try again")
		}
		return nil
	}
	return err
}
