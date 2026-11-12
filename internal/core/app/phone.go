package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/engine"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// PhoneOptions is what a Phone needs besides Options.
type PhoneOptions struct {
	// Audio opens the sound devices; nil means this build has no sound service.
	Audio ports.AudioDevices
	// UserAgent is the product name sent to the PBX.
	UserAgent string
	// Log receives diagnostics; nil discards them.
	Log *slog.Logger
	// Held opens the phone without telling the engine to register: nothing reaches the PBX
	// until Begin. The headless service checks its grant in between.
	Held bool
}

// Settings is the saved configuration as a window shows it. It never holds a password.
type Settings struct {
	// Account is nil until a valid account is configured.
	Account *domain.AccountSettings
	// HasPassword: a password for the account is at hand, typed in this session or configured.
	HasPassword bool
	Theme       string
	// AudioInput and AudioOutput are device IDs; empty is the system default.
	AudioInput, AudioOutput string
	DND                     bool
}

// SettingsPatch is an edit of the settings: nil fields stay as they are.
type SettingsPatch struct {
	Account                        *domain.AccountSettings
	Theme, AudioInput, AudioOutput *string
}

// The stored records a front end shows, under the names of this package: front ends do not import
// the store.
type (
	Contact       = store.Contact
	ContactNumber = store.ContactNumber
	NewContact    = store.NewContact
	HistoryEntry  = store.HistoryEntry
	HistoryFilter = store.HistoryFilter
)

// History filters.
const (
	AllCalls      = store.AllCalls
	IncomingCalls = store.IncomingCalls
	OutgoingCalls = store.OutgoingCalls
	MissedCalls   = store.MissedCalls
)

// listLimit bounds a contact or history listing handed to a window.
const listLimit = 1000

// Refused is a command the engine did not take. Its text is the notice's name, which is what a
// front end shows.
type Refused domain.Notice

func (r Refused) Error() string { return domain.Notice(r).String() }

// ErrRestartForCA means the configured CA bundle is not the one the running engine was started
// with: a TLS account registers only after a restart.
var ErrRestartForCA = errors.New("the CA bundle of the configuration changed: restart go-microsip to register over TLS")

// ErrNoStorage means the database could not be opened: settings, contacts and history are not
// available in this session.
var ErrNoStorage = errors.New("the database is not available")

// Phone is the softphone a window drives: the engine with the configured account, the history it
// journals, the contacts and the settings. One Phone holds the instance lock of its database until
// Close.
type Phone struct {
	o       Options
	audio   ports.AudioDevices
	release func()
	journal *journal
	engine  *engine.Engine
	events  <-chan domain.Event
	pumped  chan struct{}
	closing sync.Once

	// caFile is the CA bundle the engine was started with ("" for none): the engine keeps it for
	// its whole life.
	caFile string
	// input and output are the sound devices the engine was started with ("" for the system's
	// default): it keeps them for its whole life.
	input, output string
	// inCall: the engine has a call. It keeps its account for the length of it.
	inCall atomic.Bool

	// registering makes a registration one whole act: the command, the session's password and
	// the keyring's are those of the same request, in the order the requests came.
	registering sync.Mutex

	mu       sync.Mutex
	password domain.Secret    // typed in this session or read from the keyring; never written to a file
	held     []domain.Command // what Begin tells the engine; nil once told
}

