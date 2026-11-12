package gui

import (
	"errors"
	"sync"
	"time"

	"github.com/jniltinho/go-microsip/internal/buildinfo"
	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
)

// EventName is the one event the page listens to; its data is a UIEvent.
const EventName = "go-microsip:event"

// App is the service bound to the page: every exported method is callable from it, and nothing
// else is. The contract is frontend/src/api.ts; JSON names are the TypeScript ones.
//
// A command method returns nil once the engine has the command; what comes of it arrives as
// events. A refusal is an error whose text is the notice's name, which is what the page shows.
type App struct {
	phone *app.Phone
	// unavailable says why there is no phone; every method but Version and Ready returns it.
	unavailable string
	emit        func(UIEvent)
	quit        func()
	// dark reports the desktop's dark preference; nil when it cannot be told.
	dark func() bool
	// attention brings the window in sight (it may be hidden in the tray); nil does nothing.
	attention func()

	ending       sync.Mutex // one Shutdown at a time
	mu           sync.Mutex
	ready        func()
	probed       func(string)
	subscribed   bool
	quitting     bool
	stopped      bool
	asked        time.Time // when the page was last asked to quit
	acked        bool      // the page answered that request
	registration domain.RegistrationChanged
	call         *domain.CallSnapshot
}

// Version is the application's version and commit, for the title and About.
func (a *App) Version() string {
	b := buildinfo.Get()
	return b.Version + " (" + b.Commit + ")"
}

// Ready is called by the page once it has drawn; --self-test goes on from there.
func (a *App) Ready() {
	a.mu.Lock()
	ready := a.ready
	a.mu.Unlock()
	if ready != nil {
		ready()
	}
}

// Probed is the page's answer to the self-test's probe event: what the probe left in the page.
// Outside a self-test it does nothing.
func (a *App) Probed(result string) {
	a.mu.Lock()
	probed := a.probed
	a.mu.Unlock()
	if probed != nil {
		probed(result)
	}
}

// engine is the phone, or the reason there is none.
func (a *App) engine() (*app.Phone, error) {
	if a.phone == nil {
		return nil, errors.New(a.unavailable)
	}
	return a.phone, nil
}

// invalid is the refusal of a command whose arguments the page should never have sent.
var invalid = app.Refused(domain.NoticeInvalidState)

func (a *App) send(c domain.Command) error {
	p, err := a.engine()
	if err != nil {
		return err
	}
	return p.Send(c)
}

// forward delivers the engine's events to the page, in order, and keeps what a page that
// subscribes later needs to catch up. It returns when the engine has stopped.
func (a *App) forward() {
	for ev := range a.phone.Events() {
		ringing := false
		a.mu.Lock()
		switch ev := ev.(type) {
		case domain.RegistrationChanged:
			a.registration = ev
		case domain.CallChanged:
			// A call that starts ringing here must be answerable: the window comes out of the tray.
			ringing = ev.Call.Direction == domain.Incoming && ev.Call.Phase.Kind == domain.Ringing &&
				(a.call == nil || a.call.ID != ev.Call.ID)
			a.call = nil
			if !ev.Call.Phase.IsFinal() {
				a.call = &ev.Call
			}
		case domain.Stopped:
			a.stopped = true
		}
		if a.subscribed {
			a.emit(uiEvent(ev))
		}
		quit := a.stopped && a.quitting
		a.mu.Unlock()
		if ringing && a.attention != nil {
			a.attention()
		}
		if quit {
			a.quit() // the page asked for the end, and the engine has ended
		}
	}
}

// Subscribe starts event delivery to the page: the registration, the call in progress and the
// pending approvals come first, then every event as it happens.
func (a *App) Subscribe() error {
	if _, err := a.engine(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emit(uiEvent(a.registration))
	if a.call != nil {
		a.emit(uiEvent(domain.CallChanged{Call: *a.call}))
	}
	a.emit(UIEvent{Type: "approvals", Approvals: &[]Approval{}}) // agent control arrives with M4
	a.emitDesktop()
	if a.stopped {
		a.emit(UIEvent{Type: "stopped"})
	}
	a.subscribed = true
	return nil
}

// emitDesktop tells the page whether the desktop prefers dark. The caller holds a.mu.
func (a *App) emitDesktop() {
	if a.dark != nil {
		dark := a.dark()
		a.emit(UIEvent{Type: "desktop", Dark: &dark})
	}
}

// desktopChanged passes a change of the desktop's light or dark preference on to the page: the
// webview does not always do it, and the System skin follows it.
func (a *App) desktopChanged() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.subscribed {
		a.emitDesktop()
	}
}

