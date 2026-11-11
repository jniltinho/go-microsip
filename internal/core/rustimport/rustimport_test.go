package rustimport

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

const (
	password  = "fictional-secret-marker-4410"
	legacyRef = "keyring:rust-microsip/1001"
	rustTOML  = "schema = 1\n[account]\nusername = \"1001\"\ndomain = \"pbx.invalid\"\ntransport = \"tls\"\nmedia = \"srtp\"\nca_file = \"/etc/lab-ca.pem\"\n[ui]\ntheme = \"light\"\n"
	oldTOML   = "schema = 1\n[account]\nusername = \"9000\"\ndomain = \"old.invalid\"\n"
)

// own is a ports.FileAccess that owns every file.
type own struct{}

func (own) CurrentUser() uint32 { return 1000 }
func (own) Inspect(info os.FileInfo) (owner, mode uint32) {
	return 1000, uint32(info.Mode().Perm())
}

// world is both applications' directories and the adapters of one test.
type world struct {
	t       *testing.T
	rust    string // the Rust application's directories: only ever read
	paths   Paths
	locks   *memory.Locks
	keyring *memory.Keyring // go-microsip's
	legacy  *memory.Keyring // the Rust application's
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	w := &world{t: t, rust: filepath.Join(dir, "rust"), locks: &memory.Locks{}, keyring: &memory.Keyring{}, legacy: &memory.Keyring{}}
	w.paths = Paths{
		SourceConfig: filepath.Join(w.rust, "config", "config.toml"),
		SourceDB:     filepath.Join(w.rust, "data", "rust-microsip.sqlite3"),
		SourceLock:   filepath.Join(dir, "run", "rust-microsip", "instance.lock"),
		Config:       filepath.Join(dir, "go", "config", "config.toml"),
		DB:           filepath.Join(dir, "go", "data", "go-microsip.sqlite3"),
		Lock:         filepath.Join(dir, "run", "go-microsip", "instance.lock"),
	}
	for _, d := range []string{filepath.Dir(w.paths.SourceConfig), filepath.Dir(w.paths.SourceDB)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// importer is a new importer on the world, as a new process would make one.
func (w *world) importer() *Importer {
	return &Importer{Paths: w.paths, Locks: w.locks, Files: own{}, Secrets: w.keyring, Legacy: w.legacy}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// fill puts settings, contacts and finished calls into a database.
func fill(t *testing.T, s *store.Store, settings map[string]string, contacts, calls int, secretRef string) {
	t.Helper()
	changes := map[string]*string{}
	for k, v := range settings {
		changes[k] = &v
	}
	check(t, s.SaveConfig(changes))
	for n := range contacts {
		_, err := s.AddContact(store.NewContact{Name: fmt.Sprintf("Contact %d", n), Numbers: []store.ContactNumber{{Number: fmt.Sprintf("20%02d", n)}}})
		check(t, err)
	}
	for n := range calls {
		_, err := s.RecordCall(store.CallRecord{SessionKey: fmt.Sprintf("r:%d", n), StartedUTC: int64(1000 + n), Direction: domain.Outgoing, Remote: "1002", Outcome: store.Answered})
		check(t, err)
	}
	if secretRef != "" {
		check(t, s.SetSecretRef(secretRef))
	}
}

// rustDatabase writes the Rust application's database. With pendingWAL the files are taken while a
// connection is still open, as after a kill: the data is in the write-ahead log, not in the main
// file.
func (w *world) rustDatabase(settings map[string]string, contacts, calls int, secretRef string, pendingWAL bool) {
	w.t.Helper()
	scratch := filepath.Join(w.t.TempDir(), "build.sqlite3")
	s, err := store.Open(scratch)
	check(w.t, err)
	fill(w.t, s, settings, contacts, calls, secretRef)
	copyOver := func(suffix string) {
		raw, err := os.ReadFile(scratch + suffix)
		check(w.t, err)
		check(w.t, os.WriteFile(w.paths.SourceDB+suffix, raw, 0o600))
	}
	if pendingWAL {
		copyOver("")
		copyOver("-wal")
		if info, err := os.Stat(w.paths.SourceDB + "-wal"); err != nil || info.Size() == 0 {
			w.t.Fatalf("the fixture has no pending write-ahead log: %v", err)
		}
	}
	check(w.t, s.Close())
	if !pendingWAL {
		copyOver("")
	}
}

// previous gives go-microsip a database and a configuration file of its own.
func (w *world) previous() {
	w.t.Helper()
	write(w.t, w.paths.Config, oldTOML)
	s, err := store.Open(w.paths.DB)
	check(w.t, err)
	fill(w.t, s, map[string]string{"ui.theme": "dark"}, 0, 0, "")
	_, err = s.AddContact(store.NewContact{Name: "Old", Numbers: []store.ContactNumber{{Number: "9001"}}})
	check(w.t, err)
	check(w.t, s.Close())
}

// fingerprint is every file under dir with its mode and content hash.
func fingerprint(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[rel] = fmt.Sprintf("%o %x", info.Mode().Perm(), sha256.Sum256(raw))
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return out
}

// contacts lists the names in go-microsip's database, and its settings and keyring reference.
func (w *world) database() (names []string, settings map[string]string, secretRef string, version int64) {
	w.t.Helper()
	s, err := store.Open(w.paths.DB)
	check(w.t, err)
	defer s.Close()
	found, err := s.SearchContacts("", 100)
	check(w.t, err)
	for _, c := range found {
		names = append(names, c.Name)
	}
	settings, err = s.LoadConfig()
	check(w.t, err)
	secretRef, err = s.SecretRef()
	check(w.t, err)
	version, err = s.SchemaVersion()
	check(w.t, err)
	return names, settings, secretRef, version
}

// clean checks that an import left nothing of its own behind.
func (w *world) clean() {
	w.t.Helper()
	entries, err := os.ReadDir(filepath.Dir(w.paths.DB))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		w.t.Fatal(err)
	}
	for _, e := range entries {
		if name := e.Name(); name != filepath.Base(w.paths.DB) && !strings.Contains(name, ".before-import-") {
			w.t.Errorf("left in the data directory: %s", name)
		}
	}
	if w.importer().Pending() {
		w.t.Error("an import journal is still there")
	}
	if w.locks.Held(w.paths.SourceLock) || w.locks.Held(w.paths.Lock) {
		w.t.Error("an instance lock is still held")
	}
}

// previousIntact checks that go-microsip's own data is what it was before the import began.
func (w *world) previousIntact(config map[string]string) {
	w.t.Helper()
	if now := fingerprint(w.t, filepath.Dir(w.paths.Config)); !maps.Equal(now, config) {
		w.t.Errorf("the configuration directory changed: %v, was %v", now, config)
	}
	names, settings, ref, _ := w.database()
	if len(names) != 1 || names[0] != "Old" || settings["ui.theme"] != "dark" || len(settings) != 1 || ref != "" {
		w.t.Errorf("the database is not the previous one: contacts %v, settings %v, reference %q", names, settings, ref)
	}
	if refs := w.keyring.Refs(); len(refs) != 0 {
		w.t.Errorf("a staged password stayed in the keyring: %v", refs)
	}
	entries, _ := os.ReadDir(filepath.Dir(w.paths.DB))
	for _, e := range entries {
		if e.Name() != filepath.Base(w.paths.DB) {
			w.t.Errorf("left in the data directory: %s", e.Name())
		}
	}
}

// A profile whose account and TLS settings exist only in the TOML file: the file comes over, and
// with it the settings; the database brings contacts and history.
func TestProfileWithSettingsOnlyInTheFile(t *testing.T) {
	w := newWorld(t)
	write(t, w.paths.SourceConfig, rustTOML)
	w.rustDatabase(nil, 2, 3, "", false)
	source := fingerprint(t, w.rust)

	i := w.importer()
	p, err := i.Preview()
	check(t, err)
	if p != (Preview{ConfigFile: true, Database: true, Account: "1001@pbx.invalid", Contacts: 2, History: 3}) {
		t.Fatalf("preview %+v", p)
	}
	if text := p.String(); !strings.Contains(text, "1001@pbx.invalid") || !strings.Contains(text, "contacts: 2") || !strings.Contains(text, "--apply") {
		t.Errorf("preview text:\n%s", text)
	}
	if after := fingerprint(t, filepath.Dir(filepath.Dir(w.paths.DB))); len(after) != 0 {
		t.Errorf("the preview wrote into go-microsip's directories: %v", after)
	}
	check(t, i.Apply(p, false))

	names, settings, ref, version := w.database()
	if len(names) != 2 || version != store.SchemaVersion || ref != "" {
		t.Errorf("database: contacts %v, schema %d, reference %q", names, version, ref)
	}
	cfg, err := config.Resolve(config.Context{ConfigPath: w.paths.Config, SQLite: config.SQLiteLayer(settings), Files: own{}})
	check(t, err)
	if a := cfg.Config.Account; a.Username != "1001" || a.Transport != domain.TLS || a.Media != domain.SRTP || cfg.Config.CAFile != "/etc/lab-ca.pem" || cfg.Config.UI.Theme != config.ThemeLight {
		t.Errorf("the settings of the file did not come over: %+v", cfg.Config)
	}
	if info, err := os.Stat(w.paths.Config); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("configuration file: %v %v", info, err)
	}
	for _, sidecar := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(w.paths.DB + sidecar); err == nil {
			t.Errorf("the committed database has a %s sidecar", sidecar)
		}
	}
	if after := fingerprint(t, w.rust); !maps.Equal(after, source) {
		t.Errorf("the rust-microsip directory changed: %v, was %v", after, source)
	}
	w.clean()
}

