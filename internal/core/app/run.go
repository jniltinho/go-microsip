package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/config"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/engine"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/store"
)

// RunOptions is what the softphone without a window needs besides Options.
type RunOptions struct {
	// Audio opens the sound devices; nil means this build has no sound service.
	Audio ports.AudioDevices
	// Call is dialed once the account is registered; empty waits for calls.
	Call string
	// HangupAfter ends the call that Call started this long after it is answered; 0 leaves the end
	// to the other side or to the context.
	HangupAfter time.Duration
	// UserAgent is the product name sent to the PBX.
	UserAgent string
	// Log receives diagnostics; nil discards them.
	Log *slog.Logger
}

// ErrNoPassword means no source provides the password and nothing can ask for it here.
var ErrNoPassword = errors.New("no password is available: set account.password_env or a private account.password_file")

// ErrRunning means another go-microsip of this user holds the instance lock.
var ErrRunning = errors.New("go-microsip is already running")

// ErrRegistration and ErrCall are how a Run with a call to place fails.
var (
	ErrRegistration = errors.New("the account did not register")
	ErrCall         = errors.New("the call was not answered")
)

// noAudio is the sound of a build without a sound service.
type noAudio struct{}

func (noAudio) Inputs() ([]ports.Device, error)         { return nil, ports.ErrNoAudioService }
func (noAudio) Outputs() ([]ports.Device, error)        { return nil, ports.ErrNoAudioService }
func (noAudio) Open(_, _ string) (ports.MediaIO, error) { return nil, ports.ErrNoAudioService }

// Run is the softphone without a window: it registers the configured account, journals calls in
// the history, optionally places one call, and reports every engine event. It returns when ctx
// ends, when the call it placed has ended, or when the account cannot register for a reason only
// the user can fix. The engine is always shut down first: calls end and the account unregisters
// within the engine's bound.
func Run(ctx context.Context, o Options, r RunOptions, report func(domain.Event)) error {
	// One softphone per database: two would fight over the account and the sound, and the second
	// would close the first one's journaled calls as interrupted. The lock and the recovery of an
	// interrupted import come before anything opens the database, and the lock is released only
	// after the history is closed.
	release, err := o.exclusive()
	if err != nil {
		return err
	}
	defer release()
	c := o.context()
	cfg, err := config.Resolve(c)
	if err != nil {
		return err
	}
	if cfg.PasswordSource == "" {
		return ErrNoPassword
	}
	var ca []byte
	if cfg.Config.CAFile != "" {
		if ca, err = config.ReadCAFile(cfg.Config.CAFile, o.Files); err != nil {
			return err
		}
	}
	var target domain.DialTarget
	if r.Call != "" {
		if target, err = domain.ParseDialTarget(r.Call); err != nil {
			return fmt.Errorf("the number to call is not valid: %w", err)
		}
	}
	if r.Audio == nil {
		r.Audio = noAudio{}
	}

	j := openJournal(o, StorePath(c.Env))
	defer j.close()
	e := engine.Start(engine.Options{
		Audio: r.Audio, Input: cfg.Config.Audio.InputDevice, Output: cfg.Config.Audio.OutputDevice,
		CA: ca, UserAgent: r.UserAgent, Log: r.Log,
	})
	if cfg.Config.UI.DND {
		e.Send(domain.SetDND{On: true})
	}
	e.Send(domain.Register{Settings: cfg.Config.Account, Password: cfg.Password})

	// The end of ctx stops the engine whatever else is going on.
	shutdown := func() {
		for !e.Send(domain.Shutdown{}) {
			select {
			case <-e.Done():
				return
			case <-time.After(10 * time.Millisecond): // the command queue is full: it drains
			}
		}
	}
	go func() {
		select {
		case <-ctx.Done():
			shutdown()
		case <-e.Done():
		}
	}()

	// Reporting has its own goroutine and its own queue: a reader of the events that has stalled
	// keeps neither the call, nor the history, nor the lock. What it has not taken by the end is
	// not reported.
	reports := make(chan domain.Event, reportQueue)
	reported := make(chan struct{})
	go func() {
		defer close(reported)
		for ev := range reports {
			report(ev)
		}
	}()
	unreported := 0

	var (
		result   error
		dialed   bool
		answered bool
		hangup   <-chan time.Time
		ours     domain.CallID
	)
	stop := func(err error) {
		// Being told to stop is not a failure, whatever state the call or the account is in.
		if result == nil && ctx.Err() == nil {
			result = err
		}
		shutdown()
	}
	for open := true; open; {
		var ev domain.Event
		select {
		case <-hangup:
			e.Send(domain.Hangup{Call: ours})
			continue
		case ev, open = <-e.Events():
			if !open {
				continue
			}
		}
		j.record(ev)
		select {
		case reports <- ev:
		default:
			unreported++
		}
		switch ev := ev.(type) {
		case domain.RegistrationChanged:
			switch s := ev.State; {
			case s.Status == domain.Registered && r.Call != "" && !dialed:
				dialed = true
				e.Send(domain.Dial{Target: target})
			case s.Status == domain.RegistrationFailed && (r.Call != "" && !dialed || s.Failure == domain.Unauthorized || s.Failure == domain.Certificate):
				stop(fmt.Errorf("%w: %v", ErrRegistration, s))
			}
		case domain.CallChanged:
			if !dialed || ev.Call.Direction != domain.Outgoing || ours != 0 && ev.Call.ID != ours {
				break
			}
			ours = ev.Call.ID
			switch {
			case ev.Call.Phase.Kind == domain.Established && !answered:
				answered = true
				if r.HangupAfter > 0 {
					hangup = time.After(r.HangupAfter)
				}
			case ev.Call.Phase.IsFinal() && answered:
				stop(nil)
			case ev.Call.Phase.IsFinal():
				stop(fmt.Errorf("%w: %v", ErrCall, ev.Call.Phase))
			}
		case domain.Noticed:
			if dialed && ours == 0 { // the Dial itself was refused
				stop(fmt.Errorf("%w: %v", ErrCall, ev.Notice))
			}
		}
	}
	// The engine has stopped: calls are ended, the account is unregistered, the devices are closed,
	// and every call is in the history. The reader gets a moment to take what is left.
	close(reports)
	select {
	case <-reported:
	case <-time.After(outputGrace):
		unreported += len(reports)
	}
	if unreported > 0 {
		o.warn(fmt.Sprintf("%d events were not reported: their reader has stalled", unreported))
	}
	return result
}

