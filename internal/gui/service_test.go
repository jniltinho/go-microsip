package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/control"
	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/store"
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
			domain.RegistrationChanged{State: domain.RegistrationState{Status: domain.Registered}, Account: "1001@pbx.invalid"},
			`{"type":"registration","registration":{"state":"registered","account":"1001@pbx.invalid"}}`,
		},
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

// The close button without a tray, and the tray's Quit: the page that listens is asked, and quits
// for itself (it asks first during a call). Asking again is never consent; only a page that is
// not there, or that did not answer the last request in time, leaves the question to the window.
func TestLeavingAsksThePageThatListens(t *testing.T) {
	none := &App{unavailable: "no phone", emit: func(ev UIEvent) { t.Errorf("sent to no page: %+v", ev) }}
	if got := none.leave(); got != nothingToEnd {
		t.Fatalf("without an engine leaving found %d, want nothing to end", got)
	}
	quits := 0
	none.quit = func() { quits++ }
	none.end()
	if quits != 1 {
		t.Fatalf("without a phone ending quit %d times, want at once", quits)
	}

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
	// An engine and no page yet: the window has to ask by itself.
	if got := svc.leave(); got != pageSilent {
		t.Fatalf("before a page listened leaving found %d, want a silent page", got)
	}
	if err := svc.Subscribe(); err != nil {
		t.Fatal(err)
	}
	asked := func() (n int) {
		for _, ev := range seen.all() {
			if ev.Type == "quit" {
				n++
			}
		}
		return n
	}
	// The page answers, the person cancels, and the window is closed again at once, several
	// times: the page is asked each time and nothing quits.
	for n := 1; n <= 4; n++ {
		if got := svc.leave(); got != pageAsked {
			t.Fatalf("request %d: the page that answers was not asked (%d)", n, got)
		}
		if got := asked(); got != n {
			t.Fatalf("request %d: the page was asked %d times", n, got)
		}
		svc.QuitAsked()
	}
	// A request the page has not answered yet is the same request, asked once more, until the
	// time it has to answer is over; then the window has to ask by itself.
	if svc.leave() != pageAsked || svc.leave() != pageAsked {
		t.Fatal("a page was taken as not answering before it had the time to")
	}
	svc.mu.Lock()
	svc.asked = time.Now().Add(-2 * quitGrace)
	svc.mu.Unlock()
	if got := svc.leave(); got != pageSilent {
		t.Fatalf("a page that did not answer in time: leaving found %d, want a silent page", got)
	}
	select {
	case <-quit:
		t.Fatal("the application quit although nothing asked the engine to stop")
	case <-time.After(300 * time.Millisecond):
	}
	// Ending is one shutdown however often it is asked, and the application quits when the
	// engine has stopped.
	svc.end()
	if err := svc.Shutdown(); err != nil {
		t.Fatalf("a second shutdown while the first is on its way: %v", err)
	}
	svc.end()
	select {
	case <-quit:
	case <-time.After(15 * time.Second):
		t.Fatal("the application did not quit after the engine was asked to stop")
	}
	if err := svc.Shutdown(); err != nil { // the engine has stopped: quitting again is quitting
		t.Fatalf("a shutdown after the engine stopped: %v", err)
	}
	if got := svc.leave(); got != nothingToEnd {
		t.Fatalf("after the engine stopped leaving found %d, want nothing to end", got)
	}
}

