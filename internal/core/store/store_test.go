package store

// These tests are the Rust candidate's crates/storage/src/tests.rs, ported one for one, except the
// control ledger and grants, whose API arrives with the control channel. They run without a sound
// device, a display or a session bus.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/domain"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func ptr[T any](v T) *T { return &v }

func call(key string, started int64, direction domain.Direction, remote string, outcome Outcome) CallRecord {
	return CallRecord{SessionKey: key, StartedUTC: started, EndedUTC: ptr(started + 5), Direction: direction, Remote: remote, Outcome: outcome}
}

func contact(name string, favorite bool, numbers ...string) NewContact {
	c := NewContact{Name: name, Favorite: favorite}
	for _, n := range numbers {
		c.Numbers = append(c.Numbers, ContactNumber{Number: n})
	}
	return c
}

// must fails the test on an error and returns the value.
func must[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func openAt(t *testing.T, path string) *Store {
	t.Helper()
	s := must(Open(path))(t)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func memory(t *testing.T) *Store {
	t.Helper()
	s := must(OpenInMemory())(t)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	check(t, s.db.QueryRow("SELECT count(*) FROM "+table).Scan(&n))
	return n
}

// kind is the failure kind of err, or -1 when it is not a storage error.
func kind(err error) (ErrorKind, InvalidField) {
	var e *Error
	if !errors.As(err, &e) {
		return ErrorKind(255), ""
	}
	return e.Kind, e.Field
}

func is(err error, want ErrorKind) bool { k, _ := kind(err); return k == want }

func isInvalid(err error, field InvalidField) bool {
	k, f := kind(err)
	return k == Invalid && f == field
}

// raw opens the file with a plain connection, as another program would.
func raw(t *testing.T, path string) *sql.Conn {
	t.Helper()
	db := must(sql.Open("sqlite", path))(t)
	conn := must(db.Conn(context.Background()))(t)
	t.Cleanup(func() { _ = conn.Close(); _ = db.Close() })
	return conn
}

func exec(t *testing.T, c *sql.Conn, query string) {
	t.Helper()
	_, err := c.ExecContext(context.Background(), query)
	check(t, err)
}

func scalar(t *testing.T, c *sql.Conn, query string) (n int64) {
	t.Helper()
	check(t, c.QueryRowContext(context.Background(), query).Scan(&n))
	return n
}

func tableExists(t *testing.T, c *sql.Conn, name string) bool {
	return scalar(t, c, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = '"+name+"'") == 1
}

func changes(pairs ...string) map[string]*string {
	out := map[string]*string{}
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i+1] == "<nil>" {
			out[pairs[i]] = nil
		} else {
			out[pairs[i]] = &pairs[i+1]
		}
	}
	return out
}

func TestMigrationsAreIdempotentAndFutureSchemaIsRefusedUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data/go-microsip.sqlite3")
	s := must(Open(path))(t)
	if v := must(s.SchemaVersion())(t); v != SchemaVersion {
		t.Fatalf("schema %d", v)
	}
	if !must(s.RecordCall(call("s:1", 1_000, domain.Outgoing, "1002", Answered)))(t) {
		t.Fatal("the call was not recorded")
	}
	check(t, s.Close())

	s = must(Open(path))(t)
	if v, calls := must(s.SchemaVersion())(t), must(s.RecentCalls(10, AllCalls, ""))(t); v != SchemaVersion || len(calls) != 1 {
		t.Fatalf("reopened: schema %d, %d calls", v, len(calls))
	}
	check(t, s.Close())

	c := raw(t, path)
	exec(t, c, "PRAGMA user_version = 99")
	check(t, c.Close())
	before := must(os.ReadFile(path))(t)
	_, err := Open(path)
	var future *Error
	if !errors.As(err, &future) || future.Kind != FutureSchema || future.Found != 99 || future.Supported != SchemaVersion {
		t.Fatalf("got %v, want a future schema", err)
	}
	if after := must(os.ReadFile(path))(t); !bytes.Equal(before, after) {
		t.Error("a database from a newer release was modified")
	}
	if v := scalar(t, raw(t, path), "PRAGMA user_version"); v != 99 {
		t.Errorf("user_version %d after the refusal", v)
	}
}

func TestCorruptFileIsReportedAndPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.sqlite3")
	garbage := bytes.Repeat([]byte{'x'}, 4096)
	check(t, os.WriteFile(path, garbage, 0o600))
	if _, err := Open(path); !is(err, Corrupt) {
		t.Fatalf("got %v, want corrupt", err)
	}
	if after := must(os.ReadFile(path))(t); !bytes.Equal(after, garbage) {
		t.Error("the corrupt file was modified")
	}
}

