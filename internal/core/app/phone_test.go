package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
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
	// The latest calls, as a tray menu lists them: the number, then the contact that has it.
	if recent := p.Recent(TrayCalls); len(recent) != 1 || recent[0] != (RecentCall{Label: "\u2197 1002", Number: "1002"}) {
		t.Errorf("the latest calls: %+v", recent)
	}
	ana, err := p.SaveContact(0, NewContact{Name: "Ana Lima", Numbers: []ContactNumber{{Number: "1002"}}})
	if err != nil {
		t.Fatal(err)
	}
	if recent := p.Recent(TrayCalls); len(recent) != 1 || recent[0] != (RecentCall{Label: "\u2197 Ana Lima", Number: "1002"}) {
		t.Errorf("the latest calls with a contact: %+v", recent)
	}
	if err := p.DeleteContact(ana.ID); err != nil {
		t.Fatal(err)
	}
	if p.Recent(0) != nil {
		t.Error("calls listed with a limit of none")
	}
	for label, want := range map[string]string{
		recentLabel(domain.Incoming, store.Missed, "1003"):     "Missed: 1003",
		recentLabel(domain.Incoming, store.Answered, "Bia"):    "\u2199 Bia",
		recentLabel(domain.Outgoing, store.NoAnswer, "1004"):   "\u2197 1004",
		recentLabel(domain.Incoming, store.Rejected, "Carlos"): "\u2199 Carlos",
	} {
		if label != want {
			t.Errorf("label %q, want %q", label, want)
		}
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

// A configuration file that cannot be read is not an incomplete account: the window opens, and
// nothing is saved over what nobody could read, not even a setting that looks harmless.
func TestPhoneSavesNothingOverAnUnreadableConfiguration(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config", config.DirName, "config.toml")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("schema = 1\n[account\nusername = \"1001\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	account := domain.AccountSettings{Username: "1002", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5060, Expires: 300}
	xp := "xp"
	for name, patch := range map[string]SettingsPatch{"an account": {Account: &account}, "a skin": {Theme: &xp}} {
		if err := p.SaveSettings(patch); err == nil {
			t.Errorf("%s was saved over a configuration file that cannot be read", name)
		}
	}
	if values, err := p.journal.store.LoadConfig(); err != nil || len(values) != 0 {
		t.Errorf("the database holds %v (%v) after the refused saves", values, err)
	}
	// Once the file is fixed the same edit goes through, and the next start resolves.
	if err := os.WriteFile(file, []byte("schema = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveSettings(SettingsPatch{Account: &account}); err != nil {
		t.Fatalf("the account, once the file can be read: %v", err)
	}
	if _, err := config.Resolve(p.context()); err != nil {
		t.Errorf("the saved configuration does not resolve: %v", err)
	}
}

// Do-not-disturb saved before any account exists is applied at the next start, account or not:
// the window shows it on, so the engine must have it on.
func TestSavedDoNotDisturbIsAppliedWithoutAnAccount(t *testing.T) {
	dnd := config.Stored{UI: config.UI{DND: true}}
	for name, c := range map[string]struct {
		draft  config.Stored
		loaded bool
		cfg    *config.Effective
		want   []domain.Command
	}{
		"saved, no account yet":            {dnd, true, nil, []domain.Command{domain.SetDND{On: true}}},
		"not saved":                        {config.Stored{}, true, nil, nil},
		"the settings could not be read":   {dnd, false, nil, nil},
		"saved, an account and a password": {dnd, true, &config.Effective{PasswordSource: config.PasswordFromEnv}, []domain.Command{domain.SetDND{On: true}, domain.Register{}}},
		"an account without a password":    {config.Stored{}, true, &config.Effective{}, nil},
	} {
		got := startup(c.draft, c.loaded, c.cfg, domain.Secret{})
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
			continue
		}
		for i := range got {
			if fmt.Sprintf("%T", got[i]) != fmt.Sprintf("%T", c.want[i]) {
				t.Errorf("%s: command %d is %T, want %T", name, i, got[i], c.want[i])
			}
		}
	}

	// And through a real start: saved with no account, the next phone on the same profile reads it
	// back as on.
	dir := t.TempDir()
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	first, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetDND(true); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if s := second.Settings(); !s.DND || s.Account != nil {
		t.Errorf("settings at the second start: %+v", s)
	}
}

// The sound devices chosen before any account exists are the ones the engine starts with at the
// next start, account or not: a call of an account completed later must not go to the system's
// default microphone and speaker while the settings show a headset.
func TestSavedSoundDevicesAreUsedWithoutAnAccount(t *testing.T) {
	dir := t.TempDir()
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	first, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.input != "" || first.output != "" {
		t.Errorf("a first start uses the devices %q and %q, want the system's", first.input, first.output)
	}
	mic, speaker := "headset-mic", "headset-speaker"
	if err := first.SaveSettings(SettingsPatch{AudioInput: &mic, AudioOutput: &speaker}); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.input != mic || second.output != speaker {
		t.Errorf("the engine was started with the devices %q and %q, want the saved %q and %q", second.input, second.output, mic, speaker)
	}
	if s := second.Settings(); s.Account != nil || s.AudioInput != mic || s.AudioOutput != speaker {
		t.Errorf("settings at the second start: %+v", s)
	}
}

// A value in the configuration file that cannot be read (a transport in capitals) is not an
// account the window may complete: the account is not offered as configured, saving one is
// refused with the file's error, and nothing is stored.
func TestPhoneRefusesAnAccountOverAnInvalidFileValue(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := "schema = 1\n[account]\nusername = \"1001\"\ndomain = \"pbx.invalid\"\ntransport = \"UDP\"\n"
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	o := Options{ConfigPath: file, Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if s := p.Settings(); s.Account != nil {
		t.Fatalf("a configuration that does not resolve offers an account: %+v", s.Account)
	}
	// What the form would send: the file's account with the default the draft put in place of the
	// transport it could not read.
	account := domain.AccountSettings{Username: "1001", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5060, Transport: domain.UDP, Expires: config.DefaultExpires}
	var invalid *config.Error
	if err := p.SaveSettings(SettingsPatch{Account: &account}); !errors.As(err, &invalid) || invalid.Kind != config.Invalid || invalid.Field != config.Transport {
		t.Errorf("saving over the unreadable transport: %v, want it refused by name", err)
	}
	if values, err := p.journal.store.LoadConfig(); err != nil || len(values) != 0 {
		t.Errorf("the database holds %v (%v) after the refused save", values, err)
	}
}

// The same when the file holds every field, so that the edit has nothing to write: saving is still
// refused, not reported as done.
func TestPhoneRefusesAnAccountWhenTheFileHoldsEverything(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := "schema = 1\n[account]\ndisplay_name = \"\"\nusername = \"1001\"\nauth_username = \"\"\ndomain = \"pbx.invalid\"\nserver = \"pbx.invalid\"\nport = 5060\n" +
		"transport = \"UDP\"\nmedia = \"rtp\"\nexpires = 300\npassword_env = \"GO_MICROSIP_SIP_PASSWORD\"\n[ui]\ntheme = \"dark\"\ndnd = false\n"
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	o := Options{ConfigPath: file, Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	account := domain.AccountSettings{Username: "1001", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5060, Transport: domain.UDP, Expires: 300}
	var invalid *config.Error
	if err := p.SaveSettings(SettingsPatch{Account: &account}); !errors.As(err, &invalid) || invalid.Kind != config.Invalid || invalid.Field != config.Transport {
		t.Errorf("saving the account the file holds entirely, one value unreadable: %v, want it refused by name", err)
	}
}

// A TLS account whose configured CA bundle cannot be used never registers against the system's
// roots: not on its own at the start, and not when the window asks. A bundle configured after the
// start needs a restart.
func TestPhoneDoesNotRegisterOverTLSWithoutItsBundle(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	write := func(ca string) {
		text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1201\"\ndomain = \"127.0.0.1\"\nport = 5061\ntransport = \"tls\"\nmedia = \"srtp\"\n%s", ca)
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(fmt.Sprintf("ca_file = %q\n", filepath.Join(dir, "absent-ca.pem")))
	o := Options{ConfigPath: file, Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "GO_MICROSIP_SIP_PASSWORD": "lab"}}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	refused := func(ev domain.Event) bool {
		r, ok := ev.(domain.RegistrationChanged)
		if ok && (r.State.Status == domain.Registering || r.State.Status == domain.Registered) {
			t.Fatalf("the account went to %v without its CA bundle", r.State)
		}
		return ok && r.State.Status == domain.RegistrationFailed && r.State.Failure == domain.Certificate
	}
	until(t, p, "the refusal at the start", refused)
	s := p.Settings()
	if s.Account == nil || !s.HasPassword {
		t.Fatalf("settings: %+v", s)
	}
	if err := p.Register(*s.Account, nil); err != nil { // what Menu, Register does
		t.Fatal(err)
	}
	// The state is already "failed: certificate" and stays there: for a moment, nothing else
	// (refused fails the test on "registering" or "registered").
	for quiet := time.After(1500 * time.Millisecond); ; {
		select {
		case ev := <-p.Events():
			refused(ev)
			continue
		case <-quiet:
		}
		break
	}

	// A phone started without any bundle, to which one is configured later.
	p.Close()
	write("")
	later, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer later.Close()
	write(fmt.Sprintf("ca_file = %q\n", filepath.Join(dir, "absent-ca.pem")))
	if err := later.Register(*s.Account, nil); !errors.Is(err, ErrRestartForCA) {
		t.Errorf("registering over TLS after the CA bundle changed: %v, want the restart asked for", err)
	}
	// A ca_file that is not a valid setting (a relative path) reads as "none": that is not the
	// absence of a bundle, and a typed password does not get past it.
	write("ca_file = \"lab-ca.pem\"\n")
	typed := "typed-in-the-window"
	var invalid *config.Error
	if err := later.Register(*s.Account, &typed); !errors.As(err, &invalid) || invalid.Field != config.CAFile {
		t.Errorf("registering over TLS with an invalid ca_file: %v, want the setting refused by name", err)
	}
}

// During a call the engine keeps its account: the window can neither save nor register another
// one, so what it shows as the account stays the one in use. The skin can still change.
func TestPhoneKeepsItsAccountDuringACall(t *testing.T) {
	o, _ := options(t, ringingRegistrar(t))
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	target, _ := domain.ParseDialTarget("1002")
	if err := p.Send(domain.Dial{Target: target}); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the call ringing", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.Kind == domain.Ringing
	})
	account := *p.Settings().Account
	other := account
	other.DisplayName = "Another"
	password := "another"
	var busy Refused
	if err := p.SaveSettings(SettingsPatch{Account: &other}); !errors.As(err, &busy) || err.Error() != "busy" {
		t.Errorf("saving another account during a call: %v, want busy", err)
	}
	if err := p.Register(other, &password); !errors.As(err, &busy) || err.Error() != "busy" {
		t.Errorf("registering another account during a call: %v, want busy", err)
	}
	if s := p.Settings(); s.Account == nil || s.Account.DisplayName != "" {
		t.Errorf("the account after the refused change: %+v", s.Account)
	}
	light := "light"
	if err := p.SaveSettings(SettingsPatch{Theme: &light}); err != nil {
		t.Errorf("changing the skin during a call: %v", err)
	}
	// Once the call is over the account can change again.
	if err := p.Send(domain.Hangup{Call: 1}); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the call's end", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.IsFinal()
	})
	if err := p.SaveSettings(SettingsPatch{Account: &account}); err != nil {
		t.Errorf("saving the account after the call: %v", err)
	}
}