// A profile whose database has content only in its write-ahead log, as after a kill: the log is
// honored, and the source keeps exactly its files.
func TestProfileWithPendingWriteAheadLog(t *testing.T) {
	w := newWorld(t)
	w.rustDatabase(map[string]string{"account.username": "1002", "account.domain": "pbx.invalid", "account.port": "5070"}, 1, 2, "", true)
	source := fingerprint(t, w.rust)
	if len(source) != 2 {
		t.Fatalf("fixture files: %v", source)
	}
	i := w.importer()
	p, err := i.Preview()
	check(t, err)
	if p != (Preview{Database: true, Account: "1002@pbx.invalid", Contacts: 1, History: 2}) {
		t.Fatalf("preview %+v", p)
	}
	check(t, i.Apply(p, false))
	names, settings, _, _ := w.database()
	if len(names) != 1 || settings["account.username"] != "1002" || settings["account.port"] != "5070" {
		t.Errorf("imported: contacts %v, settings %v", names, settings)
	}
	if _, err := os.Stat(w.paths.Config); err == nil {
		t.Error("a configuration file appeared although the source has none")
	}
	if after := fingerprint(t, w.rust); !maps.Equal(after, source) {
		t.Errorf("the rust-microsip directory changed (a new sidecar?): %v, was %v", after, source)
	}
	w.clean()
}