func TestFilesAndDirectoriesArePrivate(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "a/b")
	path := filepath.Join(dir, "db.sqlite3")
	s := openAt(t, path)
	must(s.RecordCall(call("s:1", 1, domain.Incoming, "1001", Missed)))(t)
	if mode(t, filepath.Join(tmp, "a")) != 0o700 || mode(t, dir) != 0o700 || mode(t, path) != 0o600 {
		t.Errorf("modes: %o %o %o", mode(t, filepath.Join(tmp, "a")), mode(t, dir), mode(t, path))
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if m := mode(t, path+suffix); m != 0o600 {
			t.Errorf("%s has mode %o", suffix, m)
		}
	}

	loose := filepath.Join(tmp, "loose.sqlite3")
	check(t, os.WriteFile(loose, nil, 0o644))
	check(t, os.Chmod(loose, 0o644))
	check(t, must(Open(loose))(t).Close())
	if m := mode(t, loose); m != 0o600 {
		t.Errorf("an existing file kept mode %o", m)
	}
	link := filepath.Join(tmp, "link.sqlite3")
	check(t, os.Symlink(loose, link))
	if _, err := Open(link); !is(err, NotRegularFile) {
		t.Errorf("a symlink: %v", err)
	}
}

// A database in a directory that group or others can write is refused: they could swap the file.
func TestDirectoryOthersCanWriteIsRefused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	check(t, os.Mkdir(dir, 0o700))
	for _, loose := range []os.FileMode{0o770, 0o707, 0o777} {
		check(t, os.Chmod(dir, loose))
		if _, err := Open(filepath.Join(dir, "db.sqlite3")); !is(err, InsecureDirectory) || !strings.Contains(err.Error(), "chmod go-w") {
			t.Errorf("mode %o: %v", loose, err)
		}
	}
	if entries := must(os.ReadDir(dir))(t); len(entries) != 0 {
		t.Errorf("a refused open created %d files", len(entries))
	}
	check(t, os.Chmod(dir, 0o755)) // readable by others is not the problem
	check(t, must(Open(filepath.Join(dir, "db.sqlite3")))(t).Close())
}

func TestOutcomeMappingFollowsEstablishmentAndDirection(t *testing.T) {
	in, out := domain.Incoming, domain.Outgoing
	for _, c := range []struct {
		direction   domain.Direction
		established bool
		reason      domain.EndReason
		want        Outcome
	}{
		{in, true, domain.EndNormal, Answered},
		{out, true, domain.EndFailed, Answered},
		{in, false, domain.EndNormal, Missed},
		{in, false, domain.EndCanceled, Missed},
		{in, false, domain.EndNoAnswer, Missed},
		{in, false, domain.EndRejected, Rejected},
		{out, false, domain.EndCanceled, Canceled},
		{out, false, domain.EndNormal, Canceled},
		{out, false, domain.EndTransferred, Canceled},
		{out, false, domain.EndBusy, BusyPeer},
		{out, false, domain.EndNoAnswer, NoAnswer},
		{out, false, domain.EndNotAcceptable, Failed},
		{out, false, domain.EndFailed, Failed},
		{out, false, domain.EndRejected, Rejected},
	} {
		if got := OutcomeFromEnd(c.direction, c.established, c.reason); got != c.want {
			t.Errorf("%v established=%v %v: %q, want %q", c.direction, c.established, c.reason, got, c.want)
		}
	}
}

