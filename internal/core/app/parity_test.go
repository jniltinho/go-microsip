package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// Clauses of the parity matrix that had no test of their own (docs/migration/parity-matrix.md).

// answered waits for the call to be established.
func answered(t *testing.T, p *Phone) {
	t.Helper()
	until(t, p, "the answer", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.Kind == domain.Established
	})
}

func dial(t *testing.T, p *Phone) {
	t.Helper()
	target, _ := domain.ParseDialTarget("1002")
	if err := p.Send(domain.Dial{Target: target}); err != nil {
		t.Fatal(err)
	}
}

// A transfer the PBX accepts with 202 and never reports on is not a success (A126): it stays
// pending, the call stays up, and nothing says it succeeded.
func TestATransferIsNotASuccessOn202Alone(t *testing.T) {
	o, _ := options(t, lab(t, 200))
	p, err := OpenPhone(o, PhoneOptions{Audio: &memory.Audio{Tone: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	dial(t, p)
	answered(t, p)
	target, _ := domain.ParseDialTarget("1003")
	if err := p.Send(domain.Transfer{Call: 1, Target: target}); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the transfer pending", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Transfer.Kind == domain.TransferPending
	})
	// Two seconds after the 202: still pending, still up.
	quiet := time.After(2 * time.Second)
	for {
		select {
		case ev := <-p.Events():
			if c, ok := ev.(domain.CallChanged); ok && (c.Call.Transfer.Kind != domain.TransferPending || c.Call.Phase.Kind != domain.Established) {
				t.Fatalf("after a 202 with no report the call is %s with transfer %+v", c.Call.Phase, c.Call.Transfer)
			}
		case <-quiet:
			return
		}
	}
}

// A .env file in the working directory is not read on its own (A63); the same file named with
// --env-file is, which shows that it would have configured an account.
func TestADotEnvInTheWorkingDirectoryIsNotRead(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	text := "GO_MICROSIP_ACCOUNT_USERNAME=1001\nGO_MICROSIP_ACCOUNT_DOMAIN=pbx.invalid\nGO_MICROSIP_ACCOUNT_SERVER=192.0.2.1\n"
	if err := os.WriteFile(env, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	open := func(name string, o Options) *AccountSettingsOrNil {
		t.Helper()
		o.Files = own{}
		o.Env = map[string]string{"XDG_DATA_HOME": filepath.Join(dir, name, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, name, "config")}
		p, err := OpenPhone(o, PhoneOptions{Held: true})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		return &AccountSettingsOrNil{p.Settings().Account}
	}
	if got := open("alone", Options{}); got.account != nil {
		t.Errorf("the .env of the working directory configured the account %+v", *got.account)
	}
	if got := open("named", Options{EnvFile: env}); got.account == nil || got.account.Username != "1001" {
		t.Errorf("the same file named explicitly gave the account %+v, want 1001: the test proved nothing", got.account)
	}
}

// AccountSettingsOrNil carries an account out of a helper.
type AccountSettingsOrNil struct{ account *domain.AccountSettings }

// A contact and its favorite mark are there after the softphone is closed and opened again (A77,
// A78).
func TestAContactAndItsFavoriteOutliveARestart(t *testing.T) {
	dir := t.TempDir()
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	first, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.SaveContact(0, NewContact{Name: "Ana Lima", Favorite: true, Numbers: []ContactNumber{{Number: "1002", Label: "desk"}, {Number: "1003"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SaveContact(0, NewContact{Name: "Bia", Numbers: []ContactNumber{{Number: "1004"}}}); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	list, err := second.Contacts("")
	if err != nil || len(list) != 2 {
		t.Fatalf("contacts after the restart: %+v, %v", list, err)
	}
	if ana := list[0]; ana.Name != "Ana Lima" || !ana.Favorite || len(ana.Numbers) != 2 || ana.Numbers[0].Number != "1002" || ana.Numbers[0].Label != "desk" {
		t.Errorf("the favorite after the restart: %+v, want Ana Lima first with her two numbers", ana)
	}
	if list[1].Name != "Bia" || list[1].Favorite {
		t.Errorf("the other contact after the restart: %+v", list[1])
	}
}

// history reads a phone's calls, newest first.
func history(t *testing.T, p *Phone) []HistoryEntry {
	t.Helper()
	calls, err := p.History(AllCalls, "")
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

// A call that a quit ended is in the history with its outcome when the softphone is opened again
// (A91): answered, with an end time.
func TestACallEndedByQuittingIsInTheHistory(t *testing.T) {
	o, _ := options(t, lab(t, 200))
	p, err := OpenPhone(o, PhoneOptions{Audio: &memory.Audio{Tone: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	until(t, p, "registration", registered)
	dial(t, p)
	answered(t, p)
	p.Close() // quit during the call
	again, err := OpenPhone(o, PhoneOptions{Held: true})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	calls := history(t, again)
	if len(calls) != 1 || calls[0].Call.Outcome != store.Answered || calls[0].Call.EndedUTC == nil || calls[0].Call.Remote != "1002" {
		t.Fatalf("the history after the quit: %+v, want the one answered call to 1002 with its end", calls)
	}
}

// A softphone that died during a call says so when it starts again (A81, A82): the call it left
// open is listed as interrupted. The death is a copy of its database taken while the call was
// up, opened by another softphone: no process is killed here.
func TestACallLeftOpenByADeadSoftphoneIsListedAsInterrupted(t *testing.T) {
	o, db := options(t, lab(t, 200))
	p, err := OpenPhone(o, PhoneOptions{Audio: &memory.Audio{Tone: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	dial(t, p)
	answered(t, p)
	// What the disk held at that moment.
	left := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(left, "go-microsip"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		from, err := os.Open(db + suffix)
		if err != nil {
			continue // no such file in this journal mode
		}
		to, err := os.OpenFile(filepath.Join(left, "go-microsip", filepath.Base(db)+suffix), os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(to, from); err != nil {
			t.Fatal(err)
		}
		from.Close()
		to.Close()
	}
	after := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": left, "XDG_CONFIG_HOME": filepath.Join(left, "config")}}
	next, err := OpenPhone(after, PhoneOptions{Held: true})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	calls := history(t, next)
	if len(calls) != 1 || calls[0].Call.Outcome != store.Interrupted || calls[0].Call.EndedUTC != nil {
		t.Fatalf("the history of the softphone that died: %+v, want its one call interrupted, with no end", calls)
	}
}

// The softphone forgets calls older than the retention at its start (A83, A84), and keeps the
// others.
func TestOldCallsAreForgottenWhenTheSoftphoneStarts(t *testing.T) {
	dir := t.TempDir()
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	first, err := OpenPhone(o, PhoneOptions{Held: true})
	if err != nil {
		t.Fatal(err)
	}
	first.Close() // the database exists now
	s, err := store.Open(StorePath(o.Env))
	if err != nil {
		t.Fatal(err)
	}
	day := int64(24 * 60 * 60)
	now := time.Now().Unix()
	for key, age := range map[string]int64{"old": int64(store.DefaultRetentionDays+10) * day, "recent": 10 * day} {
		started, ended := now-age, now-age+60
		lasted := int64(60_000)
		if _, err := s.FinishCall(store.CallRecord{SessionKey: key, StartedUTC: started, AnsweredUTC: &started, EndedUTC: &ended,
			Direction: domain.Outgoing, Remote: key, Outcome: store.Answered, DurationMS: &lasted}); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	second, err := OpenPhone(o, PhoneOptions{Held: true})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	calls := history(t, second)
	if len(calls) != 1 || calls[0].Call.Remote != "recent" {
		t.Fatalf("the history after the start: %+v, want only the call of ten days ago", calls)
	}
}
