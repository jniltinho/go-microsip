package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// until reads the phone's events until one satisfies want.
func until(t *testing.T, p *Phone, what string, want func(domain.Event) bool) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				t.Fatalf("the events ended before %s", what)
			}
			if want(ev) {
				return
			}
		case <-deadline:
			t.Fatalf("no %s", what)
		}
	}
}

func registered(ev domain.Event) bool {
	r, ok := ev.(domain.RegistrationChanged)
	return ok && r.State.Status == domain.Registered
}

// A window's phone registers the configured account on its own, journals the calls before their
// snapshots are delivered, is the one softphone of its database, and leaves nothing behind.
func TestPhoneRegistersJournalsAndCloses(t *testing.T) {
	o, _ := options(t, registrar(t))
	o.Locks = &memory.Locks{}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := OpenPhone(o, PhoneOptions{}); !errors.Is(err, ErrRunning) {
		t.Errorf("a second phone on the same database: %v, want ErrRunning", err)
	}
	until(t, p, "registration", registered)

	target, _ := domain.ParseDialTarget("1002")
	if err := p.Send(domain.Dial{Target: target}); err != nil {
		t.Fatal(err)
	}
	until(t, p, "final snapshot", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.IsFinal()
	})
	calls, err := p.History(AllCalls, "")
	if err != nil || len(calls) != 1 || calls[0].Call.Remote != "1002" || calls[0].Call.Outcome != store.BusyPeer {
		t.Fatalf("history when the final snapshot arrived: %+v, %v", calls, err)
	}
	if missed, _ := p.History(MissedCalls, ""); len(missed) != 0 {
		t.Errorf("an outgoing busy call is listed as missed: %+v", missed)
	}
	if err := p.DeleteCall(calls[0].ID); err != nil {
		t.Error(err)
	}

	p.Close()
	p.Close() // twice is once
	var last domain.Event
	for ev := range p.Events() {
		last = ev
	}
	if _, stopped := last.(domain.Stopped); !stopped {
		t.Errorf("last event %T, want Stopped", last)
	}
	var refused Refused
	if err := p.Send(domain.Unregister{}); !errors.As(err, &refused) || err.Error() != "engine_failed" {
		t.Errorf("a command after the end: %v, want engine_failed", err)
	}
	again, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatalf("a phone after the first one closed: %v", err)
	}
	again.Close()
}

// Without an account the phone still opens: the window shows its settings. What is saved there is
// what the next read returns, a typed password is kept for the session only, and it is never in
// the database.
func TestPhoneOpensWithoutAnAccountAndTakesOneFromTheWindow(t *testing.T) {
	port := registrar(t)
	dir := t.TempDir()
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if s := p.Settings(); s.Account != nil || s.HasPassword || s.Theme != string(config.ThemeDark) {
		t.Fatalf("settings of a first start: %+v", s)
	}
	// The skin is saved before any account exists, and an unknown one becomes "system".
	xp, unknown, mic := "xp", "nineties", "mic-7"
	if err := p.SaveSettings(SettingsPatch{Theme: &xp, AudioInput: &mic}); err != nil {
		t.Fatal(err)
	}
	if s := p.Settings(); s.Theme != "xp" || s.AudioInput != "mic-7" || s.Account != nil {
		t.Errorf("after saving the skin: %+v", s)
	}
	if err := p.SaveSettings(SettingsPatch{Theme: &unknown}); err != nil || p.Settings().Theme != "system" {
		t.Errorf("an unknown skin: %v, saved as %q", err, p.Settings().Theme)
	}

	account := domain.AccountSettings{Username: "1001", Domain: "127.0.0.1", Server: "127.0.0.1", Port: uint16(port), Transport: domain.TCP, Expires: 60}
	if err := p.Register(account, nil); !errors.Is(err, ErrNoPassword) {
		t.Errorf("registering without any password: %v, want ErrNoPassword", err)
	}
	invalid := account
	invalid.Media = domain.SRTP // SRTP needs TLS
	if err := p.SaveSettings(SettingsPatch{Account: &invalid}); err == nil {
		t.Error("an invalid account was saved")
	}
	if err := p.SaveSettings(SettingsPatch{Account: &account}); err != nil {
		t.Fatal(err)
	}
	password := "typed-in-the-window"
	if err := p.Register(account, &password); err != nil {
		t.Fatal(err)
	}
	until(t, p, "registration", registered)
	s := p.Settings()
	if s.Account == nil || s.Account.Username != "1001" || s.Account.Port != uint16(port) || !s.HasPassword || s.Theme != "system" {
		t.Errorf("settings after the account was saved: %+v", s)
	}
	if err := p.Register(account, nil); err != nil { // the password of this session
		t.Errorf("registering again without retyping: %v", err)
	}
	values, err := p.journal.store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range values {
		if value == password {
			t.Errorf("the typed password is stored under %q", key)
		}
	}

	// Do-not-disturb is remembered for the next start.
	if err := p.SetDND(true); err != nil || !p.Settings().DND {
		t.Errorf("do-not-disturb: %v, saved %v", err, p.Settings().DND)
	}
}