func TestHistoryRoundTripDedupFiltersSearchAndRetention(t *testing.T) {
	s := memory(t)
	now := nowUTC()
	answered := call("s:1", now-60, domain.Outgoing, "1002", Answered)
	answered.AnsweredUTC, answered.DurationMS, answered.Secure = ptr(now-58), ptr(int64(3_500)), true
	if !must(s.RecordCall(answered))(t) || must(s.RecordCall(answered))(t) {
		t.Fatal("a duplicate session key must be ignored")
	}
	must(s.RecordCall(call("s:2", now-30, domain.Incoming, "100%_x", Missed)))(t)
	must(s.RecordCall(call("s:3", now-100*86_400, domain.Incoming, "1003", Interrupted)))(t)

	list := func(limit int, filter HistoryFilter, search string) []HistoryEntry {
		t.Helper()
		return must(s.RecentCalls(limit, filter, search))(t)
	}
	all := list(10, AllCalls, "")
	var keys []string
	for _, e := range all {
		keys = append(keys, e.Call.SessionKey)
	}
	if !slices.Equal(keys, []string{"s:2", "s:1", "s:3"}) || !reflect.DeepEqual(all[1].Call, answered) {
		t.Fatalf("history %v; second entry %+v", keys, all[1].Call)
	}
	missed := list(10, MissedCalls, "")
	if len(list(1, AllCalls, "")) != 1 || len(list(10, OutgoingCalls, "")) != 1 || len(list(10, IncomingCalls, "")) != 2 ||
		len(missed) != 1 || missed[0].Call.Remote != "100%_x" {
		t.Error("filters")
	}
	// Wildcards in a search are literal.
	if len(list(10, AllCalls, "%")) != 1 || len(list(10, AllCalls, "0_")) != 0 || len(list(10, AllCalls, "100")) != 3 {
		t.Error("search")
	}

	if n := must(s.PruneOlderThan(DefaultRetentionDays))(t); n != 1 {
		t.Errorf("pruned %d", n)
	}
	check(t, s.DeleteCall(all[0].ID))
	if err := s.DeleteCall(all[0].ID); !is(err, NotFound) {
		t.Errorf("deleting twice: %v", err)
	}
	if n := must(s.ClearHistory())(t); n != 1 || len(list(10, AllCalls, "")) != 0 {
		t.Errorf("cleared %d", n)
	}

	bad := call("s:4", now, domain.Incoming, "evil\r\nX", Missed)
	if _, err := s.RecordCall(bad); !isInvalid(err, Remote) || strings.Contains(err.Error(), "evil") {
		t.Errorf("control characters in the remote party: %v", err)
	}
	bad.Remote, bad.SessionKey = "ok", ""
	if _, err := s.RecordCall(bad); !isInvalid(err, SessionKey) {
		t.Errorf("empty session key: %v", err)
	}
	bad.SessionKey, bad.DurationMS = "s:4", ptr(int64(-1))
	if _, err := s.RecordCall(bad); !isInvalid(err, Duration) {
		t.Errorf("negative duration: %v", err)
	}
}

func TestContactsCRUDFavoritesAndLiteralSearch(t *testing.T) {
	s := memory(t)
	alice := contact(" Alice 100% ", true, "1001", "+5511999990000")
	alice.Numbers[0].Label = "work"
	a := must(s.AddContact(alice))(t)
	b := must(s.AddContact(contact("bob_lab", false, "2002")))(t)
	c := must(s.AddContact(contact("Carol", false, "3003")))(t)

	got, ok, err := s.Contact(a)
	if err != nil || !ok || got.Name != "Alice 100%" || !got.Favorite || len(got.Numbers) != 2 || got.Numbers[0].Label != "work" || got.Numbers[1].Label != "" {
		t.Fatalf("contact %+v, %v, %v", got, ok, err)
	}
	names := func(query string, limit int) []string {
		t.Helper()
		out := []string{}
		for _, c := range must(s.SearchContacts(query, limit))(t) {
			out = append(out, c.Name)
		}
		return out
	}
	for query, want := range map[string][]string{
		"": {"Alice 100%", "bob_lab", "Carol"}, "%": {"Alice 100%"}, "b_l": {"bob_lab"}, "o_": {},
		"CAROL": {"Carol"}, "99999": {"Alice 100%"},
	} {
		if got := names(query, 10); !slices.Equal(got, want) {
			t.Errorf("search %q: %v, want %v", query, got, want)
		}
	}
	if len(names("", 1)) != 1 {
		t.Error("the limit is ignored")
	}

	check(t, s.SetFavorite(c, true))
	check(t, s.SetFavorite(a, false))
	if first := names("", 10)[0]; first != "Carol" {
		t.Errorf("favorites first: %q", first)
	}
	check(t, s.UpdateContact(b, contact("Bob", true, "2003", "2004")))
	if bob, _, _ := s.Contact(b); bob.Name != "Bob" || !bob.Favorite || len(bob.Numbers) != 2 {
		t.Errorf("updated %+v", bob)
	}
	if err := s.UpdateContact(999, contact("X", false, "1")); !is(err, NotFound) {
		t.Errorf("updating a missing contact: %v", err)
	}

	check(t, s.DeleteContact(b))
	if _, ok, _ := s.Contact(b); ok || count(t, s, "contact_numbers") != 3 {
		t.Errorf("the numbers did not go with the contact: %d left", count(t, s, "contact_numbers"))
	}
	if err := s.DeleteContact(b); !is(err, NotFound) {
		t.Errorf("deleting twice: %v", err)
	}
	if err := s.SetFavorite(b, true); !is(err, NotFound) {
		t.Errorf("favorite on a missing contact: %v", err)
	}

	for field, bad := range map[InvalidField]NewContact{
		Number:          contact("X", false, "sip:1@x"),
		DuplicateNumber: contact("X", false, "1", " 1 "),
		Name:            contact("  ", false, "1"),
		NumberCount:     contact("X", false),
		Label:           {Name: "X", Numbers: []ContactNumber{{Number: "1", Label: "a\tb"}}},
	} {
		if _, err := s.AddContact(bad); !isInvalid(err, field) {
			t.Errorf("%s: %v", field, err)
		}
	}
	if n := count(t, s, "contacts"); n != 2 {
		t.Errorf("%d contacts after the refused ones", n)
	}
}