// A wish to quit is never lost to the order things arrive in. The engine's events reach the window
// after the fact: here the engine has already ended when the quit is asked, and its last events
// come later, or never.
func TestAQuitOutlivesALateStopped(t *testing.T) {
	ended := app.Refused(domain.NoticeEngineFailed)
	for _, c := range []struct {
		name    string
		last    []domain.Event // what still arrives before the stream closes
		refusal error
		quits   bool
	}{
		{"Stopped arrives after the quit was asked", []domain.Event{domain.Stopped{}}, ended, true},
		{"the stream closes without Stopped", nil, ended, true},
		{"a full queue is a refusal, and a later stop is not a quit", []domain.Event{domain.Stopped{}}, app.Refused(domain.NoticeOverloaded), false},
	} {
		stream := make(chan domain.Event)
		quit := make(chan struct{})
		var once sync.Once
		svc := &App{
			emit: func(UIEvent) {}, quit: func() { once.Do(func() { close(quit) }) },
			sending: func(domain.Command) error { return c.refusal }, stream: stream,
		}
		done := make(chan struct{})
		go func() { defer close(done); svc.forward() }()
		err := svc.Shutdown()
		if c.quits && err != nil {
			t.Errorf("%s: the quit was refused: %v", c.name, err)
		}
		if !c.quits && err == nil {
			t.Errorf("%s: the refusal was not reported", c.name)
		}
		select {
		case <-quit:
			t.Errorf("%s: the application quit before the engine's last events were through", c.name)
		case <-time.After(100 * time.Millisecond):
		}
		for _, ev := range c.last {
			stream <- ev
		}
		close(stream)
		<-done
		select {
		case <-quit:
			if !c.quits {
				t.Errorf("%s: the application quit although the quit had been refused", c.name)
			}
		case <-time.After(time.Second):
			if c.quits {
				t.Errorf("%s: the application did not quit", c.name)
			}
		}
	}
}

// The page's Quit when it shows no call is conditional, and the engine decides: a call the page
// has not heard of yet refuses it, nothing quits, and the page is told so it can ask.
func TestAnUnaskedQuitIsTheEnginesToDecide(t *testing.T) {
	busy := app.Refused(domain.NoticeBusy)
	var sent []domain.Command
	stream := make(chan domain.Event)
	quits := make(chan struct{}, 4)
	inCall := true
	svc := &App{
		emit: func(UIEvent) {}, quit: func() { quits <- struct{}{} }, stream: stream,
		sending: func(c domain.Command) error {
			sent = append(sent, c)
			if stop, ok := c.(domain.Shutdown); ok && stop.IfIdle && inCall {
				return busy
			}
			return nil
		},
	}
	done := make(chan struct{})
	go func() { defer close(done); svc.forward() }()
	if err := svc.Quit(); err != busy {
		t.Fatalf("an unasked quit during a call the page has not seen: %v, want busy", err)
	}
	if stop, ok := sent[0].(domain.Shutdown); !ok || !stop.IfIdle {
		t.Fatalf("the unasked quit sent %+v, want a conditional shutdown", sent[0])
	}
	// Refused: a later stop of the engine is not a quit.
	select {
	case <-quits:
		t.Fatal("the application quit after its conditional quit was refused")
	case <-time.After(100 * time.Millisecond):
	}
	// The person's yes is unconditional.
	if err := svc.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if stop, ok := sent[len(sent)-1].(domain.Shutdown); !ok || stop.IfIdle {
		t.Fatalf("the confirmed quit sent %+v, want an unconditional shutdown", sent[len(sent)-1])
	}
	stream <- domain.Stopped{}
	close(stream)
	<-done
	select {
	case <-quits:
	case <-time.After(time.Second):
		t.Fatal("the application did not quit after the confirmed shutdown")
	}
}

// noticeBoard is a ports.CallNotice that records what it is asked.
type noticeBoard struct {
	mu      sync.Mutex
	asked   []string
	actions chan ports.CallNoticeAction
}

func (n *noticeBoard) Incoming(call uint64, caller string) {
	n.mu.Lock()
	n.asked = append(n.asked, fmt.Sprintf("incoming %d %s", call, caller))
	n.mu.Unlock()
}

func (n *noticeBoard) Close(call uint64) {
	n.mu.Lock()
	n.asked = append(n.asked, fmt.Sprintf("close %d", call))
	n.mu.Unlock()
}