// A field the configuration file holds is not the window's to change: saving it is refused, and
// the error names the field.
func TestPhoneSettingsRefuseWhatTheFileHolds(t *testing.T) {
	o, _ := options(t, registrar(t))
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s := p.Settings()
	if s.Account == nil || !s.HasPassword {
		t.Fatalf("settings from the file: %+v", s)
	}
	changed := *s.Account
	changed.Port++
	var held *config.Error
	if err := p.SaveSettings(SettingsPatch{Account: &changed}); !errors.As(err, &held) || held.Kind != config.Overridden || held.Field != config.Port {
		t.Errorf("changing the port the file holds: %v", err)
	}
	// The same account, unchanged, and a skin the file does not hold are fine.
	light := "light"
	if err := p.SaveSettings(SettingsPatch{Account: s.Account, Theme: &light}); err != nil || p.Settings().Theme != "light" {
		t.Errorf("saving what the file does not hold: %v, skin %q", err, p.Settings().Theme)
	}
}

// Contacts go to the database and come back, favorites first.
func TestPhoneContacts(t *testing.T) {
	o, _ := options(t, registrar(t))
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ana, err := p.SaveContact(0, NewContact{Name: "Ana", Numbers: []ContactNumber{{Number: "1002", Label: "work"}}})
	if err != nil || ana.ID == 0 {
		t.Fatalf("add: %+v, %v", ana, err)
	}
	if _, err := p.SaveContact(0, NewContact{Name: "Bia", Favorite: true, Numbers: []ContactNumber{{Number: "1003"}}}); err != nil {
		t.Fatal(err)
	}
	ana, err = p.SaveContact(ana.ID, NewContact{Name: "Ana Lima", Numbers: []ContactNumber{{Number: "1004"}}})
	if err != nil || ana.Name != "Ana Lima" || len(ana.Numbers) != 1 || ana.Numbers[0].Number != "1004" {
		t.Errorf("replace: %+v, %v", ana, err)
	}
	all, err := p.Contacts("")
	if err != nil || len(all) != 2 || all[0].Name != "Bia" {
		t.Errorf("list: %+v, %v", all, err)
	}
	if found, _ := p.Contacts("1004"); len(found) != 1 || found[0].ID != ana.ID {
		t.Errorf("search by number: %+v", found)
	}
	if err := p.DeleteContact(ana.ID); err != nil {
		t.Error(err)
	}
	if left, _ := p.Contacts(""); len(left) != 1 {
		t.Errorf("after the delete: %+v", left)
	}
}

// A configuration that is not complete yet still protects what a file or the environment holds:
// the window cannot register one account and leave another one configured.
func TestPhoneKeepsOverridesOfAnIncompleteConfiguration(t *testing.T) {
	port := registrar(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(file, []byte("schema = 1\n[account]\nusername = \"1001\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := Options{ConfigPath: file, Files: own{}, Env: map[string]string{
		"XDG_DATA_HOME": filepath.Join(dir, "data"), "GO_MICROSIP_ACCOUNT_TRANSPORT": "tcp",
	}}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if s := p.Settings(); s.Account != nil {
		t.Fatalf("an account without a domain is offered as configured: %+v", s.Account)
	}
	account := domain.AccountSettings{Username: "1002", Domain: "127.0.0.1", Server: "127.0.0.1", Port: uint16(port), Transport: domain.TCP, Expires: 60}
	var held *config.Error
	if err := p.SaveSettings(SettingsPatch{Account: &account}); !errors.As(err, &held) || held.Kind != config.Overridden || held.Field != config.Username {
		t.Errorf("another username than the file's: %v, want the override refused", err)
	}
	account.Username, account.Transport = "1001", domain.UDP
	if err := p.SaveSettings(SettingsPatch{Account: &account}); !errors.As(err, &held) || held.Kind != config.Overridden || held.Field != config.Transport {
		t.Errorf("another transport than the environment's: %v, want the override refused", err)
	}
	if values, _ := p.journal.store.LoadConfig(); len(values) != 0 {
		t.Errorf("a refused edit stored %v", values)
	}
	account.Transport = domain.TCP
	if err := p.SaveSettings(SettingsPatch{Account: &account}); err != nil {
		t.Fatalf("the account that agrees with the file and the environment: %v", err)
	}
	if s := p.Settings(); s.Account == nil || s.Account.Username != "1001" || s.Account.Domain != "127.0.0.1" || s.Account.Transport != domain.TCP {
		t.Errorf("settings after the save: %+v", s.Account)
	}
	// Values the reader refuses are not stored either.
	never := account
	never.Expires = 0
	if err := p.SaveSettings(SettingsPatch{Account: &never}); err == nil {
		t.Error("a registration that never expires was saved")
	}
	if s := p.Settings(); s.Account == nil || s.Account.Expires != 60 {
		t.Errorf("after the refused edit: %+v", s.Account)
	}
}