func TestCSVPreviewHandlesQuotesCRLFBOMDuplicatesAndIssues(t *testing.T) {
	text := "\ufeffName,Number,Label\r\n" +
		"\"Doe, Jane\",1001,\"say \"\"hi\"\"\"\r\n" +
		"\"Doe, Jane\",1002,\r\n" +
		"\"Doe, Jane\",1001,dup\r\n" +
		"\r\n" +
		"Bad,sip:1@x,\n" +
		"\"multi\nline\",1003,\n" +
		"Short,1004\n" +
		"Stray\"quote,1005,\n" +
		"Ok,+5511,\n" +
		"\"open,1006,\n"
	p := must(PreviewCSV(text))(t)
	if p.RecordCount() != 9 || p.DuplicateCount() != 1 || len(p.Contacts()) != 2 {
		t.Fatalf("%d records, %d duplicates, %d contacts", p.RecordCount(), p.DuplicateCount(), len(p.Contacts()))
	}
	jane := p.Contacts()[0]
	if jane.Name != "Doe, Jane" || len(jane.Numbers) != 2 || jane.Numbers[0].Label != `say "hi"` || jane.Numbers[1].Label != "" {
		t.Errorf("first contact %+v", jane)
	}
	want := []CSVIssue{{6, InvalidNumber}, {7, InvalidName}, {9, ColumnCount}, {10, Malformed}, {12, Malformed}}
	if !slices.Equal(p.Issues(), want) || p.Importable() {
		t.Errorf("issues %v, want %v", p.Issues(), want)
	}
	s := memory(t)
	if _, err := s.ImportCSV(p); !is(err, CSVInvalid) || count(t, s, "contacts") != 0 {
		t.Errorf("importing a preview with issues: %v", err)
	}
}

func TestCSVLimitsAndHeaderAreEnforced(t *testing.T) {
	for _, bad := range []string{"", "number,name\n1,a\n", "\"name,number\"\na\n", "name,number,label,extra\n", "name\n"} {
		if _, err := PreviewCSV(bad); !is(err, CSVHeader) {
			t.Errorf("header %q: %v", bad, err)
		}
	}
	if _, err := PreviewCSV("name,number\n" + strings.Repeat("x", CSVMaxBytes)); !is(err, CSVTooLarge) {
		t.Errorf("oversized: %v", err)
	}
	if _, err := PreviewCSV("name,number\n" + strings.Repeat("a,1\n", CSVMaxRecords+1)); !is(err, CSVTooManyRecords) {
		t.Errorf("too many records: %v", err)
	}
	if p, err := PreviewCSV("name,number\n" + strings.Repeat("a,1\n", CSVMaxRecords)); err != nil || p.DuplicateCount() != CSVMaxRecords-1 {
		t.Errorf("the record limit itself: %v", err)
	}
}

func TestCSVImportMergesByNameAndIsAtomic(t *testing.T) {
	s := memory(t)
	must(s.AddContact(contact("Jane", true, "1001")))(t)
	summary := must(s.ImportCSV(must(PreviewCSV("name,number\nJane,1001\nJane,1002\nZed,2001\nZed,2002\n"))(t)))(t)
	if summary != (ImportSummary{ContactsCreated: 1, ContactsMerged: 1, NumbersAdded: 3}) {
		t.Errorf("summary %+v", summary)
	}
	if jane := must(s.SearchContacts("Jane", 10))(t); len(jane) != 1 || len(jane[0].Numbers) != 2 || !jane[0].Favorite {
		t.Errorf("merged contact %+v", jane)
	}

	// Force a failure on the last insert: nothing of this import may remain.
	_, err := s.db.Exec(`CREATE TEMP TRIGGER fail BEFORE INSERT ON contact_numbers
		WHEN NEW.number = '9999' BEGIN SELECT RAISE(ABORT, 'forced'); END;`)
	check(t, err)
	contacts, numbers := count(t, s, "contacts"), count(t, s, "contact_numbers")
	if _, err := s.ImportCSV(must(PreviewCSV("name,number\nNew One,5001\nJane,1003\nLast,9999\n"))(t)); err == nil {
		t.Fatal("the forced failure did not fail the import")
	}
	if count(t, s, "contacts") != contacts || count(t, s, "contact_numbers") != numbers || len(must(s.SearchContacts("New One", 10))(t)) != 0 {
		t.Error("a failed import left rows behind")
	}
}