// Register registers the account; a null password means the one the backend has.
func (a *App) Register(account Account, password *string) error {
	p, err := a.engine()
	if err != nil {
		return err
	}
	settings, ok := account.settings()
	if !ok {
		return invalid
	}
	err = p.Register(settings, password)
	var refused app.Refused
	if err != nil && !errors.As(err, &refused) {
		return invalid // not valid, or no password: the page checks both before it asks
	}
	return err
}

// Unregister removes the registration.
func (a *App) Unregister() error { return a.send(domain.Unregister{}) }

// Dial places a call.
func (a *App) Dial(target string) error {
	t, err := domain.ParseDialTarget(target)
	if err != nil {
		return invalid
	}
	return a.send(domain.Dial{Target: t})
}

// Answer answers an incoming call.
func (a *App) Answer(id uint64) error { return a.send(domain.Answer{Call: domain.CallID(id)}) }

// Reject refuses an incoming call.
func (a *App) Reject(id uint64) error { return a.send(domain.Reject{Call: domain.CallID(id)}) }

// Hangup ends a call in whatever phase it is.
func (a *App) Hangup(id uint64) error { return a.send(domain.Hangup{Call: domain.CallID(id)}) }

// Hold puts a call on hold.
func (a *App) Hold(id uint64) error { return a.send(domain.Hold{Call: domain.CallID(id)}) }

// Resume takes a call off hold.
func (a *App) Resume(id uint64) error { return a.send(domain.Resume{Call: domain.CallID(id)}) }

// Dtmf sends one key: 0-9, *, # or A-D.
func (a *App) Dtmf(id uint64, key string) error {
	runes := []rune(key)
	if len(runes) != 1 {
		return invalid
	}
	k, err := domain.NewDTMFKey(runes[0])
	if err != nil {
		return invalid
	}
	return a.send(domain.SendDTMF{Call: domain.CallID(id), Key: k})
}

// Transfer hands a call over to another number.
func (a *App) Transfer(id uint64, target string) error {
	t, err := domain.ParseDialTarget(target)
	if err != nil {
		return invalid
	}
	return a.send(domain.Transfer{Call: domain.CallID(id), Target: t})
}

// SetMute mutes or unmutes the microphone of a call.
func (a *App) SetMute(id uint64, muted bool) error {
	return a.send(domain.SetMute{Call: domain.CallID(id), Muted: muted})
}

// SetDnd turns do-not-disturb on or off.
func (a *App) SetDnd(on bool) error {
	p, err := a.engine()
	if err != nil {
		return err
	}
	return p.SetDND(on)
}

// Shutdown ends the calls, unregisters and stops the engine; the application quits when it has.
// It is taken once: while one is on its way a second request is that same shutdown, and neither
// sends another command nor undoes the first.
func (a *App) Shutdown() error {
	a.ending.Lock() // a second request waits for what became of the first
	defer a.ending.Unlock()
	a.mu.Lock()
	stopped, pending := a.stopped, a.quitting
	a.quitting = true // before the command: Stopped may arrive at once
	a.mu.Unlock()
	switch {
	case stopped:
		a.quit() // the engine has already ended: there is nothing left to wait for
		return nil
	case pending:
		return nil // the engine took it: the application quits when it has stopped
	}
	err := a.send(domain.Shutdown{})
	if err != nil {
		// Not taken (the queue was full): the page may ask again, and nothing quits meanwhile.
		a.mu.Lock()
		a.quitting = false
		stopped = a.stopped
		a.mu.Unlock()
		if stopped { // it ended on its own while this was refused
			a.quit()
			return nil
		}
	}
	return err
}

