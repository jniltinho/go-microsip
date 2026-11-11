// Package store is the local SQLite persistence for call history, contacts and the settings the
// user edited. It keeps the Rust candidate's schema and continues its migration numbering, so a
// database written by that application opens here unchanged.
//
// A Store owns one connection. Every query is parameterized, and user data never appears in an
// Error's text.
//
// On disk the database lives in a 0700 directory as a 0600 file; SQLite creates its -wal and -shm
// companions with the mode of the database file. A schema upgrade is one transaction keyed by
// PRAGMA user_version, preceded by a backup, and a database written by a newer release is refused
// without being modified.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"modernc.org/sqlite"
)

const (
	// SchemaVersion is the newest schema this release can read and write.
	SchemaVersion = 5
	// DefaultRetentionDays is the default call-history retention.
	DefaultRetentionDays = 90
	// MaxNumbersPerContact bounds the numbers stored for one contact.
	MaxNumbersPerContact = 32
	// MaxNameLen bounds a contact name, in bytes.
	MaxNameLen = 128
	// MaxLabelLen bounds a number label, in bytes.
	MaxLabelLen = 64
	// MaxRemoteLen bounds a call's remote party text, in bytes.
	MaxRemoteLen = 256
	// MaxSessionKeyLen bounds a call session key, in bytes.
	MaxSessionKeyLen = 128

	busyTimeout = 2 * time.Second
)

// migrations are the Rust candidate's, verbatim; new ones are appended and never edited.
var migrations = [SchemaVersion]string{`
CREATE TABLE calls (
    id INTEGER PRIMARY KEY,
    session_key TEXT NOT NULL UNIQUE,
    started_utc INTEGER NOT NULL,
    answered_utc INTEGER,
    ended_utc INTEGER,
    direction TEXT NOT NULL CHECK (direction IN ('incoming', 'outgoing')),
    remote TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN
        ('answered', 'missed', 'canceled', 'rejected', 'busy', 'no_answer', 'failed', 'interrupted')),
    duration_ms INTEGER CHECK (duration_ms >= 0),
    secure INTEGER NOT NULL CHECK (secure IN (0, 1))
);
CREATE INDEX calls_started ON calls (started_utc DESC, id DESC);
CREATE INDEX calls_remote ON calls (remote);
CREATE TABLE contacts (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    favorite INTEGER NOT NULL DEFAULT 0 CHECK (favorite IN (0, 1)),
    created_utc INTEGER NOT NULL
);
CREATE INDEX contacts_order ON contacts (favorite DESC, name COLLATE NOCASE, id);
CREATE TABLE contact_numbers (
    id INTEGER PRIMARY KEY,
    contact_id INTEGER NOT NULL REFERENCES contacts (id) ON DELETE CASCADE,
    number TEXT NOT NULL,
    label TEXT,
    UNIQUE (contact_id, number)
);
CREATE INDEX contact_numbers_number ON contact_numbers (number);
`,
	// v2: the settings the user edited. Columns are nullable: NULL means "not stored", so the field
	// falls through to the built-in default. Never a password.
	`
CREATE TABLE accounts (
    id INTEGER PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    display_name TEXT,
    username TEXT,
    auth_username TEXT,
    domain TEXT,
    server TEXT,
    port INTEGER CHECK (port BETWEEN 1 AND 65535),
    transport TEXT CHECK (transport IN ('udp', 'tcp', 'tls')),
    media TEXT CHECK (media IN ('rtp', 'srtp')),
    expires INTEGER CHECK (expires BETWEEN 1 AND 86400),
    password_env TEXT,
    password_file TEXT,
    ca_file TEXT,
    secret_ref TEXT,
    updated_utc INTEGER NOT NULL
);
CREATE UNIQUE INDEX accounts_one_enabled ON accounts (enabled) WHERE enabled = 1;
CREATE TABLE preferences (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
) WITHOUT ROWID;
`,
	// v3: the call journal. A call is written when it starts with open = 1 and the outcome it would
	// have if the process died (interrupted); the final record replaces it. Open rows are hidden
	// from history until recovered.
	`
ALTER TABLE calls ADD COLUMN open INTEGER NOT NULL DEFAULT 0 CHECK (open IN (0, 1));
`,
	// v4: control ledger, audit and owner epochs (CLI/MCP). Payloads are only HMACs under a
	// per-epoch key held in memory; no digits or full numbers.
	`
CREATE TABLE control_epochs (
    epoch INTEGER PRIMARY KEY,
    started_utc INTEGER NOT NULL
);
CREATE TABLE control_operations (
    operation_id TEXT PRIMARY KEY,
    epoch INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL,
    operation TEXT NOT NULL,
    payload_mac BLOB NOT NULL CHECK (length(payload_mac) = 32),
    state TEXT NOT NULL CHECK (state IN ('awaiting_approval', 'executing', 'succeeded',
        'failed', 'denied', 'expired', 'outcome_unknown')),
    outcome TEXT,
    created_utc INTEGER NOT NULL,
    updated_utc INTEGER NOT NULL,
    UNIQUE (epoch, idempotency_key)
);
CREATE INDEX control_operations_key ON control_operations (idempotency_key, epoch DESC);
CREATE TABLE control_audit (
    id INTEGER PRIMARY KEY,
    at_utc INTEGER NOT NULL,
    operation TEXT NOT NULL,
    decision TEXT NOT NULL,
    detail TEXT NOT NULL
);
CREATE INDEX control_audit_at ON control_audit (at_utc);
`,
	// v5: control grants. Only the SHA-256 of a capability is stored.
	`
CREATE TABLE control_grants (
    id TEXT PRIMARY KEY,
    capability_hash BLOB NOT NULL UNIQUE CHECK (length(capability_hash) = 32),
    scopes TEXT NOT NULL,
    destinations TEXT NOT NULL,
    incoming TEXT NOT NULL,
    expires_utc INTEGER NOT NULL,
    quota INTEGER NOT NULL CHECK (quota >= 0),
    used INTEGER NOT NULL DEFAULT 0 CHECK (used >= 0),
    epoch INTEGER,
    bootstrap_id TEXT UNIQUE,
    bootstrap_consumed INTEGER NOT NULL DEFAULT 0 CHECK (bootstrap_consumed IN (0, 1)),
    revoked INTEGER NOT NULL DEFAULT 0 CHECK (revoked IN (0, 1)),
    created_utc INTEGER NOT NULL
);
`,
}