func (n *noticeBoard) Actions() <-chan ports.CallNoticeAction { return n.actions }

func (n *noticeBoard) all() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.asked...)
}

// A call that rings here gets a notification, which goes when the call is answered, refused or
// over; its buttons answer and refuse that call. A call placed from here gets none.
func TestARingingCallIsNotifiedAndItsButtonsDecide(t *testing.T) {
	board := &noticeBoard{actions: make(chan ports.CallNoticeAction)}
	stream := make(chan domain.Event)
	var mu sync.Mutex
	var sent []domain.Command
	shown := 0
	svc := &App{
		emit: func(UIEvent) {}, quit: func() {}, stream: stream, notice: board,
		attention: func() { mu.Lock(); shown++; mu.Unlock() },
		sending: func(c domain.Command) error {
			mu.Lock()
			sent = append(sent, c)
			mu.Unlock()
			return nil
		},
	}
	done := make(chan struct{})
	go func() { defer close(done); svc.forward() }()
	decided := make(chan struct{})
	go func() { defer close(decided); svc.decide(board.actions) }()
	call := func(id domain.CallID, direction domain.Direction, phase domain.PhaseKind, version uint64) {
		stream <- domain.CallChanged{Call: domain.CallSnapshot{ID: id, Direction: direction, Remote: "1002", Phase: domain.CallPhase{Kind: phase}, Version: version}}
	}
	call(1, domain.Outgoing, domain.Ringing, 1) // placed from here: nothing to notify
	call(1, domain.Outgoing, domain.Ended, 2)
	call(2, domain.Incoming, domain.Ringing, 1)
	call(2, domain.Incoming, domain.Ringing, 2) // the same call again: one notification
	board.actions <- ports.CallNoticeAction{Call: 2, Answer: true}
	call(2, domain.Incoming, domain.Established, 3)
	call(2, domain.Incoming, domain.Ended, 4)
	call(3, domain.Incoming, domain.Ringing, 1)
	board.actions <- ports.CallNoticeAction{Call: 3}
	call(3, domain.Incoming, domain.Ended, 2)
	close(stream)
	<-done
	close(board.actions)
	<-decided // every button was acted on
	if got, want := board.all(), []string{"incoming 2 1002", "close 2", "incoming 3 1002", "close 3"}; !slices.Equal(got, want) {
		t.Errorf("the notifications asked for: %v, want %v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 || sent[0] != (domain.Answer{Call: 2}) || sent[1] != (domain.Reject{Call: 3}) {
		t.Errorf("the buttons sent %+v, want Answer of call 2 and Reject of call 3", sent)
	}
	if shown < 2 { // once for each call that rang; the answered one again
		t.Errorf("the window was brought in sight %d times", shown)
	}
}

// A sip: or tel: link reaches the page as a question: the number, checked again, waits for a page
// that listens, and nothing is dialed by the window.
func TestALinkIsHandedToThePageToAsk(t *testing.T) {
	dir := t.TempDir()
	core := app.Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	phone, err := app.OpenPhone(core, app.PhoneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	var seen page
	svc := &App{phone: phone, emit: seen.receive, quit: func() {}}
	go svc.forward()
	links := func() (numbers []string) {
		for _, ev := range seen.all() {
			if ev.Type == "link" {
				numbers = append(numbers, ev.Number)
			}
			if ev.Type == "call" {
				t.Errorf("a link placed a call by itself: %+v", ev.Call)
			}
		}
		return numbers
	}
	svc.link("1001")
	svc.link("1002;rm") // not a dial target: dropped
	svc.link("1002")    // the last one before a page listens is the one asked
	if got := links(); len(got) != 0 {
		t.Fatalf("links sent before a page listened: %v", got)
	}
	if err := svc.Subscribe(); err != nil {
		t.Fatal(err)
	}
	svc.link("$(reboot)")
	svc.link(" 1003 ")
	time.Sleep(200 * time.Millisecond) // a call placed by mistake would have shown by now
	if got := links(); !slices.Equal(got, []string{"1002", "1003"}) {
		t.Fatalf("links the page was asked about: %v, want 1002 then 1003", got)
	}
	if raw, _ := json.Marshal(UIEvent{Type: "link", Number: "1003"}); string(raw) != `{"type":"link","number":"1003"}` {
		t.Errorf("the link event is %s", raw)
	}
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

// agentPhone is the softphone as agent control sees it: a real database, and the commands it is
// sent written down.
type agentPhone struct {
	db   *store.Store
	mu   sync.Mutex
	sent []domain.Command
}

func (p *agentPhone) Send(c domain.Command) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, c)
	return nil
}
func (p *agentPhone) SetDND(bool) error      { return nil }
func (p *agentPhone) Settings() app.Settings { return app.Settings{} }
func (p *agentPhone) ContactsUpTo(search string, limit int) ([]app.Contact, error) {
	return p.db.SearchContacts(search, limit)
}
func (p *agentPhone) HistoryUpTo(filter app.HistoryFilter, search string, limit int) ([]app.HistoryEntry, error) {
	return p.db.RecentCalls(limit, filter, search)
}
func (p *agentPhone) AudioDevices() (inputs, outputs []ports.Device, err error) { return nil, nil, nil }
func (p *agentPhone) Ledger() (app.Ledger, error)                               { return p.db, nil }

// An agent's request for a call waits for the person at the window: the page is told, the window
// comes in sight, nothing is sent until the yes, and a no sends nothing.
func TestAnAgentRequestWaitsForThePersonAtTheWindow(t *testing.T) {
	db, err := store.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	phone := &agentPhone{db: db}
	var mu sync.Mutex
	var events []UIEvent
	shown := 0
	svc := &App{
		emit: func(ev UIEvent) { mu.Lock(); events = append(events, ev); mu.Unlock() }, quit: func() {},
		attention: func() { mu.Lock(); shown++; mu.Unlock() }, subscribed: true,
	}
	owner, err := control.NewOwner(phone, svc.approvalsChanged)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	svc.owner = owner
	last := func() []Approval {
		mu.Lock()
		defer mu.Unlock()
		if len(events) == 0 || events[len(events)-1].Type != "approvals" {
			t.Fatalf("the page was not told of the requests: %+v", events)
		}
		return *events[len(events)-1].Approvals
	}
	ask := func(key, target string) control.Response {
		return owner.Handle(control.Request{SchemaVersion: 1, RequestID: "r", IdempotencyKey: &key,
			Operation: control.Operation{Op: control.CallDial, Target: target}}, 77)
	}
	before := time.Now()
	first := ask("k1", "1002")
	if first.Status != control.StatusApprovalRequired || first.OperationID == nil {
		t.Fatalf("the agent's request: %+v", first)
	}
	waiting := last()
	if len(waiting) != 1 || waiting[0].ID != *first.OperationID || waiting[0].Description != "call 1002" || !strings.Contains(waiting[0].Requester, "77") ||
		waiting[0].Deadline < before.Add(59*time.Second).UnixMilli() || waiting[0].Deadline > time.Now().Add(61*time.Second).UnixMilli() {
		t.Errorf("what the page was told: %+v", waiting)
	}
	if wire, _ := json.Marshal(UIEvent{Type: "approvals", Approvals: &waiting}); !strings.HasPrefix(string(wire), `{"type":"approvals","approvals":[{"id":"op-1-`) ||
		!strings.Contains(string(wire), `"requester":"A local program (process 77)","description":"call 1002","deadline":`) {
		t.Errorf("the event as the page reads it: %s", wire)
	}
	if shown != 1 || len(phone.sent) != 0 {
		t.Errorf("the window was brought in sight %d times; %d commands sent before the yes", shown, len(phone.sent))
	}
	// A page that subscribes now is told what waits: Subscribe sends this list.
	if list := *svc.approvals(); len(list) != 1 || list[0].ID != *first.OperationID {
		t.Errorf("what a page that subscribes is told: %+v", list)
	}
	if err := svc.Approve(*first.OperationID); err != nil {
		t.Fatal(err)
	}
	if dial, ok := phone.sent[0].(domain.Dial); len(phone.sent) != 1 || !ok || dial.Target.Value() != "1002" || len(last()) != 0 {
		t.Errorf("after the yes: sent %v, %d waiting", phone.sent, len(last()))
	}
	second := ask("k2", "1003")
	if err := svc.Deny(*second.OperationID); err != nil || len(last()) != 0 || len(phone.sent) != 1 {
		t.Errorf("after a no: %v, %d waiting, %d sent", err, len(last()), len(phone.sent))
	}
	ask("k3", "1004")
	ask("k4", "1005")
	if err := svc.DenyAll(); err != nil || len(last()) != 0 || len(phone.sent) != 1 {
		t.Errorf("after deny all: %v, %d waiting, %d sent", err, len(last()), len(phone.sent))
	}
	if paused := ask("k5", "1006"); paused.Error == nil || paused.Error.Code != control.Busy {
		t.Errorf("a request after deny all: %+v", paused)
	}
	// Do-not-disturb changed by an agent, with the person's yes, is told to the page.
	controlled, err := control.NewOwner(controlledPhone{Phone: phone, told: svc.dndChanged}, svc.approvalsChanged)
	if err != nil {
		t.Fatal(err)
	}
	defer controlled.Close()
	on := "k-dnd"
	asked := controlled.Handle(control.Request{SchemaVersion: 1, RequestID: "r", IdempotencyKey: &on, Operation: control.Operation{Op: control.DNDSet, Enabled: true}}, 0)
	if asked.OperationID == nil {
		t.Fatalf("do-not-disturb asked for: %+v", asked)
	}
	controlled.Decide(*asked.OperationID, true)
	mu.Lock()
	told := false
	for _, ev := range events {
		told = told || (ev.Type == "dnd" && ev.DND != nil && *ev.DND)
	}
	mu.Unlock()
	if wire, _ := json.Marshal(UIEvent{Type: "dnd", DND: &told}); !told || string(wire) != `{"type":"dnd","dnd":true}` {
		t.Errorf("the page was not told that do-not-disturb is on (as %s)", wire)
	}
	// Without agent control there is nothing to decide, and the page is told of an empty list.
	bare := &App{emit: func(UIEvent) {}}
	if bare.Approve("x") == nil || bare.Deny("x") == nil || bare.DenyAll() == nil || len(*bare.approvals()) != 0 {
		t.Error("a window without agent control decided something")
	}
}

// A page that subscribes while agent control tells it of a change an agent made: neither waits
// for the lock the other holds.
func TestAPageSubscribesWhileAnAgentChangesDoNotDisturb(t *testing.T) {
	db, err := store.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &App{phone: &app.Phone{}, emit: func(UIEvent) {}, quit: func() {}}
	owner, err := control.NewOwner(controlledPhone{Phone: &agentPhone{db: db}, told: svc.dndChanged}, svc.approvalsChanged)
	if err != nil {
		t.Fatal(err)
	}
	svc.owner = owner
	done := make(chan struct{}, 2)
	go func() {
		for range 300 {
			_ = svc.Subscribe()
		}
		done <- struct{}{}
	}()
	go func() {
		for i := range 300 {
			key := fmt.Sprintf("k-%d", i)
			asked := owner.Handle(control.Request{SchemaVersion: 1, RequestID: "r", IdempotencyKey: &key,
				Operation: control.Operation{Op: control.DNDSet, Enabled: i%2 == 0}}, 0)
			if asked.OperationID != nil {
				owner.Decide(*asked.OperationID, true)
			}
		}
		done <- struct{}{}
	}()
	for range 2 {
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("the page and agent control wait for each other") // the owner is left as it is: closing it would wait too
		}
	}
	owner.Close()
}