func TestBackupIsPrivateReadableAndNeverOverwrites(t *testing.T) {
	tmp := t.TempDir()
	s := openAt(t, filepath.Join(tmp, "db.sqlite3"))
	must(s.AddContact(contact("Backup Test", false, "1001")))(t)
	target := filepath.Join(tmp, "backup.sqlite3")
	check(t, s.BackupTo(target))
	if m := mode(t, target); m != 0o600 {
		t.Errorf("backup mode %o", m)
	}
	if err := s.BackupTo(target); !is(err, BackupExists) {
		t.Errorf("a second backup to the same file: %v", err)
	}
	copied := openAt(t, target)
	if v, found := must(copied.SchemaVersion())(t), must(copied.SearchContacts("Backup", 10))(t); v != SchemaVersion || len(found) != 1 {
		t.Errorf("the backup: schema %d, %d contacts", v, len(found))
	}
}

func TestCSVRowsWithExtraFieldsAreRejectedWithoutGrowth(t *testing.T) {
	p := must(PreviewCSV("name,number\nAlice,1001" + strings.Repeat(",", 100_000) + "\n"))(t)
	if p.Importable() || len(p.Issues()) != 1 {
		t.Errorf("issues %v", p.Issues())
	}
}

func TestCSVMergeRespectsThePerContactNumberBound(t *testing.T) {
	s := memory(t)
	var numbers []string
	for i := range MaxNumbersPerContact {
		numbers = append(numbers, fmt.Sprintf("1%03d", i))
	}
	must(s.AddContact(contact("Alice", false, numbers...)))(t)
	p := must(PreviewCSV("name,number\nAlice,2001\n"))(t)
	before := count(t, s, "contact_numbers")
	if _, err := s.ImportCSV(p); !p.Importable() || !isInvalid(err, NumberCount) || count(t, s, "contact_numbers") != before {
		t.Errorf("merge beyond the bound: %v", err)
	}
}

// v1Database writes a schema-1 database with one call, the way release 1 did.
func v1Database(t *testing.T, path string) {
	t.Helper()
	c := raw(t, path)
	exec(t, c, migrations[0])
	exec(t, c, "PRAGMA user_version = 1")
	exec(t, c, `INSERT INTO calls (session_key, started_utc, direction, remote, outcome, secure) VALUES ('v1:1', 1000, 'incoming', '1001', 'missed', 0)`)
	check(t, c.Close())
}

func TestV1UpgradesWithPrivateBackupAndKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	v1Database(t, path)
	s := openAt(t, path)
	if v, calls, cfg := must(s.SchemaVersion())(t), must(s.RecentCalls(10, AllCalls, ""))(t), must(s.LoadConfig())(t); v != SchemaVersion || len(calls) != 1 || len(cfg) != 0 {
		t.Fatalf("schema %d, %d calls, settings %v", v, len(calls), cfg)
	}
	backup := path + ".v1.bak"
	if m := mode(t, backup); m != 0o600 {
		t.Errorf("backup mode %o", m)
	}
	if c := raw(t, backup); scalar(t, c, "PRAGMA user_version") != 1 || tableExists(t, c, "accounts") || scalar(t, c, "SELECT count(*) FROM calls") != 1 {
		t.Error("the backup is not the version-1 database")
	}
}

func TestInterruptedMigrationLeavesV1IntactAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	v1Database(t, path)
	failAfterMigration = 2
	_, err := Open(path)
	failAfterMigration = 0
	if !is(err, IO) {
		t.Fatalf("got %v, want the injected failure", err)
	}
	c := raw(t, path)
	if scalar(t, c, "PRAGMA user_version") != 1 || tableExists(t, c, "accounts") || tableExists(t, c, "preferences") || scalar(t, c, "SELECT count(*) FROM calls") != 1 {
		t.Error("a partially applied schema was left behind")
	}
	check(t, c.Close())
	if mode(t, path+".v1.bak") != 0o600 {
		t.Error("no backup before the failed upgrade")
	}
	// The next start completes the upgrade and keeps the first backup.
	s := openAt(t, path)
	if v, calls := must(s.SchemaVersion())(t), must(s.RecentCalls(10, AllCalls, ""))(t); v != SchemaVersion || len(calls) != 1 {
		t.Errorf("after the retry: schema %d, %d calls", v, len(calls))
	}
	if mode(t, path+".v1.bak") != 0o600 || mode(t, path+".v1.1.bak") != 0o600 {
		t.Error("the backups of both attempts must exist")
	}
}

