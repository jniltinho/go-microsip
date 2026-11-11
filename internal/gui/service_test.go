package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
)

// own is a ports.FileAccess that owns every file.
type own struct{}

func (own) CurrentUser() uint32 { return 1000 }
func (own) Inspect(info os.FileInfo) (owner, mode uint32) {
	return 1000, uint32(info.Mode().Perm())
}

// Events have the shape frontend/src/api.ts declares, field for field.
func TestEventsHaveTheContractsShape(t *testing.T) {
	for _, c := range []struct {
		event domain.Event
		want  string
	}{
		{domain.RegistrationChanged{State: domain.RegistrationState{Status: domain.Registered}}, `{"type":"registration","registration":{"state":"registered"}}`},
		{
			domain.RegistrationChanged{State: domain.RegistrationState{Status: domain.RegistrationFailed, Failure: domain.Rejected, Code: 503}},
			`{"type":"registration","registration":{"state":"failed","reason":"rejected","code":503}}`,
		},
		{
			domain.RegistrationChanged{State: domain.RegistrationState{Status: domain.RegistrationFailed, Failure: domain.Timeout, Code: 9}},
			`{"type":"registration","registration":{"state":"failed","reason":"timeout"}}`,
		},
		{
			domain.CallChanged{Call: domain.CallSnapshot{ID: 7, Direction: domain.Incoming, Remote: `<img src=x onerror=alert(1)>`, Phase: domain.CallPhase{Kind: domain.Ringing}, Version: 2}},
			`{"type":"call","call":{"id":7,"direction":"incoming","remote":"\u003cimg src=x onerror=alert(1)\u003e","phase":"ringing","muted":false,"secure":false,"version":2}}`,
		},
		{
			domain.CallChanged{Call: domain.CallSnapshot{
				ID: 8, Remote: "1002", Phase: domain.EndedWith(domain.EndNoAnswer), Muted: true, Secure: true,
				Transfer: domain.TransferState{Kind: domain.TransferFailed, Code: 486}, Version: 9,
			}},
			`{"type":"call","call":{"id":8,"direction":"outgoing","remote":"1002","phase":"ended","endReason":"no_answer","muted":true,"secure":true,"transfer":{"state":"failed","code":486},"version":9}}`,
		},
		{domain.Noticed{Notice: domain.NoticeMicrophoneLost}, `{"type":"notice","notice":"microphone_lost"}`},
		{domain.Stopped{}, `{"type":"stopped"}`},
	} {
		got, err := json.Marshal(uiEvent(c.event))
		if err != nil || string(got) != c.want {
			t.Errorf("%+v:\n got  %s\n want %s (%v)", c.event, got, c.want, err)
		}
	}
	if got, _ := json.Marshal(UIEvent{Type: "approvals", Approvals: &[]Approval{}}); string(got) != `{"type":"approvals","approvals":[]}` {
		t.Errorf("an empty approvals list: %s", got)
	}
}

// page is what the page would have received.
type page struct {
	mu     sync.Mutex
	events []UIEvent
}

func (p *page) receive(ev UIEvent) {
	p.mu.Lock()
	p.events = append(p.events, ev)
	p.mu.Unlock()
}

// all is what the page has received so far.
func (p *page) all() []UIEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]UIEvent(nil), p.events...)
}

// wait returns the first event of the type, from the given position on, and the position after it.
func (p *page) wait(t *testing.T, from int, kind string) (UIEvent, int) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		p.mu.Lock()
		for i := from; i < len(p.events); i++ {
			if p.events[i].Type == kind {
				ev := p.events[i]
				p.mu.Unlock()
				return ev, i + 1
			}
		}
		p.mu.Unlock()
	}
	t.Fatalf("no %q event", kind)
	return UIEvent{}, 0
}