// failAfterMigration is test-only fault injection: fail right after applying this migration.
var failAfterMigration int

// InvalidField says which input failed validation. It carries no user data.
type InvalidField string

// Inputs that are validated.
const (
	SessionKey      InvalidField = "session key"      // empty, too long or with control characters
	Remote          InvalidField = "remote party"     // too long or with control characters
	Name            InvalidField = "contact name"     // empty, too long or with control characters
	Number          InvalidField = "phone number"     // not a valid dial target
	Label           InvalidField = "number label"     // too long or with control characters
	NumberCount     InvalidField = "number count"     // no numbers, or more than MaxNumbersPerContact
	DuplicateNumber InvalidField = "duplicate number" // the same number twice in one contact
	Duration        InvalidField = "duration"         // negative
	ConfigKey       InvalidField = "settings key"     // outside ConfigKeys
	ConfigValue     InvalidField = "settings value"   // too long, control characters, or not a number
	SecretRef       InvalidField = "secret reference" // empty, too long or with control characters
)

// ErrorKind classifies a storage failure.
type ErrorKind uint8

// Storage failures.
const (
	IO                ErrorKind = iota // the directory or file could not be prepared
	NotRegularFile                     // the path exists but is not a regular file (a symlink, say)
	FutureSchema                       // written by a newer release; left untouched
	Busy                               // another process holds the lock beyond the busy timeout
	Corrupt                            // corrupt, or not a SQLite database
	Full                               // the disk is full
	SQLite                             // any other SQLite failure
	Invalid                            // an input failed validation
	NotFound                           // the record does not exist
	BackupExists                       // the backup destination exists; it is never overwritten
	CSVTooLarge                        // the CSV input exceeds CSVMaxBytes
	CSVTooManyRecords                  // the CSV input exceeds CSVMaxRecords
	CSVHeader                          // the first line is not name,number or name,number,label
	CSVInvalid                         // the preview has invalid rows; nothing was imported
)