// OpenPhone starts the engine and, when an account and its password are configured, registers it.
// A configuration that is incomplete or invalid is not an error here: the window opens on its
// settings. It fails with ErrRunning when another go-microsip uses the same database.
func OpenPhone(o Options, r PhoneOptions) (*Phone, error) {
	release, err := o.exclusive()
	if err != nil {
		return nil, err
	}
	p := &Phone{o: o, audio: r.Audio, release: release, pumped: make(chan struct{})}
	if p.audio == nil {
		p.audio = noAudio{}
	}
	p.journal = openJournal(o, StorePath(o.base().Env))
	cfg, _ := config.Resolve(p.context())
	// The password the user typed in an earlier session is in the keyring, when there is one; a
	// password the configuration provides (the environment, a private file) comes first.
	if cfg != nil && cfg.PasswordSource == "" {
		p.password = p.kept()
	}
	draft, _, loaded, _ := config.Draft(p.context())
	options := engine.Options{Audio: p.audio, UserAgent: r.UserAgent, Log: r.Log}
	// The CA bundle is read whether or not an account is complete yet: the window may complete a
	// TLS account later in this session, and it must then be checked against the bundle that is
	// configured, never against the system's roots. A bundle that cannot be used is told to the
	// engine, which then refuses a TLS registration as a certificate failure.
	switch {
	case !loaded:
		options.Untrusted = errors.New("the configuration could not be read, so its CA bundle is unknown")
	case draft.CAFile != "":
		p.caFile = draft.CAFile
		if options.CA, options.Untrusted = config.ReadCAFile(draft.CAFile, o.Files); options.Untrusted != nil {
			o.warn("the CA file was not read: " + options.Untrusted.Error())
		}
	}
	// The sound devices are a preference of their own, like do-not-disturb: they are the saved
	// ones whether or not an account is configured yet. A window that shows a headset selected
	// must not place the first call of a new account on the system's default devices.
	if loaded {
		p.input, p.output = draft.Audio.InputDevice, draft.Audio.OutputDevice
	}
	options.Input, options.Output = p.input, p.output
	p.engine = engine.Start(options)
	p.held = startup(draft, loaded, cfg, p.password)
	if !r.Held {
		p.Begin()
	}
	// Every event is journaled before a reader sees it, and a reader that is behind keeps neither
	// the engine nor the history waiting: the relay merges what a later event replaced and never
	// drops a call's final snapshot or Stopped.
	journaled := make(chan domain.Event)
	p.events = engine.Relay(journaled)
	go func() {
		defer close(p.pumped)
		defer close(journaled)
		for ev := range p.engine.Events() {
			if c, ok := ev.(domain.CallChanged); ok {
				p.inCall.Store(!c.Call.Phase.IsFinal())
			}
			p.journal.record(ev)
			journaled <- ev
		}
	}()
	return p, nil
}

// Begin tells the engine what a phone that opens tells it: do-not-disturb as saved, and the
// registration of the configured account. OpenPhone does it itself, unless the phone was opened
// Held; a second call does nothing.
func (p *Phone) Begin() {
	p.mu.Lock()
	commands := p.held
	p.held = nil
	p.mu.Unlock()
	for _, c := range commands {
		p.engine.Send(c)
	}
}

// startup is what the engine is told when a phone opens. Do-not-disturb is a preference of its
// own: it is applied whenever the settings could be read, whether or not an account is configured
// yet, so a window that shows it on never lets a call ring. The account registers only from a
// configuration that resolves and has a password: the configured one, or else the one kept in the
// keyring.
func startup(draft config.Stored, loaded bool, cfg *config.Effective, kept domain.Secret) []domain.Command {
	var commands []domain.Command
	if loaded && draft.UI.DND {
		commands = append(commands, domain.SetDND{On: true})
	}
	switch {
	case cfg == nil:
	case cfg.PasswordSource != "":
		commands = append(commands, domain.Register{Settings: cfg.Config.Account, Password: cfg.Password})
	case !kept.Empty():
		commands = append(commands, domain.Register{Settings: cfg.Config.Account, Password: kept})
	}
	return commands
}

// kept reads the account's password from the keyring, under the reference the database holds. A
// keyring that is locked, missing or slow is not an error: the password is then asked for.
func (p *Phone) kept() domain.Secret {
	s := p.journal.store
	if s == nil || p.o.Secrets == nil {
		return domain.Secret{}
	}
	ref, err := s.SecretRef()
	if err != nil || ref == "" {
		return domain.Secret{}
	}
	password, ok, err := p.o.Secrets.Get(ref)
	if err != nil {
		p.o.warn("the password was not read from the keyring: " + err.Error())
	}
	if err != nil || !ok {
		return domain.Secret{}
	}
	return domain.NewSecret(password)
}

// keep stores a typed password in the keyring and its reference in the database, so the account
// registers by itself at the next start. The password goes nowhere else. Without a keyring, or
// with one that does not take it, the password lives for this session, and the warning says so.
//
// Every password gets a reference of its own, and the one before is removed once the database
// names the new one. A write the keyring finishes late, after it was given up on, then lands on
// an entry nothing names, never on a password typed after it.
func (p *Phone) keep(secret domain.Secret) {
	s := p.journal.store
	if s == nil || p.o.Secrets == nil {
		return
	}
	before, err := s.SecretRef()
	if err != nil {
		p.o.warn("the password is kept for this session only: " + err.Error())
		return
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		p.o.warn("the password is kept for this session only: " + err.Error())
		return
	}
	ref := "go-microsip/" + hex.EncodeToString(id) // not a secret, and says nothing of the account
	switch err := p.o.Secrets.Set(ref, secret.Reveal()); {
	case errors.Is(err, ports.ErrSecretsTimeout):
		// The request may have reached the keyring. Its reference is recorded all the same: the
		// next start then finds the password if the keyring took it, and asks if it did not.
		p.o.warn("the keyring did not answer in time: whether it kept the password is not known; it is used for this session")
	case err != nil:
		p.o.warn("the password is kept for this session only: " + err.Error())
		return
	}
	if err := s.SetSecretRef(ref); err != nil {
		p.o.warn("the password is kept for this session only: " + err.Error())
		_ = p.o.Secrets.Delete(ref) // nothing names it
		return
	}
	if before == "" {
		return
	}
	if err := p.o.Secrets.Delete(before); err != nil {
		p.o.warn("the password used before was not removed from the keyring: " + err.Error())
	}
}

