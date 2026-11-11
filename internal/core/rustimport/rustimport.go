// Package rustimport brings the data of the Rust application (rust-microsip) into go-microsip. It
// runs only when the user asks, never at a first start, and never changes the source: the Rust
// application's files are only read, its instance lock is held while they are copied, and no
// database connection is ever opened on them.
//
// Apply follows the protocol of design D9 of migrate-to-go-wails: quiesce both applications,
// snapshot, migrate and validate the copy, stage the credential, seal, commit by renames. A journal
// records each step before it is taken, so Recover, at the next start or the next import, always
// puts back the database and the configuration file that belong together and removes a password
// that was staged for an import that did not finish.
package rustimport

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// Paths says where both applications keep their data.
type Paths struct {
	// SourceConfig is the Rust application's config.toml; it may not exist.
	SourceConfig string
	// SourceDB is the Rust application's database; it may not exist.
	SourceDB string
	// SourceLock is the Rust application's instance lock file.
	SourceLock string
	// Config and DB are go-microsip's configuration file and database.
	Config, DB string
	// Lock is go-microsip's own instance lock file.
	Lock string
}

func (p Paths) staging() string { return filepath.Join(filepath.Dir(p.DB), "import-staging") }
func (p Paths) journal() string { return filepath.Join(filepath.Dir(p.DB), "import.journal") }

// LegacySecrets reads the Rust application's keyring entries. Nothing is ever written there.
type LegacySecrets interface {
	Get(ref string) (password string, ok bool, err error)
}

// Importer runs the import. Locks and Files are required; the keyrings are optional.
type Importer struct {
	Paths Paths
	Locks ports.Locker
	Files ports.FileAccess
	// Secrets is go-microsip's keyring, where an imported password goes; nil when there is none.
	Secrets ports.Secrets
	// Legacy reads the password the Rust application stored; nil when it cannot be read.
	Legacy LegacySecrets

	// killAt makes Apply stop at a named point the way a killed process does: no cleanup, the
	// journal left as it is. snapshotting is called while the source is being copied. Tests only.
	killAt       string
	snapshotting func()
}

// Failures the user can act on.
var (
	// ErrNothingToImport: neither a configuration file nor a database of the Rust application.
	ErrNothingToImport = errors.New("no rust-microsip configuration or database was found")
	// ErrRustRunning: the Rust application holds its instance lock.
	ErrRustRunning = errors.New("rust-microsip is running; close it and run the import again")
	// ErrRunning: another go-microsip holds the instance lock.
	ErrRunning = errors.New("go-microsip is running; close it and run the import again")
	// ErrChanged: the source no longer matches the preview the user confirmed.
	ErrChanged = errors.New("the rust-microsip data changed since the preview; look at the preview again")
	// ErrNoCredential: the password was asked for and cannot be copied.
	ErrNoCredential = errors.New("the password cannot be copied: no keyring is available here, or rust-microsip stored none")

	// ErrCredentialCleanup: the files are back as they were, but the password staged for the
	// unfinished import is still in the keyring; the next recovery tries again.
	ErrCredentialCleanup = errors.New("the password staged for an unfinished import could not be removed from the keyring yet")

	errKilled = errors.New("killed") // the test's stand-in for a killed process
)

// Preview is what an import would bring, without any secret.
type Preview struct {
	// ConfigFile and Database tell which of the two sources exist.
	ConfigFile, Database bool
	// Account is the account of the Rust application as user@domain, or empty when it has none.
	Account string
	// Contacts and History count the rows of the Rust database.
	Contacts, History int
	// KeyringEntry: the Rust application stored the password in the keyring and it can be read.
	KeyringEntry bool
	// LiteralPassword: the Rust configuration file holds a literal password; the file is copied as
	// it is, so the password stays in a private file and nowhere else.
	LiteralPassword bool
}