func TestLockedDatabaseReportsBusyWithoutDataLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	s := openAt(t, path)
	check(t, s.SaveConfig(changes("account.server", "pbx.invalid")))
	other := raw(t, path)
	exec(t, other, "BEGIN EXCLUSIVE")
	if err := s.SaveConfig(changes("account.server", "other.invalid")); !is(err, Busy) {
		t.Errorf("a write under another process's lock: %v", err)
	}
	if _, err := Open(path); !is(err, Busy) {
		t.Errorf("opening under another process's lock: %v", err)
	}
	exec(t, other, "ROLLBACK")
	if got := must(s.LoadConfig())(t)["account.server"]; got != "pbx.invalid" {
		t.Errorf("server %q after the busy write", got)
	}
}

func TestFullDiskIsReportedAndRolledBack(t *testing.T) {
	s := openAt(t, filepath.Join(t.TempDir(), "db.sqlite3"))
	must(s.AddContact(contact("Before Full", false, "1001")))(t)
	// Cap the file at its current size: the next write that grows it hits SQLITE_FULL.
	var pages int
	check(t, s.db.QueryRow("PRAGMA page_count").Scan(&pages))
	_, err := s.db.Exec(fmt.Sprintf("PRAGMA max_page_count = %d", pages))
	check(t, err)
	long := strings.Repeat("x", MaxConfigValueLen)
	for i := 0; i < 64 && err == nil; i++ {
		err = s.SaveConfig(changes("audio.input_device", (fmt.Sprint(i) + long)[:MaxConfigValueLen],
			"account.display_name", long, "account.domain", long, "account.server", long))
	}
	if !is(err, Full) {
		t.Fatalf("got %v, want a full disk", err)
	}
	if found := must(s.SearchContacts("Before", 10))(t); len(found) != 1 {
		t.Error("the data written before the disk filled is gone")
	}
}

func TestSettingsRoundTripAcrossRestartWithoutSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	s := must(Open(path))(t)
	check(t, s.SaveConfig(changes("account.username", "1001", "account.domain", "pbx.invalid", "account.port", "5061",
		"account.transport", "tls", "ui.theme", "dark", "ui.dnd", "true")))
	check(t, s.SaveConfig(changes("ui.dnd", "<nil>", "account.port", "<nil>")))
	check(t, s.SetSecretRef("keyring:go-microsip/1001"))
	check(t, s.Close())

	s = openAt(t, path)
	want := map[string]string{"account.username": "1001", "account.domain": "pbx.invalid", "account.transport": "tls", "ui.theme": "dark"}
	if got := must(s.LoadConfig())(t); !maps.Equal(got, want) {
		t.Errorf("settings %v, want %v", got, want)
	}
	if ref := must(s.SecretRef())(t); ref != "keyring:go-microsip/1001" {
		t.Errorf("secret reference %q", ref)
	}
	check(t, s.SetSecretRef(""))
	if ref := must(s.SecretRef())(t); ref != "" {
		t.Errorf("secret reference %q after clearing", ref)
	}
	// No column could hold a password.
	rows := must(s.db.Query("SELECT name FROM pragma_table_info('accounts')"))(t)
	defer rows.Close()
	for rows.Next() {
		var column string
		check(t, rows.Scan(&column))
		if column == "password" {
			t.Error("the accounts table has a password column")
		}
	}
}

func TestSettingsValidationIsAllOrNothing(t *testing.T) {
	s := memory(t)
	check(t, s.SaveConfig(changes("ui.theme", "light")))
	for field, bad := range map[InvalidField]map[string]*string{
		ConfigKey:   changes("ui.theme", "dark", "account.password", "x"),
		ConfigValue: changes("ui.theme", "dark", "account.port", "abc"),
	} {
		if err := s.SaveConfig(bad); !isInvalid(err, field) || strings.Contains(err.Error(), "abc") {
			t.Errorf("%s: %v", field, err)
		}
	}
	if err := s.SaveConfig(changes("ui.theme", "dark", "account.server", "a\nb")); !isInvalid(err, ConfigValue) {
		t.Errorf("control character: %v", err)
	}
	// A CHECK constraint failure on one row rolls the others back.
	if err := s.SaveConfig(changes("account.port", "70000", "ui.theme", "dark", "account.display_name", "Lab")); err == nil {
		t.Error("a port outside the CHECK constraint was stored")
	}
	if got := must(s.LoadConfig())(t); !maps.Equal(got, map[string]string{"ui.theme": "light"}) {
		t.Errorf("settings %v after the refused writes", got)
	}
	for _, bad := range []string{"a\nb", strings.Repeat("x", maxSecretRefLen+1)} {
		if err := s.SetSecretRef(bad); !isInvalid(err, SecretRef) {
			t.Errorf("secret reference: %v", err)
		}
	}
}