// context is the resolution context with the settings saved in this session's database.
func (p *Phone) context() config.Context {
	c := p.o.base()
	if s := p.journal.store; s != nil {
		values, err := s.LoadConfig()
		if err != nil {
			p.o.warn("settings database not read: " + err.Error())
		}
		c.SQLite = config.SQLiteLayer(values)
	}
	return c
}

func (p *Phone) storage() (*store.Store, error) {
	if p.journal.store == nil {
		return nil, ErrNoStorage
	}
	return p.journal.store, nil
}

// Events delivers the engine's events in order; the channel closes after domain.Stopped. Calls are
// already in the history when their snapshots arrive here.
func (p *Phone) Events() <-chan domain.Event { return p.events }

// Send queues a command for the engine. A nil error means queued, not done: what the engine makes
// of it arrives as events. The error is a Refused: overloaded, or engine_failed once it stopped.
func (p *Phone) Send(c domain.Command) error {
	if p.engine.Send(c) {
		return nil
	}
	select {
	case <-p.engine.Done():
		return Refused(domain.NoticeEngineFailed)
	default:
		return Refused(domain.NoticeOverloaded)
	}
}

// ShutdownIfIdle stops the engine only when it has no call, as the engine itself knows it at that
// moment; a window's own view of calls comes later, so it cannot decide this. With a call in
// progress nothing changes and the answer is the engine's "busy": the window then asks the person.
func (p *Phone) ShutdownIfIdle() error {
	taken := make(chan bool, 1)
	if err := p.Send(domain.Shutdown{IfIdle: true, Taken: taken}); err != nil {
		return err
	}
	select {
	case stopping := <-taken:
		if !stopping {
			return Refused(domain.NoticeBusy)
		}
	case <-p.engine.Done(): // it ended meanwhile: stopped is what was asked for
	}
	return nil
}

// Register registers the account. A nil password means the one at hand: typed earlier, kept in
// the keyring, or configured. A typed password is kept in the keyring when there is one, and
// otherwise for the session only.
func (p *Phone) Register(account domain.AccountSettings, password *string) error {
	if err := account.Validate(); err != nil {
		return err
	}
	if p.inCall.Load() {
		return Refused(domain.NoticeBusy) // the engine would refuse it: nothing changes meanwhile
	}
	// One at a time, to the end: a second request waits, so the password it leaves in the keyring
	// is the one of the registration that came last, not of whichever write finished last.
	p.registering.Lock()
	defer p.registering.Unlock()
	p.mu.Lock()
	secret := p.password
	p.mu.Unlock()
	switch {
	case password != nil:
		secret = domain.NewSecret(*password)
	case secret.Empty():
		if cfg, err := config.Resolve(p.context()); err == nil {
			secret = cfg.Password
		}
	}
	if secret.Empty() {
		return ErrNoPassword
	}
	if account.Transport == domain.TLS {
		// The engine checks the server against the bundle it started with. A configuration that
		// does not resolve says nothing reliable of its bundle (an invalid ca_file reads as none),
		// and another bundle than the engine's would mean trusting the wrong roots.
		draft, _, loaded, err := config.Draft(p.context())
		switch {
		case err != nil:
			return err
		case !loaded || draft.CAFile != p.caFile:
			return ErrRestartForCA
		}
	}
	if err := p.Send(domain.Register{Settings: account, Password: secret}); err != nil {
		return err
	}
	p.mu.Lock()
	p.password = secret
	p.mu.Unlock()
	if password != nil {
		p.keep(secret) // a typed password: remembered for the next start, in the keyring only
	}
	return nil
}

// SetDND turns do-not-disturb on or off and remembers it for the next start.
func (p *Phone) SetDND(on bool) error {
	if err := p.Send(domain.SetDND{On: on}); err != nil {
		return err
	}
	if s := p.journal.store; s != nil {
		value := "false"
		if on {
			value = "true"
		}
		if err := s.SaveConfig(map[string]*string{config.DND.Key(): &value}); err != nil {
			p.o.warn("do-not-disturb not saved: " + err.Error())
		}
	}
	return nil
}