// Error is a storage failure. Its text never includes user data or a path.
type Error struct {
	Kind ErrorKind
	// Field is set for Invalid.
	Field InvalidField
	// Found and Supported are the schema versions of FutureSchema.
	Found, Supported int64
	// Err is the cause, for IO and SQLite.
	Err error
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Error() string {
	switch e.Kind {
	case IO:
		return "the storage directory or file could not be prepared"
	case NotRegularFile:
		return "the storage path is not a regular file"
	case FutureSchema:
		return fmt.Sprintf("the database schema version %d is newer than the supported version %d", e.Found, e.Supported)
	case Busy:
		return "the database is locked by another process"
	case Corrupt:
		return "the database file is corrupt or not a database"
	case Full:
		return "the disk is full"
	case Invalid:
		return "invalid " + string(e.Field)
	case NotFound:
		return "the record was not found"
	case BackupExists:
		return "the backup destination already exists"
	case CSVTooLarge:
		return "the CSV file exceeds the size limit"
	case CSVTooManyRecords:
		return "the CSV file exceeds the record limit"
	case CSVHeader:
		return "the CSV header must be name,number or name,number,label"
	case CSVInvalid:
		return "the CSV preview contains invalid rows"
	}
	return "a database operation failed"
}

func invalid(f InvalidField) error { return &Error{Kind: Invalid, Field: f} }

// wrap classifies a SQLite or file-system error; an Error passes through.
func wrap(err error) error {
	var own *Error
	var db *sqlite.Error
	switch {
	case err == nil, errors.As(err, &own):
		return err
	case errors.As(err, &db):
		switch db.Code() & 0xff { // the primary result code
		case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED
			return &Error{Kind: Busy}
		case 11, 26: // SQLITE_CORRUPT, SQLITE_NOTADB
			return &Error{Kind: Corrupt}
		case 13: // SQLITE_FULL
			return &Error{Kind: Full}
		}
		return &Error{Kind: SQLite, Err: err}
	case errors.As(err, new(*fs.PathError)), errors.As(err, new(*os.LinkError)):
		return &Error{Kind: IO, Err: err}
	}
	return &Error{Kind: SQLite, Err: err}
}

// Store is the handle to the database.
type Store struct {
	db *sql.DB
}

// Open opens or creates the database at path and migrates it.
//
// A missing parent directory is created with mode 0700 and the file with mode 0600; a broader mode
// of an existing file is tightened to 0600. Before an existing older schema is upgraded, a
// consistent 0600 copy is written beside the file as <file>.v<old>.bak (or <file>.v<old>.<n>.bak
// when an earlier attempt left one). The upgrade is one transaction: interrupted or failed, it
// leaves the previous version intact. Nothing is ever deleted.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, wrap(err)
			}
		}
	}
	if err := createPrivateFile(path, false); err != nil {
		return nil, err
	}
	s, err := open((&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String())
	if err != nil {
		return nil, err
	}
	version, err := s.SchemaVersion()
	if err == nil && version > 0 && version < SchemaVersion {
		err = s.backupBeforeMigration(path, version)
	}
	if err == nil {
		err = s.migrate()
	}
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// OpenInMemory opens a migrated in-memory database, for tests and previews.
func OpenInMemory() (*Store, error) {
	s, err := open("file::memory:")
	if err == nil {
		if err = s.migrate(); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	return s, err
}

func open(uri string) (*Store, error) {
	// The pragmas apply to every connection the pool opens; write transactions take the lock at
	// BEGIN, so a busy database is reported before anything is changed.
	db, err := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)&_txlock=immediate",
		uri, busyTimeout.Milliseconds()))
	if err != nil {
		return nil, wrap(err)
	}
	db.SetMaxOpenConns(1) // one connection: the store's writes are serialized, and :memory: is one database
	s := &Store{db: db}
	// The version is read before anything that could write the header.
	found, err := s.SchemaVersion()
	if err == nil && found > SchemaVersion {
		err = &Error{Kind: FutureSchema, Found: found, Supported: SchemaVersion}
	}
	if err == nil {
		_, err = db.Exec("PRAGMA journal_mode = WAL")
	}
	if err != nil {
		_ = db.Close()
		return nil, wrap(err)
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return wrap(s.db.Close()) }