// A database the Rust application itself wrote comes over with its settings.
func TestRustCandidateDatabaseImports(t *testing.T) {
	w := newWorld(t)
	raw, err := os.ReadFile("../store/testdata/rust-0.1.0-rc.3.sqlite3")
	check(t, err)
	check(t, os.WriteFile(w.paths.SourceDB, raw, 0o600))
	i := w.importer()
	p, err := i.Preview()
	check(t, err)
	check(t, i.Apply(p, false))
	if _, settings, _, _ := w.database(); p.Account != "1001@pbx.invalid" || settings["account.transport"] != "tls" || settings["audio.input_device"] != "Lab microphone" {
		t.Errorf("preview %+v, settings %v", p, settings)
	}
}

// The import refuses while either application runs, and keeps the Rust application out while it
// copies.
func TestBothApplicationsAreKeptOut(t *testing.T) {
	w := newWorld(t)
	write(t, w.paths.SourceConfig, rustTOML)
	w.previous()
	before := fingerprint(t, filepath.Dir(w.paths.Config))
	i := w.importer()
	p, err := i.Preview()
	check(t, err)

	rust, err := w.locks.Lock(w.paths.SourceLock) // the Rust application is running
	check(t, err)
	if _, err := i.Preview(); !errors.Is(err, ErrRustRunning) {
		t.Errorf("preview while rust-microsip runs: %v", err)
	}
	if err := i.Apply(p, false); !errors.Is(err, ErrRustRunning) {
		t.Errorf("import while rust-microsip runs: %v", err)
	}
	rust()
	w.previousIntact(before)

	own, err := w.locks.Lock(w.paths.Lock) // another go-microsip is running
	check(t, err)
	if err := i.Apply(p, false); !errors.Is(err, ErrRunning) {
		t.Errorf("import while go-microsip runs: %v", err)
	}
	own()
	w.previousIntact(before)

	// While the source is copied both locks are held: the Rust application cannot start.
	copied := false
	i.snapshotting = func() {
		copied = true
		if !w.locks.Held(w.paths.SourceLock) || !w.locks.Held(w.paths.Lock) {
			t.Error("the source is copied without both instance locks")
		}
		if _, err := w.locks.Lock(w.paths.SourceLock); !errors.Is(err, ports.ErrLocked) {
			t.Errorf("rust-microsip could start during the snapshot: %v", err)
		}
	}
	check(t, i.Apply(p, false))
	if !copied {
		t.Error("the snapshot hook did not run")
	}
	w.clean()
}