// Settings reads the saved configuration.
func (p *Phone) Settings() Settings {
	c := p.context()
	draft, _, _, draftErr := config.Draft(c)
	s := Settings{
		Theme: string(draft.UI.Theme), DND: draft.UI.DND,
		AudioInput: draft.Audio.InputDevice, AudioOutput: draft.Audio.OutputDevice,
	}
	// An account is offered as configured only when the whole configuration resolves: a draft
	// with an error holds defaults in place of what could not be read.
	if draftErr == nil {
		s.Account = &draft.Account
	}
	p.mu.Lock()
	s.HasPassword = !p.password.Empty()
	p.mu.Unlock()
	if !s.HasPassword {
		cfg, err := config.Resolve(c)
		s.HasPassword = err == nil && cfg.PasswordSource != ""
	}
	return s
}

// SaveSettings stores an edit in the database. A field that the configuration file, the
// environment or a flag holds cannot be changed here: the error names it. While a configuration
// source cannot be read at all, nothing is saved. The audio devices and the
// CA bundle of a running engine are those it started with; a change applies at the next start.
func (p *Phone) SaveSettings(patch SettingsPatch) error {
	s, err := p.storage()
	if err != nil {
		return err
	}
	if patch.Account != nil && p.inCall.Load() {
		// The engine keeps its account for the length of a call: saving another one now would
		// leave the window showing an account the engine does not use.
		return Refused(domain.NoticeBusy)
	}
	before, sources, loaded, draftErr := config.Draft(p.context())
	if !loaded {
		// A source could not be read: what it holds is unknown, so nothing is saved over it. The
		// error names the file; the window shows it.
		return draftErr
	}
	edits := map[string]*string{}
	if patch.Account != nil {
		after := before
		after.Account = *patch.Account
		// A field that a file, the environment or a flag holds is refused when the edit changes
		// it, whether or not the rest of the configuration is complete yet.
		if edits, err = config.SQLiteEdits(sources, &before, after); err != nil {
			return err
		}
		if draftErr != nil {
			// Nothing valid is stored yet: every field of the account is written, so a stored
			// value that could not be read is replaced too. Fields a higher source holds are
			// skipped; the check above has just shown that the edit agrees with them.
			if edits, err = config.SQLiteEdits(sources, nil, after); err != nil {
				return err
			}
		}
	}
	set := func(f config.Field, value *string, old string) error {
		if value == nil || *value == old {
			return nil
		}
		if (f == config.InputDevice || f == config.OutputDevice) && !config.ValidDevice(*value) {
			return &config.Error{Kind: config.Invalid, Field: f, Origin: config.SQLite}
		}
		if source, held := sources[f]; held && source != config.SQLite && source != config.Default {
			return &config.Error{Kind: config.Overridden, Field: f, Origin: source}
		}
		edits[f.Key()] = value
		return nil
	}
	if patch.Theme != nil {
		theme := string(config.ParseTheme(*patch.Theme))
		patch.Theme = &theme
	}
	err = errors.Join(
		set(config.UITheme, patch.Theme, string(before.UI.Theme)),
		set(config.InputDevice, patch.AudioInput, before.Audio.InputDevice),
		set(config.OutputDevice, patch.AudioOutput, before.Audio.OutputDevice),
	)
	if err != nil {
		return err
	}
	if patch.Account != nil {
		// An account is saved only if the configuration resolves with it, also when nothing is
		// left to write because a file holds every field. Otherwise a value a
		// file or the environment holds and that cannot be read would stay in the way: the edit
		// would look saved, register from a default, and fail at the next start.
		c := p.context()
		if c.SQLite == nil {
			c.SQLite = map[config.Field]string{}
		}
		for key, value := range edits {
			if f, ok := config.FieldFromKey(key); !ok {
				continue
			} else if value == nil {
				delete(c.SQLite, f)
			} else {
				c.SQLite[f] = *value
			}
		}
		if _, _, _, err := config.Draft(c); err != nil {
			return err
		}
	}
	if len(edits) == 0 {
		return nil
	}
	return s.SaveConfig(edits)
}

// AudioDevices lists the capture and playback devices.
func (p *Phone) AudioDevices() (inputs, outputs []ports.Device, err error) {
	if inputs, err = p.audio.Inputs(); err != nil {
		return nil, nil, err
	}
	outputs, err = p.audio.Outputs()
	return inputs, outputs, err
}