func TestOnlyOneAccountCanBeEnabled(t *testing.T) {
	s := memory(t)
	check(t, s.SaveConfig(changes("account.username", "1001")))
	if _, err := s.db.Exec("INSERT INTO accounts (enabled, username, updated_utc) VALUES (1, '1002', 0)"); err == nil {
		t.Error("a second enabled account was accepted")
	}
	_, err := s.db.Exec("INSERT INTO accounts (enabled, username, updated_utc) VALUES (0, '1003', 0)")
	check(t, err)
	if got := must(s.LoadConfig())(t)["account.username"]; got != "1001" {
		t.Errorf("username %q", got)
	}
}

func TestReplaceConfigIsAtomicAndClearsPreviousValues(t *testing.T) {
	s := memory(t)
	check(t, s.SaveConfig(changes("account.username", "1001", "account.server", "old.invalid", "ui.dnd", "true")))
	check(t, s.SetSecretRef("keyring:old"))
	before := must(s.LoadConfig())(t)
	values := map[string]string{"account.username": "2001", "account.domain": "new.invalid", "account.port": "0"} // violates CHECK
	if err := s.ReplaceConfig(values); err == nil {
		t.Fatal("a port of 0 was stored")
	}
	if got, ref := must(s.LoadConfig())(t), must(s.SecretRef())(t); !maps.Equal(got, before) || ref != "keyring:old" {
		t.Errorf("after the failed replace: %v, %q", got, ref)
	}
	delete(values, "account.port")
	check(t, s.ReplaceConfig(values))
	if got, ref := must(s.LoadConfig())(t), must(s.SecretRef())(t); !maps.Equal(got, values) || ref != "" {
		t.Errorf("after the replace: %v, %q", got, ref)
	}
}

func TestKilledMidCallIsRecoveredOnceAsInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	start := func(key string) CallStart {
		return CallStart{SessionKey: key, StartedUTC: 1_000, Direction: domain.Outgoing, Remote: "1002", Secure: true}
	}
	s := must(Open(path))(t)
	if !must(s.BeginCall(start("p1:1")))(t) || must(s.BeginCall(start("p1:1")))(t) {
		t.Fatal("a call is journaled once per session key")
	}
	// Open rows are not listed while the call runs.
	if listed := must(s.RecentCalls(10, AllCalls, ""))(t); len(listed) != 0 {
		t.Errorf("%d open calls listed", len(listed))
	}
	must(s.BeginCall(start("p1:2")))(t)
	done := call("p1:2", 1_000, domain.Outgoing, "1002", Answered)
	done.AnsweredUTC, done.DurationMS = ptr(int64(1_001)), ptr(int64(4_000))
	if !must(s.FinishCall(done))(t) || must(s.FinishCall(done))(t) {
		t.Fatal("finishing twice must keep one entry")
	}
	check(t, s.Close()) // without finishing p1:1: the process was killed

	s = openAt(t, path)
	if first, second := must(s.RecoverInterrupted())(t), must(s.RecoverInterrupted())(t); first != 1 || second != 0 {
		t.Fatalf("recovered %d, then %d", first, second)
	}
	list := must(s.RecentCalls(10, AllCalls, ""))(t)
	byKey := map[string]CallRecord{}
	for _, e := range list {
		byKey[e.Call.SessionKey] = e.Call
	}
	lost := byKey["p1:1"]
	if len(list) != 2 || lost.Outcome != Interrupted || lost.EndedUTC != nil || lost.DurationMS != nil || !lost.Secure {
		t.Errorf("recovered call %+v of %d", lost, len(list))
	}
	if d := byKey["p1:2"].DurationMS; d == nil || *d != 4_000 {
		t.Errorf("finished call %+v", byKey["p1:2"])
	}
	// A late final record for a recovered key does not duplicate it.
	if must(s.FinishCall(call("p1:1", 1_000, domain.Outgoing, "1002", Answered)))(t) {
		t.Error("a recovered call was finished again")
	}
	// Finishing a call that was never journaled still records it once.
	if !must(s.FinishCall(call("p2:1", 2_000, domain.Incoming, "1003", Missed)))(t) || count(t, s, "calls") != 3 {
		t.Errorf("%d calls", count(t, s, "calls"))
	}
}

