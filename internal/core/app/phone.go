package app

import (
	"errors"
	"log/slog"
	"sync"
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

	mu       sync.Mutex
	password domain.Secret // typed in this session; never stored
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
	cfg, err := config.Resolve(p.context())
	var ca []byte
	if err == nil && cfg.Config.CAFile != "" {
		if ca, err = config.ReadCAFile(cfg.Config.CAFile, o.Files); err != nil {
			o.warn("the CA file was not read: " + err.Error())
			cfg = nil // a TLS account must not register without its CA bundle
		}
	}
	options := engine.Options{Audio: p.audio, CA: ca, UserAgent: r.UserAgent, Log: r.Log}
	if cfg != nil {
		options.Input, options.Output = cfg.Config.Audio.InputDevice, cfg.Config.Audio.OutputDevice
	}
	p.engine = engine.Start(options)
	if cfg != nil {
		if cfg.Config.UI.DND {
			p.engine.Send(domain.SetDND{On: true})
		}
		if cfg.PasswordSource != "" {
			p.engine.Send(domain.Register{Settings: cfg.Config.Account, Password: cfg.Password})
		}
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
			p.journal.record(ev)
			journaled <- ev
		}
	}()
	return p, nil
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

// Register registers the account. A nil password means the one at hand: typed earlier in this
// session, or configured. A typed password is kept for the session only.
func (p *Phone) Register(account domain.AccountSettings, password *string) error {
	if err := account.Validate(); err != nil {
		return err
	}
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
	if err := p.Send(domain.Register{Settings: account, Password: secret}); err != nil {
		return err
	}
	p.mu.Lock()
	p.password = secret
	p.mu.Unlock()
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
	draft, _, _, _ := config.Draft(c)
	s := Settings{
		Theme: string(draft.UI.Theme), DND: draft.UI.DND,
		AudioInput: draft.Audio.InputDevice, AudioOutput: draft.Audio.OutputDevice,
	}
	if draft.Account.Username != "" && draft.Account.Validate() == nil {
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
	if err != nil || len(edits) == 0 {
		return err
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
func (p *Phone) Contacts(search string) ([]Contact, error) {
	s, err := p.storage()
	if err != nil {
		return nil, err
	}
	return s.SearchContacts(search, listLimit)
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
	s, err := p.storage()
	if err != nil {
		return nil, err
	}
	return s.RecentCalls(listLimit, filter, search)
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