// Contacts lists the contacts whose name or number contains search, favorites first.
func (p *Phone) Contacts(search string) ([]Contact, error) { return p.ContactsUpTo(search, listLimit) }

// ContactsUpTo is Contacts with the caller's own bound: agent control pages through all of them.
func (p *Phone) ContactsUpTo(search string, limit int) ([]Contact, error) {
	s, err := p.storage()
	if err != nil {
		return nil, err
	}
	return s.SearchContacts(search, limit)
}

// SaveContact adds a contact when id is 0 and replaces the one with that id otherwise.
func (p *Phone) SaveContact(id int64, c NewContact) (Contact, error) {
	s, err := p.storage()
	if err != nil {
		return Contact{}, err
	}
	if id == 0 {
		id, err = s.AddContact(c)
	} else {
		err = s.UpdateContact(id, c)
	}
	if err != nil {
		return Contact{}, err
	}
	saved, _, err := s.Contact(id)
	return saved, err
}

// DeleteContact deletes a contact.
func (p *Phone) DeleteContact(id int64) error {
	s, err := p.storage()
	if err != nil {
		return err
	}
	return s.DeleteContact(id)
}

// History lists the calls, newest first, whose remote party contains search.
func (p *Phone) History(filter HistoryFilter, search string) ([]HistoryEntry, error) {
	return p.HistoryUpTo(filter, search, listLimit)
}

// HistoryUpTo is History with the caller's own bound: agent control pages through all of it.
func (p *Phone) HistoryUpTo(filter HistoryFilter, search string, limit int) ([]HistoryEntry, error) {
	s, err := p.storage()
	if err != nil {
		return nil, err
	}
	return s.RecentCalls(limit, filter, search)
}

// TrayCalls is how many of the latest calls a tray menu lists, as in the Rust candidate.
const TrayCalls = 5

// RecentCall is one of the latest calls as a short list shows it.
type RecentCall struct {
	// Label is plain text: "↗ who" for a call placed, "↙ who" for one received, "Missed: who"
	// for one missed, where who is the name of a contact that has the number, or the number.
	Label string
	// Number is what calling it again dials.
	Number string
}

// Recent lists the newest calls that can be called again, newest first, at most limit. A call
// whose other party is not something to dial (an anonymous caller, say) is left out. Without a
// database there are none.
func (p *Phone) Recent(limit int) []RecentCall {
	s := p.journal.store
	if s == nil || limit <= 0 {
		return nil
	}
	calls, err := s.RecentCalls(4*limit, store.AllCalls, "")
	if err != nil {
		return nil
	}
	var recent []RecentCall
	for _, entry := range calls {
		call := entry.Call
		if _, err := domain.ParseDialTarget(call.Remote); err != nil {
			continue
		}
		who := call.Remote
		// shortcut: one contact search per listed call (five at most); index the numbers if the
		// list grows.
		if contacts, err := s.SearchContacts(call.Remote, listLimit); err == nil {
		named:
			for _, c := range contacts {
				for _, n := range c.Numbers {
					if n.Number == call.Remote {
						who = c.Name
						break named
					}
				}
			}
		}
		recent = append(recent, RecentCall{Label: recentLabel(call.Direction, call.Outcome, who), Number: call.Remote})
		if len(recent) == limit {
			break
		}
	}
	return recent
}

func recentLabel(direction domain.Direction, outcome store.Outcome, who string) string {
	switch {
	case outcome == store.Missed:
		return "Missed: " + who
	case direction == domain.Outgoing:
		return "\u2197 " + who
	}
	return "\u2199 " + who
}

// DeleteCall deletes one history entry.
func (p *Phone) DeleteCall(id int64) error {
	s, err := p.storage()
	if err != nil {
		return err
	}
	return s.DeleteCall(id)
}

// ClearHistory deletes every history entry.
func (p *Phone) ClearHistory() error {
	s, err := p.storage()
	if err != nil {
		return err
	}
	_, err = s.ClearHistory()
	return err
}

// Close ends every call, unregisters, stops the engine within its bound, closes the history and
// frees the instance lock. It is safe to call more than once.
func (p *Phone) Close() {
	p.closing.Do(func() {
		for !p.engine.Send(domain.Shutdown{}) {
			select {
			case <-p.engine.Done():
			case <-time.After(10 * time.Millisecond): // the command queue is full: it drains
				continue
			}
			break
		}
		<-p.pumped // every event is journaled
		p.journal.close()
		p.release()
	})
}