// A grant made in the window is a private file whose capability runs what it covers without a
// prompt; the list, the revocation and the audit trail are what the page shows.
func TestAGrantMadeInTheWindowIsAPrivateFile(t *testing.T) {
	db, err := store.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	phone := &agentPhone{db: db}
	dir := t.TempDir()
	svc := &App{emit: func(UIEvent) {}, grantDir: filepath.Join(dir, "run", "grants"), bootstrapDir: filepath.Join(dir, "config", "bootstrap")}
	owner, err := control.NewOwner(phone, svc.approvalsChanged)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	svc.owner = owner
	spec := GrantSpec{Scopes: []string{"control:dial"}, Destinations: []string{"1002"}, Minutes: 10, Quota: 1}
	path, err := svc.CreateGrant(spec, false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 || filepath.Dir(path) != svc.grantDir || !strings.HasSuffix(path, ".grant") {
		t.Fatalf("the grant file %s: %v", path, err)
	}
	capability, err := control.ReadCapability(path, own{})
	if err != nil {
		t.Fatal(err)
	}
	key := "g1"
	granted := owner.Handle(control.Request{SchemaVersion: 1, RequestID: "r", IdempotencyKey: &key, Capability: &capability,
		Operation: control.Operation{Op: control.CallDial, Target: "1002"}}, 0)
	if granted.Status != control.StatusAccepted || len(phone.sent) != 1 || len(*svc.approvals()) != 0 {
		t.Errorf("a call with the grant's file: %+v (%+v), %d sent", granted, granted.Error, len(phone.sent))
	}
	grants, err := svc.Grants()
	if err != nil || len(grants) != 1 || grants[0].Bootstrap || grants[0].Used != 1 || grants[0].Quota != 1 || grants[0].Expires < time.Now().UnixMilli() ||
		!slices.Equal(grants[0].Destinations, []string{"1002"}) || grants[0].Incoming == nil {
		t.Fatalf("the grants the page lists: %+v, %v", grants, err)
	}
	if wire, _ := json.Marshal(grants); strings.Contains(string(wire), capability) || !strings.Contains(string(wire), `"incoming":[]`) {
		t.Errorf("the list as the page reads it: %s", wire)
	}
	// The export for the headless service: a file beside the configuration, listed until used.
	boot, err := svc.CreateGrant(GrantSpec{Scopes: []string{"control:dnd"}, Minutes: 5}, true)
	if err != nil || filepath.Dir(boot) != svc.bootstrapDir || !strings.HasSuffix(boot, ".json") {
		t.Fatalf("the bootstrap file %s: %v", boot, err)
	}
	if id, _, err := control.ReadBootstrap(boot, own{}); err != nil || !strings.HasPrefix(id, "boot-") {
		t.Errorf("the bootstrap file's content: %q, %v", id, err)
	}
	if both, _ := svc.Grants(); len(both) != 2 || both[0].Bootstrap == both[1].Bootstrap {
		t.Errorf("the grants after the export: %+v", both)
	}
	// What cannot be granted is said, and leaves no file.
	if _, err := svc.CreateGrant(GrantSpec{Scopes: []string{"control:shell"}, Minutes: 5}, false); err == nil {
		t.Error("an unknown permission was granted")
	}
	if _, err := svc.CreateGrant(GrantSpec{Scopes: []string{"control:dial"}, Destinations: []string{"10 02"}, Minutes: 5}, false); err == nil {
		t.Error("a destination that is no number was granted")
	}
	if files, _ := os.ReadDir(svc.grantDir); len(files) != 1 {
		t.Errorf("%d files in the grants directory, want the one grant", len(files))
	}
	if err := svc.RevokeGrant(grants[0].ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := svc.Grants(); len(after) != 1 || !after[0].Bootstrap {
		t.Errorf("the grants after the session's one was revoked: %+v", after)
	}
	log, err := svc.AuditLog()
	if err != nil || len(log) < 5 || log[0].Decision != "revoked" || log[0].At < time.Now().Add(-time.Minute).UnixMilli() {
		t.Errorf("the audit trail: %+v, %v", log, err)
	}
	for _, entry := range log {
		if strings.Contains(entry.Detail, capability) || strings.Contains(entry.Detail, "1002") {
			t.Errorf("the audit trail holds a capability or a number: %+v", entry)
		}
	}
	if err := svc.ClearAudit(); err != nil {
		t.Fatal(err)
	}
	if log, _ := svc.AuditLog(); len(log) != 0 {
		t.Errorf("the audit trail after it was cleared: %+v", log)
	}
	// Without a private directory, or without agent control, nothing is granted.
	svc.grantDir = ""
	if _, err := svc.CreateGrant(spec, false); err == nil {
		t.Error("a grant was made with nowhere to write its file")
	}
	bare := &App{emit: func(UIEvent) {}}
	if _, err := bare.CreateGrant(spec, false); err == nil {
		t.Error("a window without agent control made a grant")
	}
	if _, err := bare.Grants(); err == nil || bare.RevokeGrant("x") == nil || bare.ClearAudit() == nil {
		t.Error("a window without agent control listed or revoked grants")
	}
	if _, err := bare.AuditLog(); err == nil {
		t.Error("a window without agent control has an audit trail")
	}
}

// Every backend method the page calls (frontend/src/api.ts) is a method of the service, and the
// grant the page sends is read field for field.
func TestThePageCallsMethodsTheServiceHas(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "api.ts"))
	if err != nil {
		t.Fatal(err)
	}
	service := reflect.TypeOf(&App{})
	names := map[string]bool{}
	for _, pattern := range []string{`call(?:<[^>]*>)?\("([A-Z][A-Za-z]*)"`, `return \["([A-Z][A-Za-z]*)"`} {
		for _, found := range regexp.MustCompile(pattern).FindAllStringSubmatch(string(source), -1) {
			names[found[1]] = true
		}
	}
	if len(names) < 30 {
		t.Fatalf("only %d backend methods found in api.ts: the patterns no longer match it", len(names))
	}
	for name := range names {
		if _, has := service.MethodByName(name); !has {
			t.Errorf("the page calls %s, which the service does not have", name)
		}
	}
	for _, name := range []string{"Grants", "CreateGrant", "RevokeGrant", "AuditLog", "ClearAudit", "Approve", "Deny", "DenyAll"} {
		if !names[name] {
			t.Errorf("the page does not call %s", name)
		}
	}
	var spec GrantSpec
	if err := json.Unmarshal([]byte(`{"scopes":["control:dial"],"destinations":["1002"],"incoming":["1003"],"minutes":10,"quota":2}`), &spec); err != nil ||
		len(spec.Scopes) != 1 || spec.Destinations[0] != "1002" || spec.Incoming[0] != "1003" || spec.Minutes != 10 || spec.Quota != 2 {
		t.Errorf("the grant the page sends is read as %+v, %v", spec, err)
	}
	if wire, _ := json.Marshal(AuditEntry{At: 1, Operation: "o", Decision: "d", Detail: "x"}); string(wire) != `{"at":1,"operation":"o","decision":"d","detail":"x"}` {
		t.Errorf("an audit line as the page reads it: %s", wire)
	}
	if wire, _ := json.Marshal(Grant{ID: "g", Scopes: []string{}, Destinations: []string{}, Incoming: []string{}}); string(wire) !=
		`{"id":"g","scopes":[],"destinations":[],"incoming":[],"expires":0,"quota":0,"used":0,"bootstrap":false}` {
		t.Errorf("a grant as the page reads it: %s", wire)
	}
}