// The page drives the phone through the bound methods: nothing is delivered before it subscribes,
// the current state comes first, refusals are notice names, and a shutdown it asked for ends the
// application.
func TestThePageDrivesThePhone(t *testing.T) {
	dir := t.TempDir()
	core := app.Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	phone, err := app.OpenPhone(core, app.PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	var seen page
	quit := make(chan struct{})
	var once sync.Once
	svc := &App{phone: phone, emit: seen.receive, quit: func() { once.Do(func() { close(quit) }) }}
	go svc.forward()

	if err := svc.Dial("1002"); err != nil { // queued; the engine refuses it, before any page listens
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if early := seen.all(); len(early) != 0 {
		t.Fatalf("events before the page subscribed: %+v", early)
	}
	if err := svc.Subscribe(); err != nil {
		t.Fatal(err)
	}
	if first := seen.all(); len(first) < 2 || first[0].Type != "registration" || first[0].Registration.State != "disabled" ||
		first[1].Type != "approvals" || len(*first[1].Approvals) != 0 {
		t.Fatalf("what a subscribing page gets first: %+v", first)
	}
	if err := svc.Dial("1002"); err != nil {
		t.Fatal(err)
	}
	if ev, _ := seen.wait(t, 2, "notice"); ev.Notice != "not_registered" {
		t.Errorf("a call without a registration: %+v", ev)
	}
	for name, err := range map[string]error{
		"a dial target with a space": svc.Dial("10 02"),
		"a DTMF key that is not one": svc.Dtmf(1, "x"),
		"two DTMF keys at once":      svc.Dtmf(1, "12"),
		"an unknown transport":       svc.Register(Account{Username: "1001", Domain: "pbx.invalid", Transport: "sctp", Media: "rtp"}, nil),
		"registering without a password": svc.Register(Account{
			Username: "1001", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5060, Transport: "udp", Media: "rtp", Expires: 300,
		}, nil),
	} {
		if err == nil || err.Error() != "invalid_state" {
			t.Errorf("%s: %v, want the invalid_state refusal", name, err)
		}
	}

	xp := "xp"
	if err := svc.SaveSettings(SettingsPatch{Theme: &xp}); err != nil {
		t.Fatal(err)
	}
	if s, err := svc.Settings(); err != nil || s.Theme != "xp" || s.Account != nil || s.HasPassword {
		t.Errorf("settings: %+v, %v", s, err)
	}
	saved, err := svc.SaveContact(Contact{Name: "Ana", Numbers: []ContactNumber{{Number: "1002"}}})
	if err != nil || saved.ID == 0 {
		t.Fatalf("contact: %+v, %v", saved, err)
	}
	if list, err := svc.Contacts("ana"); err != nil || len(list) != 1 || list[0].Numbers[0].Number != "1002" {
		t.Errorf("contacts: %+v, %v", list, err)
	}
	if list, err := svc.History("all", ""); err != nil || list == nil || len(list) != 0 {
		t.Errorf("history: %#v, %v (an empty list must not be null for the page)", list, err)
	}
	if _, err := svc.History("everything", ""); err == nil {
		t.Error("an unknown history filter was accepted")
	}
	if err := svc.Approve("1"); err == nil {
		t.Error("an agent request was approved in a build without agent control")
	}

	// Values the configuration reader would refuse at the next start are refused here, and
	// nothing of them is stored.
	newline := "mic\nwith a newline"
	account := Account{Username: "1001", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5060, Transport: "udp", Media: "rtp", Expires: 300}
	never := account
	never.Expires = 0
	for name, patch := range map[string]SettingsPatch{
		"a registration that never expires":      {Account: &never},
		"a device name with a control character": {AudioInput: &newline},
	} {
		if err := svc.SaveSettings(patch); err == nil {
			t.Errorf("%s was saved", name)
		}
	}
	if s, _ := svc.Settings(); s.Account != nil || s.AudioInput != "" {
		t.Errorf("a refused edit left something behind: %+v", s)
	}
	if err := svc.SaveSettings(SettingsPatch{Account: &account}); err != nil {
		t.Errorf("a valid account: %v", err)
	}

	if err := svc.Shutdown(); err != nil {
		t.Fatal(err)
	}
	seen.wait(t, 0, "stopped")
	select {
	case <-quit:
	case <-time.After(5 * time.Second):
		t.Error("the application was not asked to quit after the shutdown the page requested")
	}
	// Asked again once the engine has stopped: there is nothing to wait for, and it is not an
	// error the page would show as a failed engine.
	if err := svc.Shutdown(); err != nil {
		t.Errorf("Shutdown after the engine stopped: %v", err)
	}
	if err := svc.Unregister(); err == nil || err.Error() != "engine_failed" {
		t.Errorf("a command after the engine stopped: %v, want engine_failed", err)
	}
}

// The page is told whether the desktop prefers dark when it subscribes and at every change: the
// System skin follows that, whatever the webview makes of the desktop.
func TestThePageIsToldTheDesktopsPreference(t *testing.T) {
	dir := t.TempDir()
	core := app.Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	phone, err := app.OpenPhone(core, app.PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	var seen page
	dark := true
	svc := &App{phone: phone, emit: seen.receive, quit: func() {}, dark: func() bool { return dark }}
	svc.desktopChanged() // nobody listens yet
	if early := seen.all(); len(early) != 0 {
		t.Fatalf("events before the page subscribed: %+v", early)
	}
	if err := svc.Subscribe(); err != nil {
		t.Fatal(err)
	}
	dark = false
	svc.desktopChanged()
	var told []bool
	for _, ev := range seen.all() {
		if ev.Type == "desktop" {
			told = append(told, *ev.Dark)
		}
	}
	if len(told) != 2 || !told[0] || told[1] {
		t.Errorf("what the page was told of the desktop: %v, want dark then light", told)
	}
	if got, _ := json.Marshal(UIEvent{Type: "desktop", Dark: &dark}); string(got) != `{"type":"desktop","dark":false}` {
		t.Errorf("the desktop event: %s", got)
	}
}

// Saving through the bound method over a configuration file that cannot be read stores nothing:
// once the file is gone, no account has appeared.
func TestSavingOverAnUnreadableConfigurationStoresNothing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config", "go-microsip", "config.toml")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("schema = 1\n[account\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core := app.Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	phone, err := app.OpenPhone(core, app.PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	svc := &App{phone: phone, emit: func(UIEvent) {}, quit: func() {}}
	account := Account{Username: "1001", Domain: "pbx.invalid", Server: "pbx.invalid", Port: 5060, Transport: "udp", Media: "rtp", Expires: 300}
	if err := svc.SaveSettings(SettingsPatch{Account: &account}); err == nil {
		t.Fatal("an account was saved over a configuration file that cannot be read")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if s, err := svc.Settings(); err != nil || s.Account != nil || s.Theme != "dark" {
		t.Errorf("after the refused save and without the file: %+v, %v", s, err)
	}
}

// Without a phone the window still opens: every method says why there is none.
func TestWithoutAPhoneEveryMethodSaysWhy(t *testing.T) {
	svc := &App{unavailable: "go-microsip is already running"}
	_, settingsErr := svc.Settings()
	for name, err := range map[string]error{"Subscribe": svc.Subscribe(), "Dial": svc.Dial("1002"), "Settings": settingsErr, "SetDnd": svc.SetDnd(true)} {
		if err == nil || err.Error() != "go-microsip is already running" {
			t.Errorf("%s: %v", name, err)
		}
	}
	svc.Ready() // nothing waits for it
	svc.Probed("{}")
}

// Every asset is served with the content security policy: nothing inline, nothing remote, no eval.
func TestAssetsCarryTheContentSecurityPolicy(t *testing.T) {
	served := confine(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	answer := httptest.NewRecorder()
	served.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	policy := answer.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "connect-src 'self'", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(policy, directive) {
			t.Errorf("the policy lacks %q: %s", directive, policy)
		}
	}
	for _, loose := range []string{"unsafe-inline", "unsafe-eval", "*", "http:", "https:"} {
		if strings.Contains(policy, loose) {
			t.Errorf("the policy allows %q: %s", loose, policy)
		}
	}
	if answer.Body.String() != "ok" {
		t.Error("the asset itself was not served")
	}
}

// The self-test passes only when the page reports that neither an inline script nor eval ran and
// no new window opened, the guard refused the navigation, and the process listens nowhere.
func TestProbeVerdict(t *testing.T) {
	const clean = `{"inline":false,"eval":false,"opened":false}`
	if summary, err := probeVerdict(clean, true, true, 1, nil, nil); err != nil || !strings.Contains(summary, "a new window and navigation to another address refused") || !strings.Contains(summary, "no listening socket") {
		t.Errorf("a confined page: %q, %v", summary, err)
	}
	if summary, err := probeVerdict(`{"inline":false,"guarded":false,"eval":false}`, false, false, 0, nil, os.ErrInvalid); err != nil || !strings.Contains(summary, "not probed") || !strings.Contains(summary, "not checked") {
		t.Errorf("a platform without a guard: %q, %v", summary, err)
	}
	for name, err := range map[string]error{
		"an inline script ran":                         second(probeVerdict(`{"inline":true,"eval":false,"opened":false}`, true, true, 1, nil, nil)),
		"eval ran":                                     second(probeVerdict(`{"inline":false,"eval":true,"opened":false}`, true, true, 1, nil, nil)),
		"a new window opened":                          second(probeVerdict(`{"inline":false,"eval":false,"opened":true}`, true, true, 1, nil, nil)),
		"the navigation went on":                       second(probeVerdict(clean, true, true, 0, nil, nil)),
		"a listening socket":                           second(probeVerdict(clean, true, true, 1, []string{"00000000:1F90"}, nil)),
		"no report from the page":                      second(probeVerdict("", true, true, 1, nil, nil)),
		"a guard that is expected and not installed":   second(probeVerdict(`{"inline":false,"guarded":false,"eval":false}`, true, false, 0, nil, nil)),
		"a report that is not JSON":                    second(probeVerdict("<html>", true, true, 1, nil, nil)),
		"a report without the page's own answers":      second(probeVerdict(`{"inline":false}`, true, true, 1, nil, nil)),
		"a guarded page that did not try a new window": second(probeVerdict(`{"inline":false,"eval":false}`, true, true, 1, nil, nil)),
		"an unguarded page without the eval answer":    second(probeVerdict(`{"inline":false,"guarded":false}`, false, false, 0, nil, nil)),
	} {
		if err == nil {
			t.Errorf("%s: the self-test passed", name)
		}
	}
}

func second(_ string, err error) error { return err }