// The password is copied only when asked, under a new reference of go-microsip's own keyring; the
// Rust application's entry is only read, and the password is in no file.
func TestCredentialIsCopiedOnlyWhenAsked(t *testing.T) {
	w := newWorld(t)
	w.rustDatabase(map[string]string{"account.username": "1001", "account.domain": "pbx.invalid"}, 0, 0, legacyRef, false)
	check(t, w.legacy.Set(legacyRef, password))
	i := w.importer()
	p, err := i.Preview()
	check(t, err)
	if !p.KeyringEntry || !strings.Contains(p.String(), "--credential") {
		t.Fatalf("preview %+v", p)
	}

	check(t, i.Apply(p, false))
	if _, _, ref, _ := w.database(); ref != "" || len(w.keyring.Refs()) != 0 {
		t.Errorf("a password came over without being asked for: reference %q, keyring %v", ref, w.keyring.Refs())
	}

	check(t, i.Apply(p, true))
	_, _, ref, _ := w.database()
	if got, ok, _ := w.keyring.Get(ref); !strings.HasPrefix(ref, "go-microsip/") || !ok || got != password || len(w.keyring.Refs()) != 1 {
		t.Errorf("reference %q, keyring %v", ref, w.keyring.Refs())
	}
	if got, ok, _ := w.legacy.Get(legacyRef); !ok || got != password || len(w.legacy.Refs()) != 1 {
		t.Error("the rust-microsip keyring was changed")
	}
	for path := range fingerprint(t, filepath.Dir(filepath.Dir(w.paths.DB))) {
		raw, _ := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(w.paths.DB)), path))
		if strings.Contains(string(raw), password) {
			t.Errorf("the password is in %s", path)
		}
	}

	// Without a keyring here the password cannot be copied, and nothing else is either.
	before, _, refBefore, _ := w.database()
	none := w.importer()
	none.Secrets = nil
	if err := none.Apply(p, true); !errors.Is(err, ErrNoCredential) {
		t.Errorf("import with --credential and no keyring: %v", err)
	}
	if after, _, refAfter, _ := w.database(); len(after) != len(before) || refAfter != refBefore {
		t.Error("a refused import changed the database")
	}
	w.clean()
}

// A kill after each step, between each rename, and right after the keyring write: the next start
// puts the previous database and configuration file back together, leaves no staged password, and
// the import can be run again.
func TestKillAtEveryPointIsUndone(t *testing.T) {
	for _, point := range []string{
		"quiesced", "snapshot", "migrated", "credential-journaled", "credential-written", "credential", "sealed",
		"commit-journaled", "db-backed-up", "config-backed-up", "db-in-place", "config-in-place",
	} {
		t.Run(point, func(t *testing.T) {
			w := newWorld(t)
			write(t, w.paths.SourceConfig, rustTOML)
			w.rustDatabase(nil, 2, 1, legacyRef, false)
			check(t, w.legacy.Set(legacyRef, password))
			w.previous()
			before := fingerprint(t, filepath.Dir(w.paths.Config))
			source := fingerprint(t, w.rust)

			killed := w.importer()
			p, err := killed.Preview()
			check(t, err)
			killed.killAt = point
			if err := killed.Apply(p, true); !errors.Is(err, errKilled) {
				t.Fatalf("the import was not killed at %s: %v", point, err)
			}
			// The locks of a killed process are released by the system.
			check(t, w.importer().Recover())
			w.previousIntact(before)
			if after := fingerprint(t, w.rust); !maps.Equal(after, source) {
				t.Errorf("the rust-microsip directory changed: %v", after)
			}

			// The import can be run again, and then it is complete.
			check(t, w.importer().Apply(p, true))
			names, _, ref, _ := w.database()
			if _, ok, _ := w.keyring.Get(ref); len(names) != 2 || !ok || len(w.keyring.Refs()) != 1 {
				t.Errorf("second import: contacts %v, reference %q, keyring %v", names, ref, w.keyring.Refs())
			}
			if raw, _ := os.ReadFile(w.paths.Config); string(raw) != rustTOML {
				t.Error("the configuration file of the second import is not the source's")
			}
			w.clean()
		})
	}
}