// quitGrace is how long the page has to say it got a request to quit; after it the page is taken
// as not answering.
const quitGrace = 3 * time.Second

// leave is the close button on a desktop without a tray, and the tray's Quit: the page is asked to
// run its own Quit, which asks before it ends a call. Asking again is never consent: a page that
// answers is only asked again. leave reports true when the page cannot be asked, because none is
// listening or because it did not answer the last request within quitGrace; the window then has to
// ask by itself (see Run) before it ends anything.
func (a *App) leave() (unanswered bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case !a.subscribed || a.stopped:
		return true
	case !a.asked.IsZero() && !a.acked:
		if time.Since(a.asked) > quitGrace {
			return true
		}
		// The last request is still on its way: this one is the same request.
	default:
		a.asked, a.acked = time.Now(), false
	}
	a.emit(UIEvent{Type: "quit"})
	return false
}

// QuitAsked is the page's answer to a "quit" event: it is there, and its own Quit is running.
func (a *App) QuitAsked() {
	a.mu.Lock()
	a.acked = true
	a.mu.Unlock()
}

// inCall reports whether a call is in progress: ending it wants a yes from the person.
func (a *App) inCall() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.call != nil
}

// end leaves without the page: the engine ends its calls and unregisters, then the application
// quits; with no engine it quits at once. A busy engine refuses, and closing again asks again.
func (a *App) end() {
	if a.phone == nil {
		a.quit()
		return
	}
	_ = a.Shutdown()
}

// Settings returns the saved settings, never a password.
func (a *App) Settings() (Settings, error) {
	p, err := a.engine()
	if err != nil {
		return Settings{}, err
	}
	s := p.Settings()
	out := Settings{HasPassword: s.HasPassword, Theme: s.Theme, AudioInput: s.AudioInput, AudioOutput: s.AudioOutput, DND: s.DND}
	if s.Account != nil {
		account := accountOf(*s.Account)
		out.Account = &account
	}
	return out, nil
}

// SaveSettings stores the fields the patch holds.
func (a *App) SaveSettings(patch SettingsPatch) error {
	p, err := a.engine()
	if err != nil {
		return err
	}
	edit := app.SettingsPatch{Theme: patch.Theme, AudioInput: patch.AudioInput, AudioOutput: patch.AudioOutput}
	if patch.Account != nil {
		settings, ok := patch.Account.settings()
		if !ok {
			return errors.New("the account is not valid")
		}
		edit.Account = &settings
	}
	return p.SaveSettings(edit)
}

// AudioDevices lists the microphones and speakers.
func (a *App) AudioDevices() (AudioDevices, error) {
	p, err := a.engine()
	if err != nil {
		return AudioDevices{}, err
	}
	inputs, outputs, err := p.AudioDevices()
	if err != nil {
		return AudioDevices{}, err
	}
	out := AudioDevices{Inputs: []AudioDevice{}, Outputs: []AudioDevice{}}
	for _, d := range inputs {
		out.Inputs = append(out.Inputs, AudioDevice{ID: d.ID, Name: d.Name, IsDefault: d.Default})
	}
	for _, d := range outputs {
		out.Outputs = append(out.Outputs, AudioDevice{ID: d.ID, Name: d.Name, IsDefault: d.Default})
	}
	return out, nil
}

// Contacts lists the contacts whose name or number contains search, favorites first.
func (a *App) Contacts(search string) ([]Contact, error) {
	p, err := a.engine()
	if err != nil {
		return nil, err
	}
	list, err := p.Contacts(search)
	out := make([]Contact, 0, len(list))
	for _, c := range list {
		out = append(out, contactOf(c))
	}
	return out, err
}

// SaveContact adds a contact (id 0) or replaces one, and returns it as stored.
func (a *App) SaveContact(c Contact) (Contact, error) {
	p, err := a.engine()
	if err != nil {
		return Contact{}, err
	}
	record := app.NewContact{Name: c.Name, Favorite: c.Favorite}
	for _, n := range c.Numbers {
		record.Numbers = append(record.Numbers, app.ContactNumber{Number: n.Number, Label: n.Label})
	}
	saved, err := p.SaveContact(c.ID, record)
	return contactOf(saved), err
}