// SchemaVersion is the schema version of the open database.
func (s *Store) SchemaVersion() (int64, error) {
	var v int64
	return v, wrap(s.db.QueryRow("PRAGMA user_version").Scan(&v))
}

// tx runs f in one transaction: committed when f returns nil, rolled back otherwise.
func (s *Store) tx(f func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return wrap(err)
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return wrap(err)
	}
	return wrap(tx.Commit())
}

func (s *Store) migrate() error {
	return s.tx(func(tx *sql.Tx) error {
		var current int64
		if err := tx.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
			return err
		}
		if current > SchemaVersion {
			return &Error{Kind: FutureSchema, Found: current, Supported: SchemaVersion}
		}
		for i, ddl := range migrations {
			if version := int64(i + 1); version > current {
				if _, err := tx.Exec(ddl); err != nil {
					return err
				}
				if failAfterMigration == i+1 {
					return &Error{Kind: IO, Err: errors.New("injected")}
				}
			}
		}
		if current < SchemaVersion {
			_, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion))
			return err
		}
		return nil
	})
}

// BackupTo writes a consistent copy to a new 0600 file. An existing destination is never
// overwritten, and a partial destination is removed.
func (s *Store) BackupTo(path string) error {
	if err := createPrivateFile(path, true); err != nil {
		return err
	}
	// VACUUM INTO fills the empty file just created, so the copy keeps its private mode.
	if _, err := s.db.Exec("VACUUM INTO ?", path); err != nil {
		_ = os.Remove(path)
		return wrap(err)
	}
	return nil
}

func (s *Store) backupBeforeMigration(path string, version int64) error {
	for attempt := 0; attempt < 100; attempt++ {
		target := fmt.Sprintf("%s.v%d.bak", path, version)
		if attempt > 0 {
			target = fmt.Sprintf("%s.v%d.%d.bak", path, version, attempt)
		}
		var exists *Error
		if err := s.BackupTo(target); !errors.As(err, &exists) || exists.Kind != BackupExists {
			return err
		}
	}
	return &Error{Kind: BackupExists}
}

// createPrivateFile creates a regular 0600 file or, unless newOnly, reuses one.
func createPrivateFile(path string, newOnly bool) error {
	// shortcut: the symlink check and the open are two steps; closing the gap needs O_NOFOLLOW,
	// which is not portable. The directory is private (0700), so only the user can race it.
	if info, err := os.Lstat(path); err == nil {
		if newOnly {
			return &Error{Kind: BackupExists}
		}
		if !info.Mode().IsRegular() {
			return &Error{Kind: NotRegularFile}
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	if newOnly {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return &Error{Kind: BackupExists}
	}
	if err != nil {
		return wrap(err)
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Mode().Perm()&0o077 != 0 {
		return wrap(file.Chmod(0o600))
	}
	return nil
}

// textOK: not empty, bounded, no control characters.
func textOK(value string, max int) bool {
	return value != "" && len(value) <= max && strings.IndexFunc(value, unicode.IsControl) < 0
}

// likePattern matches query as a literal substring: %, _ and \ are escaped for LIKE ... ESCAPE '\'.
func likePattern(query string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
}

func nowUTC() int64 { return time.Now().Unix() }