// A kill after the last rename is recorded leaves a complete import: the next start only tidies up.
func TestKillAfterTheCommitKeepsTheImport(t *testing.T) {
	w := newWorld(t)
	write(t, w.paths.SourceConfig, rustTOML)
	w.rustDatabase(nil, 2, 1, legacyRef, false)
	check(t, w.legacy.Set(legacyRef, password))
	w.previous()
	killed := w.importer()
	p, err := killed.Preview()
	check(t, err)
	killed.killAt = "done-journaled"
	if err := killed.Apply(p, true); !errors.Is(err, errKilled) {
		t.Fatal(err)
	}
	check(t, w.importer().Recover())
	names, _, ref, _ := w.database()
	if _, ok, _ := w.keyring.Get(ref); len(names) != 2 || !ok {
		t.Errorf("after recovery: contacts %v, reference %q", names, ref)
	}
	if raw, _ := os.ReadFile(w.paths.Config); string(raw) != rustTOML {
		t.Error("the imported configuration file was undone")
	}
	// The previous data is kept as backups beside the new.
	backups := 0
	for _, dir := range []string{filepath.Dir(w.paths.DB), filepath.Dir(w.paths.Config)} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".before-import-") {
				backups++
			}
		}
	}
	if backups != 2 {
		t.Errorf("%d backups of the previous data, want the database and the configuration file", backups)
	}
	w.clean()
}

// A start imports nothing: without a journal recovery touches nothing, even with a Rust profile
// present.
func TestFirstStartImportsNothing(t *testing.T) {
	w := newWorld(t)
	write(t, w.paths.SourceConfig, rustTOML)
	w.rustDatabase(nil, 2, 1, "", false)
	source := fingerprint(t, w.rust)
	check(t, w.importer().Recover())
	if created := fingerprint(t, filepath.Dir(filepath.Dir(w.paths.DB))); len(created) != 0 {
		t.Errorf("a start created %v", created)
	}
	if after := fingerprint(t, w.rust); !maps.Equal(after, source) {
		t.Error("a start touched the rust-microsip directory")
	}
}

func TestSourceThatChangedOrIsMissingIsRefused(t *testing.T) {
	w := newWorld(t)
	if _, err := w.importer().Preview(); !errors.Is(err, ErrNothingToImport) {
		t.Errorf("preview without a source: %v", err)
	}
	w.rustDatabase(nil, 1, 0, "", false)
	w.previous()
	before := fingerprint(t, filepath.Dir(w.paths.Config))
	i := w.importer()
	p, err := i.Preview()
	check(t, err)
	w.rustDatabase(nil, 2, 0, "", false) // the Rust application was used in between
	if err := i.Apply(p, false); !errors.Is(err, ErrChanged) {
		t.Errorf("import of a changed source: %v", err)
	}
	w.previousIntact(before)

	// A source file this application cannot read is refused, with the previous data intact.
	write(t, w.paths.SourceConfig, "schema = 1\n[account]\nbogus = 1\n")
	if _, err := i.Preview(); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("preview of an unreadable configuration file: %v", err)
	}
	check(t, os.WriteFile(w.paths.SourceDB, []byte(strings.Repeat("x", 4096)), 0o600))
	check(t, os.Remove(w.paths.SourceConfig))
	if _, err := i.Preview(); err == nil {
		t.Error("preview of a corrupt database succeeded")
	}
	w.previousIntact(before)
}

// midCommit leaves an import killed in the middle of its commit, with a password staged.
func midCommit(t *testing.T) (*world, map[string]string, Preview) {
	t.Helper()
	w := newWorld(t)
	write(t, w.paths.SourceConfig, rustTOML)
	w.rustDatabase(nil, 2, 1, legacyRef, false)
	check(t, w.legacy.Set(legacyRef, password))
	w.previous()
	before := fingerprint(t, filepath.Dir(w.paths.Config))
	killed := w.importer()
	p, err := killed.Preview()
	check(t, err)
	killed.killAt = "db-in-place"
	if err := killed.Apply(p, true); !errors.Is(err, errKilled) {
		t.Fatal(err)
	}
	return w, before, p
}