// DeleteContact deletes a contact.
func (a *App) DeleteContact(id int64) error {
	p, err := a.engine()
	if err != nil {
		return err
	}
	return p.DeleteContact(id)
}

// History lists the calls, newest first: filter is all, incoming, outgoing or missed.
func (a *App) History(filter, search string) ([]HistoryEntry, error) {
	p, err := a.engine()
	if err != nil {
		return nil, err
	}
	which, ok := map[string]app.HistoryFilter{
		"all": app.AllCalls, "incoming": app.IncomingCalls, "outgoing": app.OutgoingCalls, "missed": app.MissedCalls,
	}[filter]
	if !ok {
		return nil, errors.New("unknown history filter")
	}
	list, err := p.History(which, search)
	out := make([]HistoryEntry, 0, len(list))
	for _, e := range list {
		out = append(out, HistoryEntry{
			ID: e.ID, Direction: e.Call.Direction.String(), Remote: e.Call.Remote, Outcome: string(e.Call.Outcome),
			StartedUTC: e.Call.StartedUTC, DurationMS: e.Call.DurationMS,
		})
	}
	return out, err
}

// DeleteCall deletes one history entry.
func (a *App) DeleteCall(id int64) error {
	p, err := a.engine()
	if err != nil {
		return err
	}
	return p.DeleteCall(id)
}

// ClearHistory deletes the whole history.
func (a *App) ClearHistory() error {
	p, err := a.engine()
	if err != nil {
		return err
	}
	return p.ClearHistory()
}

// errNoAgentControl: requests from agents arrive with the control channel (milestone M4); until
// then there is none to decide.
var errNoAgentControl = errors.New("agent requests are not available in this build")

// Approve allows one pending agent request.
func (a *App) Approve(string) error { return errNoAgentControl }

// Deny refuses one pending agent request.
func (a *App) Deny(string) error { return errNoAgentControl }

// DenyAll refuses every pending agent request and pauses new ones.
func (a *App) DenyAll() error { return errNoAgentControl }

// Account is the SIP account as the page exchanges it. It never carries the password.
type Account struct {
	DisplayName  string `json:"displayName"`
	Username     string `json:"username"`
	AuthUsername string `json:"authUsername"`
	Domain       string `json:"domain"`
	Server       string `json:"server"`
	Port         uint16 `json:"port"`
	Transport    string `json:"transport"`
	Media        string `json:"media"`
	Expires      uint32 `json:"expires"`
}

func accountOf(s domain.AccountSettings) Account {
	return Account{
		DisplayName: s.DisplayName, Username: s.Username, AuthUsername: s.AuthUsername, Domain: s.Domain, Server: s.Server,
		Port: s.Port, Transport: s.Transport.String(), Media: s.Media.String(), Expires: s.Expires,
	}
}

// settings is the account in the core's types; ok is false for a transport or media it does not know.
func (a Account) settings() (s domain.AccountSettings, ok bool) {
	transport, okT := domain.ParseTransport(a.Transport)
	media, okM := domain.ParseMediaSecurity(a.Media)
	return domain.AccountSettings{
		DisplayName: a.DisplayName, Username: a.Username, AuthUsername: a.AuthUsername, Domain: a.Domain, Server: a.Server,
		Port: a.Port, Transport: transport, Media: media, Expires: a.Expires,
	}, okT && okM
}

// Settings is the saved configuration as the page shows it.
type Settings struct {
	Account     *Account `json:"account"`
	HasPassword bool     `json:"hasPassword"`
	Theme       string   `json:"theme"`
	AudioInput  string   `json:"audioInput"`
	AudioOutput string   `json:"audioOutput"`
	DND         bool     `json:"dnd"`
}

// SettingsPatch is an edit of the settings; absent fields stay as they are.
type SettingsPatch struct {
	Account     *Account `json:"account"`
	Theme       *string  `json:"theme"`
	AudioInput  *string  `json:"audioInput"`
	AudioOutput *string  `json:"audioOutput"`
}

// AudioDevice is one microphone or speaker.
type AudioDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// AudioDevices are the devices a call can use.
type AudioDevices struct {
	Inputs  []AudioDevice `json:"inputs"`
	Outputs []AudioDevice `json:"outputs"`
}