// A password typed in the window is remembered in the keyring, and only there: the database keeps
// a reference that says nothing, no file holds the password, and the next start registers by
// itself. Without a keyring the password lives for the session and the next start asks again.
func TestATypedPasswordIsKeptInTheKeyringForTheNextStart(t *testing.T) {
	const typed = "fictional-typed-password-4471"
	port := registrar(t)
	fresh := func(secrets ports.Secrets) (Options, *[]string, string) {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.toml")
		text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1001\"\ndomain = \"127.0.0.1\"\nport = %d\ntransport = \"tcp\"\nexpires = 60\n", port)
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		var warnings []string
		o := Options{
			ConfigPath: file, Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")},
			Secrets: secrets, Locks: &memory.Locks{}, Warn: func(m string) { warnings = append(warnings, m) },
		}
		return o, &warnings, dir
	}
	quiet := func(p *Phone) { // nothing registers by itself
		t.Helper()
		select {
		case ev := <-p.Events():
			if r, ok := ev.(domain.RegistrationChanged); ok && r.State.Status != domain.Disabled {
				t.Fatalf("a registration (%v) without a password", r.State)
			}
		case <-time.After(500 * time.Millisecond):
		}
	}

	keyring := &memory.Keyring{}
	o, warnings, dir := fresh(keyring)
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	quiet(p)
	if p.Settings().HasPassword {
		t.Fatal("a password before one was typed")
	}
	account := *p.Settings().Account
	password := typed
	if err := p.Register(account, &password); err != nil {
		t.Fatal(err)
	}
	until(t, p, "registration with the typed password", registered)
	p.Close()
	if len(*warnings) != 0 {
		t.Errorf("warnings with a keyring that works: %v", *warnings)
	}
	entries := keyring.Entries()
	if len(entries) != 1 {
		t.Fatalf("the keyring holds %d entries, want the one password", len(entries))
	}
	for ref, value := range entries {
		if value != typed || !strings.HasPrefix(ref, "go-microsip/") || strings.Contains(ref, "1001") {
			t.Errorf("the keyring entry is not the password under a reference that says nothing: %q", ref)
		}
	}
	// Nothing the session wrote holds the password.
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err == nil && bytes.Contains(content, []byte(typed)) {
			t.Errorf("%s holds the password", filepath.Base(path))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// The next start registers by itself, with the kept password.
	p, err = OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	until(t, p, "registration at the next start", registered)
	if !p.Settings().HasPassword {
		t.Error("the kept password is not reported as at hand")
	}
	// Typing another one replaces the entry: there is still one.
	other := typed + "-changed"
	if err := p.Register(account, &other); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if entries := keyring.Entries(); len(entries) != 1 {
		t.Errorf("after another password the keyring holds %d entries", len(entries))
	} else {
		for _, value := range entries {
			if value != other {
				t.Error("the keyring does not hold the password typed last")
			}
		}
	}

	// The doctor says where the password is, and nothing of it.
	if report := Doctor(o); !report.PasswordPresent || report.PasswordSource == nil || *report.PasswordSource != "keyring" ||
		strings.Contains(report.String(), other) || strings.Contains(report.JSON(), other) {
		t.Errorf("the doctor with a password in the keyring: %s", report)
	}
	// An imported configuration replaces the account: its password leaves the keyring with it.
	imported := filepath.Join(dir, "imported.toml")
	if err := os.WriteFile(imported, []byte("schema = 1\n[account]\nusername = \"1009\"\ndomain = \"pbx.invalid\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(o, imported, true); err != nil {
		t.Fatal(err)
	}
	if entries := keyring.Entries(); len(entries) != 0 {
		t.Errorf("after the account was replaced the keyring still holds %d entries", len(entries))
	}
	if report := Doctor(o); report.PasswordPresent {
		t.Errorf("the doctor after the account was replaced: %s", report)
	}

	// Without a keyring: the session registers, the warning says the password is not kept, and
	// the next start has none.
	o, warnings, _ = fresh(&memory.Keyring{Unavailable: true})
	p, err = OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Register(account, &password); err != nil {
		t.Fatal(err)
	}
	until(t, p, "registration for the session", registered)
	p.Close()
	if len(*warnings) != 1 || !strings.Contains((*warnings)[0], "for this session only") || strings.Contains((*warnings)[0], typed) {
		t.Errorf("warnings without a keyring: %v", *warnings)
	}
	p, err = OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	quiet(p)
	if p.Settings().HasPassword {
		t.Error("a password at the next start without a keyring")
	}
}

// slowKeyring is a keyring whose first write waits until it is let go.
type slowKeyring struct {
	memory.Keyring
	first   sync.Once
	started chan struct{} // closed when the first write has begun
	release chan struct{} // closed to let it finish
}

func (k *slowKeyring) Set(ref, password string) error {
	k.first.Do(func() {
		close(k.started)
		<-k.release
	})
	return k.Keyring.Set(ref, password)
}

// Two registrations one after the other, the first one's keyring write slow: the password that
// stays is the second one's. The second waits for the first to be whole instead of passing it.
func TestThePasswordKeptIsTheOneOfTheLastRegistration(t *testing.T) {
	port := registrar(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1001\"\ndomain = \"127.0.0.1\"\nport = %d\ntransport = \"tcp\"\nexpires = 60\n", port)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	keyring := &slowKeyring{started: make(chan struct{}), release: make(chan struct{})}
	o := Options{ConfigPath: file, Files: own{}, Locks: &memory.Locks{}, Secrets: keyring,
		Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	account := *p.Settings().Account
	first, second := "fictional-first-password", "fictional-second-password"
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- p.Register(account, &first) }()
	<-keyring.started // the first registration is at its keyring write
	go func() { secondDone <- p.Register(account, &second) }()
	select {
	case err := <-secondDone:
		t.Fatalf("the second registration finished (%v) while the first was still writing its password", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(keyring.release)
	for name, done := range map[string]chan error{"first": firstDone, "second": secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("the %s registration: %v", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("the %s registration did not finish", name)
		}
	}
	entries := keyring.Entries()
	if len(entries) != 1 {
		t.Fatalf("the keyring holds %d entries", len(entries))
	}
	for _, kept := range entries {
		if kept != second {
			t.Error("the keyring kept the first registration's password, not the last one's")
		}
	}
}

// lateKeyring is a keyring that says its time is up on every write, having taken it or not.
type lateKeyring struct {
	memory.Keyring
	takes bool
}

func (k *lateKeyring) Set(ref, password string) error {
	if k.takes {
		_ = k.Keyring.Set(ref, password)
	}
	return ports.ErrSecretsTimeout
}

// A write the keyring did not answer in time may or may not have been taken. The session goes on
// with the password, the warning says the outcome is not known, and the reference is recorded
// either way: the next start finds the password if the keyring took it, and asks if it did not.
// Nothing is left in the keyring under a reference the database does not know.
func TestAKeyringThatAnswersLateLeavesNothingAstray(t *testing.T) {
	port := registrar(t)
	for _, takes := range []bool{true, false} {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.toml")
		text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1001\"\ndomain = \"127.0.0.1\"\nport = %d\ntransport = \"tcp\"\nexpires = 60\n", port)
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		keyring := &lateKeyring{takes: takes}
		var warnings []string
		o := Options{ConfigPath: file, Files: own{}, Locks: &memory.Locks{}, Secrets: keyring,
			Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}, Warn: func(m string) { warnings = append(warnings, m) }}
		p, err := OpenPhone(o, PhoneOptions{})
		if err != nil {
			t.Fatal(err)
		}
		account := *p.Settings().Account
		password := "fictional-late-password"
		if err := p.Register(account, &password); err != nil {
			t.Fatal(err)
		}
		until(t, p, "registration for the session", registered)
		p.Close()
		if len(warnings) != 1 || !strings.Contains(warnings[0], "not known") || strings.Contains(warnings[0], password) {
			t.Errorf("takes=%v: warnings %v, want one that says the outcome is not known", takes, warnings)
		}
		p, err = OpenPhone(o, PhoneOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Settings().HasPassword; got != takes {
			t.Errorf("takes=%v: a password at the next start: %v", takes, got)
		}
		if takes {
			until(t, p, "registration at the next start with the password the keyring took", registered)
		}
		p.Close()
		// Every entry of the keyring is one the database names.
		for ref := range keyring.Entries() {
			if report := Doctor(o); !report.PasswordPresent {
				t.Errorf("takes=%v: the keyring holds %q, which nothing refers to", takes, ref)
			}
		}
	}
}

// tardyKeyring is a keyring whose first write says its time is up and is done only when the test
// lets it: after the writes that followed. It remembers the password read last.
type tardyKeyring struct {
	memory.Keyring
	first sync.Once
	late  func()
	read  string
}

func (k *tardyKeyring) Set(ref, password string) error {
	tardy := false
	k.first.Do(func() {
		tardy = true
		k.late = func() { _ = k.Keyring.Set(ref, password) }
	})
	if tardy {
		return ports.ErrSecretsTimeout
	}
	return k.Keyring.Set(ref, password)
}

func (k *tardyKeyring) Get(ref string) (string, bool, error) {
	password, ok, err := k.Keyring.Get(ref)
	k.read = password
	return password, ok, err
}

// A keyring may finish a write after the application gave up on it, and after the write of a
// password typed later. The later password is the one the next start reads: each has an entry
// of its own, so the late one replaces nothing.
func TestAWriteThatLandsLateDoesNotReplaceTheLaterPassword(t *testing.T) {
	port := registrar(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1001\"\ndomain = \"127.0.0.1\"\nport = %d\ntransport = \"tcp\"\nexpires = 60\n", port)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	keyring := &tardyKeyring{}
	o := Options{ConfigPath: file, Files: own{}, Locks: &memory.Locks{}, Secrets: keyring,
		Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}, Warn: func(string) {}}
	p, err := OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	account := *p.Settings().Account
	first, second := "fictional-first-password", "fictional-second-password"
	for _, password := range []string{first, second} {
		if err := p.Register(account, &password); err != nil {
			t.Fatal(err)
		}
	}
	keyring.late() // the first write is done now, after the second
	p.Close()
	p, err = OpenPhone(o, PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration at the next start", registered)
	if keyring.read != second {
		t.Error("the next start read the password of the write that landed late, not the one typed last")
	}
}

// A phone opened Held tells the PBX nothing until Begin: the headless service takes its grant
// in between, and a service that is refused has registered nowhere.
func TestAHeldPhoneRegistersOnlyWhenItBegins(t *testing.T) {
	port := registrar(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1001\"\ndomain = \"127.0.0.1\"\nport = %d\ntransport = \"tcp\"\nexpires = 60\n", port)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	o := Options{ConfigPath: file, Files: own{}, Locks: &memory.Locks{},
		Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "GO_MICROSIP_SIP_PASSWORD": "fictional-held-password"}}
	p, err := OpenPhone(o, PhoneOptions{Held: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if !p.Settings().HasPassword {
		t.Fatal("the configured password is not at hand")
	}
	select {
	case ev := <-p.Events():
		if r, ok := ev.(domain.RegistrationChanged); ok && r.State.Status != domain.Disabled {
			t.Fatalf("a held phone registered (%v)", r.State)
		}
	case <-time.After(700 * time.Millisecond):
	}
	p.Begin()
	until(t, p, "registration after Begin", registered)
	p.Begin() // a second time: nothing more to tell
}