// Recovery needs go-microsip's instance lock: it never runs beside an import, another recovery or
// a running softphone, and while it cannot run nothing is touched.
func TestRecoveryNeedsTheInstanceLock(t *testing.T) {
	w, before, _ := midCommit(t)
	state := fingerprint(t, filepath.Dir(filepath.Dir(w.paths.DB)))
	held, err := w.locks.Lock(w.paths.Lock)
	check(t, err)
	i := w.importer()
	if err := i.Recover(); !errors.Is(err, ErrRunning) || !i.Pending() {
		t.Errorf("recovery under another instance's lock: %v, pending %v", err, i.Pending())
	}
	if now := fingerprint(t, filepath.Dir(filepath.Dir(w.paths.DB))); !maps.Equal(now, state) {
		t.Error("a recovery that could not run changed files")
	}
	held()
	check(t, i.Recover())
	if i.Pending() {
		t.Error("the journal is still there after the recovery")
	}
	w.previousIntact(before)
}

// A recovery that is itself killed is finished by the next one.
func TestRecoveryCanBeInterrupted(t *testing.T) {
	for _, point := range []string{"recover-db-restored", "recover-files-restored"} {
		t.Run(point, func(t *testing.T) {
			w, before, p := midCommit(t)
			killed := w.importer()
			killed.killAt = point
			if err := killed.Recover(); !errors.Is(err, errKilled) {
				t.Fatalf("the recovery was not killed at %s: %v", point, err)
			}
			check(t, w.importer().Recover())
			w.previousIntact(before)
			check(t, w.importer().Apply(p, true))
			if names, _, _, _ := w.database(); len(names) != 2 {
				t.Errorf("the import after the recovery: contacts %v", names)
			}
			w.clean()
		})
	}
}

// A staged password is never orphaned: while the keyring cannot be reached the journal keeps its
// reference, the files are already back, and a later recovery removes it.
func TestStagedPasswordIsNeverOrphaned(t *testing.T) {
	for name, broken := range map[string]func(*world, *Importer){
		"no keyring":          func(_ *world, i *Importer) { i.Secrets = nil },
		"unreachable keyring": func(w *world, _ *Importer) { w.keyring.Unavailable = true },
	} {
		t.Run(name, func(t *testing.T) {
			w, before, _ := midCommit(t)
			if len(w.keyring.Refs()) != 1 {
				t.Fatalf("the fixture staged %d passwords", len(w.keyring.Refs()))
			}
			i := w.importer()
			broken(w, i)
			if err := i.Recover(); !errors.Is(err, ErrCredentialCleanup) || !i.Pending() {
				t.Fatalf("recovery without a keyring: %v, pending %v", err, i.Pending())
			}
			// The files are already the previous ones; only the keyring entry is still owed.
			if now := fingerprint(t, filepath.Dir(w.paths.Config)); !maps.Equal(now, before) {
				t.Error("the configuration file was not restored")
			}
			if names, _, _, _ := w.database(); len(names) != 1 || names[0] != "Old" {
				t.Errorf("the database was not restored: %v", names)
			}
			if err := i.Recover(); !errors.Is(err, ErrCredentialCleanup) {
				t.Errorf("a second recovery without a keyring: %v", err)
			}
			w.keyring.Unavailable = false
			check(t, w.importer().Recover())
			w.previousIntact(before)
		})
	}
}

// Undoing an earlier import restores the configuration file that import moved; the import that
// follows goes where the current session points, which may be another file.
func TestRecoveryDoesNotRedirectTheNextImport(t *testing.T) {
	w, before, p := midCommit(t)
	first := w.paths.Config
	elsewhere := w.importer()
	elsewhere.Paths.Config = filepath.Join(filepath.Dir(filepath.Dir(first)), "other", "config.toml")
	check(t, elsewhere.Apply(p, false))
	if now := fingerprint(t, filepath.Dir(first)); !maps.Equal(now, before) {
		t.Errorf("the first configuration file is not the restored one: %v, was %v", now, before)
	}
	if raw, err := os.ReadFile(elsewhere.Paths.Config); err != nil || string(raw) != rustTOML {
		t.Errorf("the import did not write the configuration file of its own session: %v", err)
	}
	if elsewhere.Paths.Config == first || elsewhere.Pending() {
		t.Error("the importer's destination was changed by the recovery, or a journal is left")
	}
	if names, _, _, _ := w.database(); len(names) != 2 {
		t.Errorf("imported contacts %v", names)
	}
}