// ContactNumber is one number of a contact.
type ContactNumber struct {
	Number string `json:"number"`
	Label  string `json:"label"`
}

// Contact is a stored contact; ID 0 in SaveContact adds one.
type Contact struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Favorite bool            `json:"favorite"`
	Numbers  []ContactNumber `json:"numbers"`
}

func contactOf(c app.Contact) Contact {
	out := Contact{ID: c.ID, Name: c.Name, Favorite: c.Favorite, Numbers: []ContactNumber{}}
	for _, n := range c.Numbers {
		out.Numbers = append(out.Numbers, ContactNumber{Number: n.Number, Label: n.Label})
	}
	return out
}

// HistoryEntry is one call of the history.
type HistoryEntry struct {
	ID         int64  `json:"id"`
	Direction  string `json:"direction"`
	Remote     string `json:"remote"`
	Outcome    string `json:"outcome"`
	StartedUTC int64  `json:"startedUtc"`
	DurationMS *int64 `json:"durationMs,omitempty"`
}

// Registration is the registration state with the reason of a failure.
type Registration struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	Code   uint16 `json:"code,omitempty"`
	// Account is the account the engine uses, as "user@domain"; absent when it uses none.
	Account string `json:"account,omitempty"`
}

// Transfer is the progress of a blind transfer.
type Transfer struct {
	State string `json:"state"`
	Code  uint16 `json:"code,omitempty"`
}

// Call is one snapshot of a call. Remote is remote-controlled text: the page shows it as text only.
type Call struct {
	ID        uint64    `json:"id"`
	Direction string    `json:"direction"`
	Remote    string    `json:"remote"`
	Phase     string    `json:"phase"`
	EndReason string    `json:"endReason,omitempty"`
	Muted     bool      `json:"muted"`
	Secure    bool      `json:"secure"`
	Transfer  *Transfer `json:"transfer,omitempty"`
	Version   uint64    `json:"version"`
}

// Approval is an agent request waiting for the person at the window.
type Approval struct {
	ID          string `json:"id"`
	Requester   string `json:"requester"`
	Description string `json:"description"`
	Deadline    int64  `json:"deadline"`
}

// UIEvent is what the page receives: Type says which of the other fields is set.
type UIEvent struct {
	Type         string        `json:"type"`
	Registration *Registration `json:"registration,omitempty"`
	Call         *Call         `json:"call,omitempty"`
	Notice       string        `json:"notice,omitempty"`
	Approvals    *[]Approval   `json:"approvals,omitempty"`
	// Dark, of the "desktop" event: the desktop prefers a dark appearance.
	Dark *bool `json:"dark,omitempty"`
}

// uiEvent is an engine event as the page receives it.
func uiEvent(ev domain.Event) UIEvent {
	switch ev := ev.(type) {
	case domain.RegistrationChanged:
		r := Registration{State: ev.State.Status.String(), Account: ev.Account}
		if ev.State.Status == domain.RegistrationFailed {
			r.Reason = ev.State.Failure.String()
			if ev.State.Failure == domain.Rejected {
				r.Code = ev.State.Code
			}
		}
		return UIEvent{Type: "registration", Registration: &r}
	case domain.CallChanged:
		c := ev.Call
		out := Call{
			ID: uint64(c.ID), Direction: c.Direction.String(), Remote: c.Remote, Phase: c.Phase.Kind.String(),
			Muted: c.Muted, Secure: c.Secure, Version: c.Version,
		}
		if c.Phase.IsFinal() {
			out.EndReason = c.Phase.End.String()
		}
		if c.Transfer.Kind != domain.NoTransfer {
			out.Transfer = &Transfer{State: c.Transfer.Kind.String()}
			if c.Transfer.Kind == domain.TransferFailed {
				out.Transfer.Code = c.Transfer.Code
			}
		}
		return UIEvent{Type: "call", Call: &out}
	case domain.Noticed:
		return UIEvent{Type: "notice", Notice: ev.Notice.String()}
	}
	return UIEvent{Type: "stopped"}
}