// String is the preview shown before --apply.
func (p Preview) String() string {
	var b strings.Builder
	b.WriteString("import preview of rust-microsip (nothing written yet):\n")
	yes := map[bool]string{true: "found", false: "not found"}
	fmt.Fprintf(&b, "  configuration file: %s\n  database: %s\n", yes[p.ConfigFile], yes[p.Database])
	account := p.Account
	if account == "" {
		account = "(none)"
	}
	fmt.Fprintf(&b, "  account: %s\n  contacts: %d\n  call history: %d\n", account, p.Contacts, p.History)
	switch {
	case p.KeyringEntry:
		b.WriteString("  password: in the rust-microsip keyring; copied only with --credential\n")
	case p.LiteralPassword:
		b.WriteString("  password: literal in the configuration file; it stays in that private file\n")
	default:
		b.WriteString("  password: none stored\n")
	}
	b.WriteString("Run again with --apply to replace go-microsip's settings, contacts and history with these.\n" +
		"The rust-microsip files are only read. The current go-microsip database and configuration file are kept as backups.\n")
	return b.String()
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// copyFile copies src to a new private file dst and syncs it.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	return errors.Join(err, out.Close())
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// snapshot copies the source's configuration file and its database with the write-ahead log into
// dir. The source is only read: SQLite never opens it, so it gets no new sidecar file.
func (i *Importer) snapshot(dir string) (configCopy, dbCopy string, err error) {
	if exists(i.Paths.SourceConfig) {
		configCopy = filepath.Join(dir, "config.toml")
		if err := copyFile(i.Paths.SourceConfig, configCopy); err != nil {
			return "", "", err
		}
	}
	if exists(i.Paths.SourceDB) {
		dbCopy = filepath.Join(dir, "database.sqlite3")
		if err := copyFile(i.Paths.SourceDB, dbCopy); err != nil {
			return "", "", err
		}
		// What was committed but not yet checkpointed lives in the log; the shared-memory file is
		// only an index of it, which SQLite rebuilds.
		if wal := i.Paths.SourceDB + "-wal"; exists(wal) {
			if err := copyFile(wal, dbCopy+"-wal"); err != nil {
				return "", "", err
			}
		}
	}
	if i.snapshotting != nil {
		i.snapshotting()
	}
	if configCopy == "" && dbCopy == "" {
		return "", "", ErrNothingToImport
	}
	return configCopy, dbCopy, nil
}

// inspect opens the copies and describes them. The returned store, when there is a database, is the
// migrated copy, still open.
func (i *Importer) inspect(configCopy, dbCopy string) (Preview, *store.Store, error) {
	p := Preview{ConfigFile: configCopy != "", Database: dbCopy != ""}
	settings := map[string]string{}
	var s *store.Store
	if dbCopy != "" {
		var err error
		if s, err = store.Open(dbCopy); err != nil {
			return p, nil, fmt.Errorf("the rust-microsip database cannot be used: %w", err)
		}
		fail := func(err error) (Preview, *store.Store, error) {
			_ = s.Close()
			return p, nil, err
		}
		if err := s.CheckIntegrity(); err != nil {
			return fail(fmt.Errorf("the rust-microsip database cannot be used: %w", err))
		}
		if p.Contacts, p.History, err = s.Counts(); err != nil {
			return fail(err)
		}
		if settings, err = s.LoadConfig(); err != nil {
			return fail(err)
		}
		ref, err := s.SecretRef()
		if err != nil {
			return fail(err)
		}
		if ref != "" && i.Legacy != nil {
			_, p.KeyringEntry, _ = i.Legacy.Get(ref) // an unreadable keyring is "no entry", not a failure
		}
	}
	if configCopy != "" {
		// The copy is ours and private; what is checked here is that the file is one this
		// application reads (schema, keys, types).
		values, literal, err := config.FileValues(configCopy, i.Files)
		if err != nil {
			if s != nil {
				_ = s.Close()
			}
			return p, nil, fmt.Errorf("the rust-microsip configuration file cannot be used: %w", err)
		}
		p.LiteralPassword = literal
		for f, v := range values {
			settings[f.Key()] = v // the file wins over the database, as when the application runs
		}
	}
	if user, domain := settings["account.username"], settings["account.domain"]; user != "" && domain != "" {
		p.Account = user + "@" + domain
	}
	return p, s, nil
}

// Preview describes what an import would bring. It takes the Rust application's lock while it
// copies, works on a private temporary copy, and leaves nothing behind.
func (i *Importer) Preview() (Preview, error) {
	unlock, err := i.Locks.Lock(i.Paths.SourceLock)
	if errors.Is(err, ports.ErrLocked) {
		return Preview{}, ErrRustRunning
	}
	if err != nil {
		return Preview{}, err
	}
	dir, err := os.MkdirTemp("", "go-microsip-import-")
	if err != nil {
		unlock()
		return Preview{}, err
	}
	defer os.RemoveAll(dir)
	configCopy, dbCopy, err := i.snapshot(dir)
	unlock()
	if err != nil {
		return Preview{}, err
	}
	p, s, err := i.inspect(configCopy, dbCopy)
	if s != nil {
		_ = s.Close()
	}
	return p, err
}

// journal is the record of an import in progress. It is written and synced before the step it
// names, so recovery always knows what may exist.
type journal struct {
	// Step is the last step that was begun.
	Step string `json:"step"`
	// Credential is the keyring reference allocated for the imported password. It is recorded
	// before the password is written, so recovery knows what to remove either way.
	Credential string `json:"credential,omitempty"`
	// HadDB and HadConfig tell whether go-microsip had a database and a configuration file.
	HadDB     bool `json:"had_db"`
	HadConfig bool `json:"had_config"`
	// DBBackup and ConfigBackup are where they are moved to.
	DBBackup     string `json:"db_backup,omitempty"`
	ConfigBackup string `json:"config_backup,omitempty"`
	// NewConfig tells whether the import brings a configuration file.
	NewConfig bool `json:"new_config"`
	// Config is go-microsip's configuration file as the import knew it: recovery restores that
	// file, also from a session whose environment names another one, or none.
	Config string `json:"config,omitempty"`
}

// Steps of the journal. Everything before stepCommit leaves go-microsip's files untouched.
const (
	stepStaging = "staging"
	stepCommit  = "commit"
	stepDone    = "done"
	// stepCleanup: the files are settled; only the staged password is still to be removed.
	stepCleanup = "cleanup"
)

// syncDirs makes the renames in go-microsip's two directories durable.
func (i *Importer) syncDirs() error {
	err := syncDir(filepath.Dir(i.Paths.DB))
	if config := filepath.Dir(i.Paths.Config); i.Paths.Config != "" && exists(config) {
		err = errors.Join(err, syncDir(config))
	}
	return err
}

// mkdirDurable creates a private directory with its missing parents and syncs the directory each
// new one was made in, so that a file renamed into it does not vanish with it at a power loss.
func mkdirDurable(dir string) error {
	var made []string
	for d := dir; !exists(d); d = filepath.Dir(d) {
		made = append(made, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, d := range made {
		if err := syncDir(filepath.Dir(d)); err != nil {
			return err
		}
	}
	return nil
}

func (i *Importer) writeJournal(j journal) error {
	path := i.Paths.journal()
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func (i *Importer) kill(point string) error {
	if i.killAt == point {
		return errKilled
	}
	return nil
}

// removeDatabase removes a database file with its sidecars.
func removeDatabase(path string) error {
	var all error
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			all = errors.Join(all, err)
		}
	}
	return all
}

// Pending reports whether an import was begun and is neither finished nor undone.
func (i *Importer) Pending() bool { return exists(i.Paths.journal()) }

// Recover finishes or undoes an import that did not end. An import interrupted before its commit is
// discarded; one interrupted during its commit is undone, so the previous database and the previous
// configuration file are in use together again. Either way the password staged for it is removed
// from the keyring. Without a journal it does nothing: a first start imports nothing.
//
// It takes go-microsip's instance lock for itself, so it cannot run beside an import, another
// recovery or a running go-microsip; a caller that holds the lock uses RecoverLocked.
func (i *Importer) Recover() error {
	unlock, err := i.Locks.Lock(i.Paths.Lock)
	if errors.Is(err, ports.ErrLocked) {
		return ErrRunning
	}
	if err != nil {
		return err
	}
	defer unlock()
	return i.RecoverLocked()
}

// RecoverLocked is Recover for a caller that holds go-microsip's instance lock.
func (i *Importer) RecoverLocked() error {
	raw, err := os.ReadFile(i.Paths.journal())
	if errors.Is(err, fs.ErrNotExist) {
		_ = os.Remove(i.Paths.journal() + ".tmp")
		return nil
	}
	if err != nil {
		return err
	}
	var j journal
	if err := json.Unmarshal(raw, &j); err != nil {
		return fmt.Errorf("the import journal cannot be read: %w", err)
	}
	if j.Config != "" {
		i.Paths.Config = j.Config
	}
	if j.Step == stepCommit {
		// Put back what was moved away; what took its place came from the import.
		if j.HadDB {
			if exists(j.DBBackup) {
				if err := errors.Join(removeDatabase(i.Paths.DB), os.Rename(j.DBBackup, i.Paths.DB)); err != nil {
					return err
				}
			}
		} else if err := removeDatabase(i.Paths.DB); err != nil {
			return err
		}
		if err := i.kill("recover-db-restored"); err != nil {
			return err
		}
		if j.HadConfig {
			if exists(j.ConfigBackup) {
				_ = os.Remove(i.Paths.Config)
				if err := os.Rename(j.ConfigBackup, i.Paths.Config); err != nil {
					return err
				}
			}
		} else if j.NewConfig {
			if err := os.Remove(i.Paths.Config); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	if err := os.RemoveAll(i.Paths.staging()); err != nil {
		return err
	}
	// The files are settled; that must be on disk before the journal that says how to settle them
	// is changed or removed.
	if err := i.syncDirs(); err != nil {
		return err
	}
	if err := i.kill("recover-files-restored"); err != nil {
		return err
	}
	if j.Step != stepDone && j.Credential != "" {
		err := ports.ErrNoSecretService
		if i.Secrets != nil {
			err = i.Secrets.Delete(j.Credential)
		}
		if err != nil {
			// The reference is the only way to find the entry again: it stays on record.
			if werr := i.writeJournal(journal{Step: stepCleanup, Credential: j.Credential}); werr != nil {
				return errors.Join(ErrCredentialCleanup, err, werr)
			}
			return fmt.Errorf("%w: %v", ErrCredentialCleanup, err)
		}
	}
	if err := os.Remove(i.Paths.journal()); err != nil {
		return err
	}
	return syncDir(filepath.Dir(i.Paths.journal()))
}

// Apply imports what confirmed describes. With credential it also copies the password from the Rust
// application's keyring into go-microsip's. On any failure go-microsip's previous data is what
// remains, and the import can be run again. go-microsip's instance lock is held from before the
// recovery of an earlier attempt until after the cleanup of this one.
func (i *Importer) Apply(confirmed Preview, credential bool) error {
	if err := mkdirDurable(filepath.Dir(i.Paths.DB)); err != nil {
		return err
	}
	unlock, err := i.Locks.Lock(i.Paths.Lock)
	if errors.Is(err, ports.ErrLocked) {
		return ErrRunning
	}
	if err != nil {
		return err
	}
	defer unlock()
	if err := i.RecoverLocked(); err != nil {
		return err
	}
	err = i.apply(confirmed, credential)
	if err != nil && !errors.Is(err, errKilled) {
		// One way back for every failure: the same recovery a restart would run.
		if rerr := i.RecoverLocked(); rerr != nil {
			return errors.Join(err, rerr)
		}
	}
	return err
}

func (i *Importer) apply(confirmed Preview, credential bool) (err error) {
	p := i.Paths
	// 1. Quiesce both sides. go-microsip's own lock is already held, to the end; its store is
	// closed.
	unlockRust, err := i.Locks.Lock(p.SourceLock)
	if errors.Is(err, ports.ErrLocked) {
		return ErrRustRunning
	}
	if err != nil {
		return err
	}
	rustLocked := true
	releaseRust := func() {
		if rustLocked {
			rustLocked = false
			unlockRust()
		}
	}
	defer releaseRust()
	if credential && (i.Secrets == nil || i.Legacy == nil) {
		return ErrNoCredential
	}
	j := journal{Step: stepStaging, HadDB: exists(p.DB), HadConfig: exists(p.Config), Config: p.Config}
	if err := i.writeJournal(j); err != nil {
		return err
	}
	if err := i.kill("quiesced"); err != nil {
		return err
	}

	// 2. Snapshot, with the Rust application locked out until the copy is complete.
	staging := p.staging()
	if err := os.Mkdir(staging, 0o700); err != nil {
		return err
	}
	configCopy, dbCopy, err := i.snapshot(staging)
	if err != nil {
		return err
	}
	releaseRust()
	if err := i.kill("snapshot"); err != nil {
		return err
	}

	// 3. Migrate and validate the copy; it must be what the user confirmed.
	found, s, err := i.inspect(configCopy, dbCopy)
	if err != nil {
		return err
	}
	closeStore := func() error {
		if s == nil {
			return nil
		}
		err := s.Close()
		s = nil
		return err
	}
	defer func() {
		if !errors.Is(err, errKilled) { // a killed process closes nothing
			_ = closeStore()
		}
	}()
	if found != confirmed {
		return ErrChanged
	}
	if s == nil { // the source has a configuration file only: go-microsip gets an empty database
		dbCopy = filepath.Join(staging, "database.sqlite3")
		if s, err = store.Open(dbCopy); err != nil {
			return err
		}
	}
	if err := i.kill("migrated"); err != nil {
		return err
	}

	// 4. The credential. The reference the Rust application stored names an entry of its keyring,
	// which means nothing here: it is replaced or cleared.
	legacyRef, err := s.SecretRef()
	if err != nil {
		return err
	}
	if credential {
		password, ok, err := i.Legacy.Get(legacyRef)
		if err != nil || !ok || legacyRef == "" {
			return errors.Join(ErrNoCredential, err)
		}
		id := make([]byte, 16)
		if _, err := rand.Read(id); err != nil {
			return err
		}
		j.Credential = "go-microsip/" + hex.EncodeToString(id)
		if err := i.writeJournal(j); err != nil { // the reference first: recovery knows what to remove
			return err
		}
		if err := i.kill("credential-journaled"); err != nil {
			return err
		}
		if err := i.Secrets.Set(j.Credential, password); err != nil {
			return fmt.Errorf("the password could not be stored in the keyring: %w", err)
		}
		if err := i.kill("credential-written"); err != nil {
			return err
		}
	}
	if err := s.SetSecretRef(j.Credential); err != nil {
		return err
	}
	if err := i.kill("credential"); err != nil {
		return err
	}

	// 5. Seal: everything into the one database file, no sidecar left, all of it on disk.
	if err := s.Checkpoint(); err != nil {
		return err
	}
	if err := closeStore(); err != nil {
		return err
	}
	for _, sidecar := range []string{dbCopy + "-wal", dbCopy + "-shm"} {
		if exists(sidecar) {
			return fmt.Errorf("the imported database was not sealed: %s remains", filepath.Base(sidecar))
		}
	}
	for _, file := range []string{dbCopy, configCopy} {
		if file == "" {
			continue
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		err = errors.Join(f.Sync(), f.Close())
		if err != nil {
			return err
		}
	}
	if err := syncDir(staging); err != nil {
		return err
	}
	if err := i.kill("sealed"); err != nil {
		return err
	}

	// 6. Commit. From here go-microsip's own files move; the journal says where to, before they do.
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	j.Step, j.NewConfig = stepCommit, configCopy != ""
	if j.HadDB {
		j.DBBackup = p.DB + ".before-import-" + stamp
	}
	if j.HadConfig {
		j.ConfigBackup = p.Config + ".before-import-" + stamp
	}
	if err := i.writeJournal(j); err != nil {
		return err
	}
	if err := i.kill("commit-journaled"); err != nil {
		return err
	}
	if j.HadDB {
		// A database go-microsip closed normally has no sidecar; one left by a crash is folded in
		// first, so the backup is one file too.
		old, err := store.Open(p.DB)
		if err == nil {
			err = errors.Join(old.Checkpoint(), old.Close())
		}
		if err != nil {
			return fmt.Errorf("go-microsip's database cannot be backed up: %w", err)
		}
		if err := os.Rename(p.DB, j.DBBackup); err != nil {
			return err
		}
	}
	if err := i.kill("db-backed-up"); err != nil {
		return err
	}
	if j.HadConfig {
		if err := os.Rename(p.Config, j.ConfigBackup); err != nil {
			return err
		}
	}
	if err := i.kill("config-backed-up"); err != nil {
		return err
	}
	if err := os.Rename(dbCopy, p.DB); err != nil {
		return err
	}
	if err := i.kill("db-in-place"); err != nil {
		return err
	}
	if configCopy != "" {
		if err := mkdirDurable(filepath.Dir(p.Config)); err != nil {
			return err
		}
		if err := os.Rename(configCopy, p.Config); err != nil {
			return err
		}
	}
	if err := i.kill("config-in-place"); err != nil {
		return err
	}
	// Every rename, in both directories, is on disk before the journal says the import is done.
	if err := i.syncDirs(); err != nil {
		return err
	}
	j.Step = stepDone
	if err := i.writeJournal(j); err != nil {
		return err
	}
	if err := i.kill("done-journaled"); err != nil {
		return err
	}
	return i.RecoverLocked() // done: this only removes the staging directory and the journal
}