// reportQueue is how many events may wait for their reader.
const reportQueue = 1024

// outputGrace is how long the events of a stopped engine may take to be reported.
const outputGrace = 2 * time.Second

// journal writes the calls of one run to the history. Without a database it does nothing.
type journal struct {
	store   *store.Store
	session string
	warn    func(string)
	calls   map[domain.CallID]*journaled
}

type journaled struct {
	started  time.Time
	answered time.Time
}

func openJournal(o Options, path string) *journal {
	j := &journal{warn: o.warn, calls: map[domain.CallID]*journaled{}}
	if path == "" {
		o.warn("history storage unavailable: " + ErrNoDataDirectory.Error())
		return j
	}
	s, err := store.Open(path)
	if err != nil {
		// The file is left as it is; this session keeps no history.
		o.warn("history storage unavailable: " + err.Error())
		return j
	}
	// Calls a killed process left open are closed before any new call begins.
	if _, err := s.RecoverInterrupted(); err != nil {
		o.warn("history recovery failed: " + err.Error())
	}
	if _, err := s.PruneOlderThan(store.DefaultRetentionDays); err != nil {
		o.warn("history retention failed: " + err.Error())
	}
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	j.store, j.session = s, hex.EncodeToString(id)
	return j
}

func (j *journal) close() {
	if j.store != nil {
		_ = j.store.Close()
	}
}

// record journals a call when it first appears and completes it with its final snapshot.
func (j *journal) record(ev domain.Event) {
	changed, ok := ev.(domain.CallChanged)
	if !ok || j.store == nil {
		return
	}
	c := changed.Call
	key := fmt.Sprintf("%s:%d", j.session, c.ID)
	now := time.Now()
	call := j.calls[c.ID]
	var err error
	if call == nil {
		call = &journaled{started: now}
		j.calls[c.ID] = call
		_, err = j.store.BeginCall(store.CallStart{
			SessionKey: key, StartedUTC: now.Unix(), Direction: c.Direction, Remote: c.Remote, Secure: c.Secure,
		})
	}
	if call.answered.IsZero() && (c.Phase.Kind == domain.Established || c.Phase.Kind == domain.Held) {
		call.answered = now
	}
	if c.Phase.IsFinal() {
		record := store.CallRecord{
			SessionKey: key, StartedUTC: call.started.Unix(), Direction: c.Direction, Remote: c.Remote, Secure: c.Secure,
			Outcome: store.OutcomeFromEnd(c.Direction, !call.answered.IsZero(), c.Phase.End),
		}
		ended := now.Unix()
		record.EndedUTC = &ended
		if !call.answered.IsZero() {
			at, lasted := call.answered.Unix(), now.Sub(call.answered).Milliseconds()
			record.AnsweredUTC, record.DurationMS = &at, &lasted
		}
		delete(j.calls, c.ID)
		_, ferr := j.store.FinishCall(record)
		err = errors.Join(err, ferr)
	}
	if err != nil {
		j.warn("history not written: " + err.Error())
	}
}