// A database the Rust candidate itself wrote (testdata/README.md) opens as it is: same schema
// version, no migration and so no backup, and its settings resolve to a valid account here.
func TestRustCandidateDatabaseOpens(t *testing.T) {
	fixture := must(os.ReadFile("testdata/rust-0.1.0-rc.3.sqlite3"))(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "go-microsip.sqlite3")
	check(t, os.WriteFile(path, fixture, 0o644))
	s := openAt(t, path)
	if v := must(s.SchemaVersion())(t); v != SchemaVersion {
		t.Fatalf("schema %d", v)
	}
	stored := must(s.LoadConfig())(t)
	want := map[string]string{
		"account.username": "1001", "account.domain": "pbx.invalid", "account.server": "pbx.invalid", "account.port": "5061",
		"account.transport": "tls", "account.media": "srtp", "ui.theme": "light", "ui.dnd": "true", "audio.input_device": "Lab microphone",
	}
	if !maps.Equal(stored, want) {
		t.Errorf("settings %v, want %v", stored, want)
	}
	if entries := must(os.ReadDir(dir))(t); slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.HasSuffix(e.Name(), ".bak") }) {
		t.Error("a backup was written although nothing was migrated")
	}
	if m := mode(t, path); m != 0o600 {
		t.Errorf("the imported file keeps mode %o", m)
	}
	cfg, err := config.Resolve(config.Context{SQLite: config.SQLiteLayer(stored), Files: nobody{}})
	if err != nil {
		t.Fatalf("the stored settings do not resolve: %v", err)
	}
	a := cfg.Config.Account
	if a.Username != "1001" || a.Port != 5061 || a.Transport != domain.TLS || a.Media != domain.SRTP || cfg.Config.UI.Theme != config.ThemeLight ||
		!cfg.Config.UI.DND || cfg.Config.Audio.InputDevice != "Lab microphone" || cfg.Source(config.Username) != config.SQLite {
		t.Errorf("resolved %+v", cfg.Config)
	}
	// It stays usable: history, contacts and the journal work on it.
	must(s.AddContact(contact("Lab", false, "1002")))(t)
	must(s.BeginCall(CallStart{SessionKey: "g:1", StartedUTC: 1, Remote: "1002"}))(t)
	if n := must(s.RecoverInterrupted())(t); n != 1 {
		t.Errorf("recovered %d", n)
	}
}

// nobody is a ports.FileAccess for a resolution that reads no file.
type nobody struct{}

func (nobody) CurrentUser() uint32                      { return 0 }
func (nobody) Inspect(os.FileInfo) (owner, mode uint32) { return 0, 0o600 }

func TestConfigKeysMirrorTheConfigurationSchema(t *testing.T) {
	var keys []string
	for _, f := range config.Fields() {
		keys = append(keys, f.Key())
	}
	if !slices.Equal(keys, ConfigKeys) {
		t.Errorf("store keys %v, configuration keys %v", ConfigKeys, keys)
	}
}

// The database's address for SQLite: a Windows path keeps its drive in the path (a drive read as
// a host makes the database impossible to open), and characters that mean something in an address
// are escaped.
func TestTheDatabaseAddressKeepsADriveInThePath(t *testing.T) {
	for path, want := range map[string]string{
		"/home/u/.local/share/go-microsip/go-microsip.sqlite3":          "file:///home/u/.local/share/go-microsip/go-microsip.sqlite3",
		"C:/Users/lab/AppData/Local/go-microsip/go-microsip.sqlite3":    "file:///C:/Users/lab/AppData/Local/go-microsip/go-microsip.sqlite3",
		"C:/Users/a b/AppData/Local/go-microsip/go-microsip#1?.sqlite3": "file:///C:/Users/a%20b/AppData/Local/go-microsip/go-microsip%231%3F.sqlite3",
	} {
		if got := slashURI(path); got != want {
			t.Errorf("%s: %s, want %s", path, got, want)
		}
	}
	// A relative path is the file in the working directory, not a host.
	if got := fileURI("relative.sqlite3"); !strings.HasPrefix(got, "file:///") || !strings.HasSuffix(got, "/relative.sqlite3") {
		t.Errorf("a relative path: %s", got)
	}
}
