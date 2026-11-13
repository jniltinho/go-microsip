package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/sip"
)

const (
	// registerTimeout bounds one registration attempt.
	registerTimeout = 10 * time.Second
	// noAnswerTimeout ends an outgoing call nobody answered: a minute, as long as phones ring
	// before a network gives up or its voice mail answers.
	noAnswerTimeout = 60 * time.Second
	// operationTimeout bounds a hold, resume, transfer request or hang-up on the network.
	operationTimeout = 5 * time.Second
	// transferTimeout bounds the wait for a transfer's result.
	transferTimeout = 30 * time.Second
	// shutdownTimeout bounds everything Shutdown waits for.
	shutdownTimeout = 5 * time.Second
	// maxRegistrationAttempts is how often a failed registration is retried on its own.
	maxRegistrationAttempts = 8
)

// retryDelay is the bounded backoff before registration attempt n+1: 5 s doubling up to 5 minutes.
func retryDelay(attempts int) time.Duration {
	return min(5*time.Second<<min(attempts, 6), 5*time.Minute)
}

// actor is the engine's state. Only the run loop touches it.
type actor struct {
	e *Engine

	account    sip.Account
	configured bool
	// generation grows with every change of account, so a result that arrives from an earlier one
	// is discarded.
	generation int
	endpoint   *sip.Endpoint
	ctx        context.Context // ends with the account
	cancel     context.CancelFunc
	reg        domain.RegistrationState
	// identity is the account the last reported registration state was about.
	identity string
	attempts int
	retry    *time.Timer

	dnd      bool
	call     *call
	lastCall uint64
	// cleanup counts the calls whose sound devices are not closed yet: every call that ended, not
	// only the last one. A call ends, and a next one may come and go, before the devices of the
	// first are closed.
	cleanup sync.WaitGroup
}

func (e *Engine) run() {
	a := &actor{e: e}
	defer func() {
		if r := recover(); r != nil {
			// The actor is gone: say so, release what it held, and still end with Stopped.
			e.opts.Log.Error("engine failure", "panic", fmt.Sprint(r))
			a.emit(domain.Noticed{Notice: domain.NoticeEngineFailed})
			a.release()
		}
		// The actor ends here. Results that arrived meanwhile are applied now, later ones by the
		// goroutine that brings them: each finds its account or call gone and releases what it holds.
		e.mu.Lock()
		pending := e.results
		e.results, e.dead = nil, true
		e.mu.Unlock()
		e.late.Lock()
		for _, f := range pending {
			e.apply(f)
		}
		e.late.Unlock()
		a.emit(domain.Stopped{})
		close(e.stopped)
		e.events.close()
	}()
	for {
		select {
		case c := <-e.commands:
			if stop, ok := c.(domain.Shutdown); ok {
				taken := !(stop.IfIdle && a.call != nil) // decided here, where the calls are
				if stop.Taken != nil {
					select {
					case stop.Taken <- taken:
					default: // a sender that left no room does not hold the engine
					}
				}
				if !taken {
					continue
				}
				a.shutdown()
				return
			}
			a.handle(c)
		case <-e.wake:
			a.results(e.take())
		}
	}
}

// results applies a batch of results. If one of them fails, the failure goes on to end the actor,
// but not before the rest of the batch is back in the queue: the stop applies it.
func (a *actor) results(batch []func()) {
	for n, f := range batch {
		func() {
			defer func() {
				if r := recover(); r != nil {
					a.e.mu.Lock()
					a.e.results = append(batch[n+1:], a.e.results...)
					a.e.mu.Unlock()
					panic(r)
				}
			}()
			f()
		}()
	}
}

func (a *actor) emit(ev domain.Event)   { a.e.events.emit(ev) }
func (a *actor) notice(n domain.Notice) { a.emit(domain.Noticed{Notice: n}) }

func (a *actor) setRegistration(s domain.RegistrationState) {
	// The account the state is about: the one this engine holds, not what a front end saved.
	identity := ""
	if s.Status != domain.Disabled {
		identity = a.account.Settings.Username + "@" + a.account.Settings.Domain
	}
	if a.reg != s || a.identity != identity {
		a.reg, a.identity = s, identity
		a.emit(domain.RegistrationChanged{State: s, Account: identity})
	}
}

func (a *actor) handle(c domain.Command) {
	switch c := c.(type) {
	case domain.Register:
		a.register(c)
	case domain.Unregister:
		a.unregister()
	case domain.Dial:
		a.dial(c.Target)
	case domain.Answer:
		a.answer(c.Call)
	case domain.Reject:
		a.reject(c.Call)
	case domain.Hangup:
		a.hangup(c.Call)
	case domain.SendDTMF:
		a.dtmf(c.Call, c.Key)
	case domain.Hold:
		a.hold(c.Call, true)
	case domain.Resume:
		a.hold(c.Call, false)
	case domain.Transfer:
		a.transfer(c.Call, c.Target)
	case domain.SetMute:
		a.mute(c.Call, c.Muted)
	case domain.SetDND:
		a.dnd = c.On
	}
}

// --- registration ---------------------------------------------------------------------------

func (a *actor) register(c domain.Register) {
	if c.Settings.Validate() != nil {
		a.notice(domain.NoticeInvalidState)
		return
	}
	if a.call != nil {
		a.notice(domain.NoticeBusy) // the account does not change under a call
		return
	}
	a.dropAccount()
	a.account = sip.Account{Settings: c.Settings, Password: c.Password, CA: a.e.opts.CA, UserAgent: a.e.opts.UserAgent, Codecs: a.e.opts.Codecs}
	if err := a.e.opts.Untrusted; err != nil && c.Settings.Transport == domain.TLS {
		// The bundle this account must be checked against is not there: nothing is sent, and no
		// retry would change that.
		a.e.opts.Log.Warn("registration refused: the configured CA bundle could not be used", "error", err)
		a.configured = false
		a.setRegistration(domain.RegistrationState{Status: domain.RegistrationFailed, Failure: domain.Certificate})
		return
	}
	a.configured, a.attempts = true, 0
	a.connect()
}

func (a *actor) unregister() {
	if a.call != nil {
		a.notice(domain.NoticeBusy)
		return
	}
	a.dropAccount()
	a.configured = false
	a.setRegistration(domain.RegistrationState{Status: domain.Disabled})
}

// dropAccount forgets the current endpoint; its sockets close in the background.
func (a *actor) dropAccount() {
	a.generation++
	if a.retry != nil {
		a.retry.Stop()
		a.retry = nil
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if ep := a.endpoint; ep != nil {
		a.endpoint = nil
		go ep.Close()
	}
}

// connect creates the endpoint and registers it, beside the actor.
func (a *actor) connect() {
	gen, account, log := a.generation, a.account, a.e.opts.Log
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx, a.cancel = ctx, cancel
	a.setRegistration(domain.RegistrationState{Status: domain.Registering})
	go func() {
		ep, err := sip.NewEndpoint(account, log)
		if err == nil {
			if err = ep.Serve(ctx); err == nil {
				rctx, done := context.WithTimeout(ctx, registerTimeout)
				err = ep.Register(rctx)
				done()
			}
			if err != nil {
				ep.Close()
			}
		}
		a.e.post(func() {
			if gen != a.generation {
				if err == nil {
					go ep.Close()
				}
				return
			}
			if err != nil {
				a.registrationFailed(err)
				return
			}
			a.endpoint, a.attempts = ep, 0
			a.setRegistration(domain.RegistrationState{Status: domain.Registered})
			go a.incoming(ctx, ep, gen)
			go a.keepRegistered(ctx, ep, gen)
			go a.watchAddress(ctx, ep, gen)
		})
	}()
}

// addressCheck is how often a registered endpoint's local address is looked for.
const addressCheck = 3 * time.Second

// watchAddress tells the actor when the address the endpoint is bound to leaves this machine (a
// VPN that dropped, a cable pulled): the registrar cannot be reached from it any more, whatever
// the registration's lifetime says, and the account registers again when the network is back.
func (a *actor) watchAddress(ctx context.Context, ep *sip.Endpoint, gen int) {
	tick := time.NewTicker(addressCheck)
	defer tick.Stop()
	gone := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		// Told once for each time the address goes: an endpoint that a call keeps alive is
		// registered again in place, and its address may go a second time.
		now := ep.AddressGone()
		if now && !gone {
			a.e.post(func() {
				if gen == a.generation && a.reg.Status == domain.Registered {
					a.registrationFailed(sip.ErrAddressGone)
				}
			})
		}
		gone = now
	}
}

// keepRegistered renews the binding; when the registrar stops accepting it, the actor is told.
func (a *actor) keepRegistered(ctx context.Context, ep *sip.Endpoint, gen int) {
	err := ep.KeepRegistered(ctx)
	if ctx.Err() != nil {
		return
	}
	a.e.post(func() {
		if gen == a.generation {
			a.registrationFailed(err)
		}
	})
}

// registrationFailed shows the reason and schedules a retry, unless the reason needs the user:
// retrying refused credentials or an untrusted certificate changes nothing.
func (a *actor) registrationFailed(err error) {
	a.e.opts.Log.Warn("registration failed", "error", err)
	state := sip.RegistrationFailure(err)
	a.setRegistration(state)
	a.attempts++
	if state.Failure == domain.Unauthorized || state.Failure == domain.Certificate || a.attempts > maxRegistrationAttempts {
		return
	}
	gen := a.generation
	a.retry = time.AfterFunc(retryDelay(a.attempts-1), func() {
		a.e.post(func() {
			if gen != a.generation || a.reg.Status != domain.RegistrationFailed {
				return
			}
			if ep := a.endpoint; ep != nil && a.call != nil {
				a.reregister(ep, gen) // keep the endpoint: the call lives on it
				return
			}
			attempts := a.attempts
			a.dropAccount()
			a.attempts = attempts
			a.connect()
		})
	})
}

// reregister registers again on the same endpoint.
func (a *actor) reregister(ep *sip.Endpoint, gen int) {
	a.setRegistration(domain.RegistrationState{Status: domain.Registering})
	account := a.ctx
	go func() {
		ctx, done := context.WithTimeout(account, registerTimeout)
		err := ep.Register(ctx)
		done()
		a.e.post(func() {
			if gen != a.generation {
				return
			}
			if err != nil {
				a.registrationFailed(err)
				return
			}
			a.attempts = 0
			a.setRegistration(domain.RegistrationState{Status: domain.Registered})
			go a.keepRegistered(account, ep, gen)
		})
	}()
}

// incoming hands each incoming call to the actor.
func (a *actor) incoming(ctx context.Context, ep *sip.Endpoint, gen int) {
	for {
		select {
		case d := <-ep.Incoming:
			a.e.post(func() { a.ring(d, gen) })
		case <-ctx.Done():
			return
		}
	}
}

// --- calls ----------------------------------------------------------------------------------

// find returns the call with the ID, or notices why not.
func (a *actor) find(id domain.CallID) *call {
	if a.call == nil || a.call.snap.ID != id {
		a.notice(domain.NoticeUnknownCall)
		return nil
	}
	return a.call
}

func (a *actor) newCall(dir domain.Direction, remote string, phase domain.PhaseKind) *call {
	a.lastCall++
	c := &call{
		snap: domain.CallSnapshot{
			ID: domain.CallID(a.lastCall), Direction: dir, Remote: remote, Phase: domain.CallPhase{Kind: phase},
			StartedMS: time.Now().UnixMilli(),
		},
		keys: make(chan domain.DTMFKey, 32),
	}
	a.call = c
	a.publish(c)
	return c
}

// publish emits the call's snapshot with the next version.
func (a *actor) publish(c *call) {
	c.snap.Version++
	a.emit(domain.CallChanged{Call: c.snap})
}

// move changes the call's phase if the state machine allows it.
func (a *actor) move(c *call, next domain.CallPhase) bool {
	if !c.snap.Phase.CanTransitionTo(next) {
		return false
	}
	c.snap.Phase = next
	if next.Kind != domain.Ringing {
		c.quiet() // a ring or a ringback belongs to ringing
	}
	if next.Kind == domain.Established && c.snap.AnsweredMS == 0 {
		c.snap.AnsweredMS = time.Now().UnixMilli() // stays in every snapshot from here on
	}
	a.publish(c)
	return true
}

// end is the one place a call ends: its final snapshot is emitted exactly once and everything it
// held is released.
func (a *actor) end(c *call, reason domain.EndReason) {
	if c.snap.Phase.IsFinal() {
		return
	}
	c.snap.Phase = domain.EndedWith(reason)
	c.snap.EndedMS = time.Now().UnixMilli()
	c.quiet()
	a.publish(c)
	c.release()
	a.cleanup.Add(1)
	go func(released, settled chan struct{}) {
		defer a.cleanup.Done()
		<-released
		if settled != nil { // devices that were still opening when the call ended
			<-settled
		}
	}(c.released, c.settled)
	if a.call == c {
		a.call = nil
	}
}

func (a *actor) dial(target domain.DialTarget) {
	if a.call != nil {
		a.notice(domain.NoticeBusy)
		return
	}
	ep := a.endpoint
	if ep == nil || a.reg.Status != domain.Registered {
		a.notice(domain.NoticeNotRegistered)
		return
	}
	c := a.newCall(domain.Outgoing, target.Value(), domain.Dialing)
	ctx, cancel := context.WithTimeout(context.Background(), noAnswerTimeout)
	c.cancel = cancel
	go func() {
		d, med, err := ep.DialEarly(ctx, target.Value(), func(status int) {
			a.e.post(func() { a.progress(c, status) })
		}, func(med *diago.DialogMedia) {
			a.e.post(func() { a.early(c, med) })
		})
		a.e.post(func() {
			if c.snap.Phase.IsFinal() { // ended meanwhile
				if err == nil {
					go closeDialog(d, med)
				}
				return
			}
			if err != nil {
				a.e.opts.Log.Warn("outgoing call failed", "error", err) // a SIP status or a transport error: no secret
				a.end(c, dialFailure(ctx, c, err))
				return
			}
			if c.snap.Phase.Kind == domain.Terminating { // hung up as the answer arrived
				go closeDialog(d, med)
				a.end(c, domain.EndCanceled)
				return
			}
			c.dialog, c.refer = d, func(ctx context.Context, to gosip.Uri, notify func(int)) error {
				return d.ReferOptions(ctx, to, diago.ReferClientOptions{Headers: []gosip.Header{}, OnNotify: notify})
			}
			a.established(c, med)
		})
	}()
}

// dialFailure maps why an outgoing call did not connect.
func dialFailure(ctx context.Context, c *call, err error) domain.EndReason {
	var refused *sipgo.ErrDialogResponse
	switch {
	case c.snap.Phase.Kind == domain.Terminating:
		return domain.EndCanceled
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return domain.EndNoAnswer
	case errors.Is(err, sip.ErrInsecureAnswer), errors.Is(err, sip.ErrNoCommonCodec):
		return domain.EndNotAcceptable
	case errors.As(err, &refused) && refused.Res != nil:
		switch refused.Res.StatusCode {
		case 486, 600:
			return domain.EndBusy
		case 403, 603:
			return domain.EndRejected
		case 408, 480:
			return domain.EndNoAnswer
		case 415, 488, 606:
			return domain.EndNotAcceptable
		}
	}
	return domain.EndFailed
}

// ring announces an incoming call, or refuses it when the user cannot take it.
func (a *actor) ring(d *diago.DialogServerSession, gen int) {
	if gen != a.generation || a.dnd || a.call != nil {
		go func() {
			_ = d.Respond(gosip.StatusBusyHere, "Busy Here", nil)
			_ = d.Close()
		}()
		return
	}
	c := a.newCall(domain.Incoming, d.FromUser(), domain.Ringing)
	c.dialog, c.server = d, d
	c.refer = func(ctx context.Context, to gosip.Uri, notify func(int)) error {
		return d.ReferOptions(ctx, to, diago.ReferServerOptions{Headers: []gosip.Header{}, OnNotify: notify})
	}
	c.rung = make(chan struct{})
	go func() { _ = d.Ringing(); close(c.rung) }()
	a.watch(c)
	a.sound(c, ring)
}

func (a *actor) answer(id domain.CallID) {
	c := a.find(id)
	if c == nil {
		return
	}
	if c.server == nil || c.snap.Phase.Kind != domain.Ringing || c.answering {
		a.notice(domain.NoticeInvalidState)
		return
	}
	c.answering = true
	c.quiet() // the ring stops with the decision, not with the PBX's confirmation
	d := c.server
	go func() {
		<-c.rung
		med, err := d.Answer(diago.AnswerOptions{})
		a.e.post(func() {
			if c.snap.Phase.IsFinal() {
				if err == nil { // answered after the call was given up: hang the dialog up
					go closeDialog(d, med)
				}
				return
			}
			if err != nil {
				a.e.opts.Log.Warn("answer failed", "error", err)
				a.end(c, domain.EndFailed)
				return
			}
			a.established(c, med)
		})
	}()
}

func (a *actor) reject(id domain.CallID) {
	c := a.find(id)
	if c == nil {
		return
	}
	if c.server == nil || c.snap.Phase.Kind != domain.Ringing || c.answering {
		a.notice(domain.NoticeInvalidState)
		return
	}
	a.refuse(c)
}

// refuse answers a ringing incoming call 486 and ends it.
func (a *actor) refuse(c *call) {
	d := c.server
	go func() {
		<-c.rung
		_ = d.Respond(gosip.StatusBusyHere, "Busy Here", nil)
		_ = d.Close()
	}()
	a.end(c, domain.EndRejected)
}

// established starts the call's audio and watches for its end.
func (a *actor) established(c *call, med *diago.DialogMedia) {
	c.media = med
	c.snap.Secure = sip.Secure(med)
	a.move(c, domain.CallPhase{Kind: domain.Established})
	if c.snap.Direction == domain.Outgoing {
		a.watch(c)
	}
	if c.settled != nil {
		if c.earlyFailed {
			// Its audio ended on its own while it rang, and nothing here can start it again on
			// the same media: an answered call that hears nothing is ended, as it is when the
			// audio of an answered call ends. The microphone was never sent.
			a.e.opts.Log.Warn("the call is ended: its audio stopped before the answer")
			a.terminate(c, domain.EndFailed)
			return
		}
		c.private(onEarly, false) // the audio runs since the early media: from now it also sends
		return
	}
	a.openSound(c, med)
}

// early starts the audio of an outgoing call that still rings, when the other side plays
// something before the answer (an operator's ringing tone, a recorded message): the speaker
// plays it in place of the ringback, and nothing is sent until the call is answered.
func (a *actor) early(c *call, med *diago.DialogMedia) {
	if c.snap.Phase.Kind != domain.Ringing || c.media != nil {
		return // over or answered meanwhile: the dialog that ends closes its media
	}
	c.media = med
	c.private(onEarly, true)
	c.quiet()
	a.openSound(c, med)
}

// openSound opens the sound devices of a call and connects them to its media.
func (a *actor) openSound(c *call, med *diago.DialogMedia) {
	// The sound service is asked beside the actor: one that does not answer must not keep the
	// engine from taking commands, Shutdown among them.
	opts := a.e.opts
	c.settled = make(chan struct{})
	go func() {
		audio, err := opts.Audio.Open(opts.Input, opts.Output)
		if err != nil {
			close(c.settled) // nothing is open
		} else if !c.offer(audio) {
			return // the call's audio was stopped while the devices were opening
		}
		a.e.post(func() {
			over := c.released != nil || c.snap.Phase.IsFinal()
			if err == nil {
				audio := c.take()
				if audio == nil {
					return // stopAudio took the devices and closes them
				}
				if err = a.startSound(c, med, audio); err != nil {
					go audio.Close()
				}
				close(c.settled)
			}
			if err != nil && !over {
				// The call stays up without sound; the user sees why and decides.
				opts.Log.Warn("audio unavailable", "error", err)
				if errors.Is(err, ports.ErrMicrophoneDenied) {
					a.notice(domain.NoticeMicrophoneDenied) // the user can lift it: say which setting
				} else {
					a.notice(domain.NoticeAudioUnavailable)
				}
			}
		})
	}()
}

// startSound connects opened sound devices to a call's media.
func (a *actor) startSound(c *call, med *diago.DialogMedia, audio ports.MediaIO) error {
	opts := a.e.opts
	return c.startAudio(med, audio, opts.Log, func() {
		a.e.post(func() {
			opts.Log.Warn("the microphone stopped delivering")
			a.notice(domain.NoticeMicrophoneLost)
		})
	}, func(direction string, cause error) {
		a.e.post(func() { a.mediaFailed(c, direction, cause) })
	})
}

// mediaGrace is how long a dialog has to turn out ended after its audio stopped: when the other
// side hangs up, the audio may end a moment before the dialog says so.
const mediaGrace = 500 * time.Millisecond

// mediaFailed ends a call whose audio stopped on its own in one direction: a call that looks
// established and carries nothing is worse than one that ends. Audio that stops because the other
// side hung up is not a failure: the dialog ends within a moment and the call ends as it should.
func (a *actor) mediaFailed(c *call, direction string, cause error) {
	if c.snap.Phase.Kind == domain.Ringing {
		// What played before the answer stopped: the call still rings, and is ended if it is
		// answered (see established).
		a.e.opts.Log.Warn("the audio before the answer stopped", "direction", direction, "error", cause)
		c.earlyFailed = true
		return
	}
	if k := c.snap.Phase.Kind; k != domain.Established && k != domain.Held {
		return
	}
	gone := c.dialog.Context().Done()
	go func() {
		select {
		case <-gone:
			return // the dialog ended: whoever watches it ends the call
		case <-time.After(mediaGrace):
		}
		a.e.post(func() {
			if k := c.snap.Phase.Kind; k != domain.Established && k != domain.Held {
				return
			}
			a.e.opts.Log.Warn("the call's audio failed", "direction", direction, "error", cause)
			a.terminate(c, domain.EndFailed)
		})
	}()
}

// terminate hangs an answered call up and ends it for this reason, also when the other side never
// confirms.
func (a *actor) terminate(c *call, reason domain.EndReason) {
	c.ending = reason
	a.move(c, domain.CallPhase{Kind: domain.Terminating})
	d := c.dialog
	go func() {
		ctx, done := context.WithTimeout(context.Background(), operationTimeout)
		if err := d.Hangup(ctx); err != nil {
			a.e.opts.Log.Warn("hang-up was not confirmed", "error", err)
		}
		done()
		a.e.post(func() { a.end(c, reason) })
	}()
}

// watch tells the actor when the other side ends the call.
func (a *actor) watch(c *call) {
	done := c.dialog.Context().Done()
	go func() {
		<-done
		a.e.post(func() {
			switch {
			case c.snap.Phase.Kind == domain.Ringing:
				reason := domain.EndCanceled
				if ep := a.endpoint; ep != nil && c.server != nil && c.server.InviteRequest != nil {
					if id := c.server.InviteRequest.CallID(); id != nil && ep.AnsweredElsewhere(id.Value()) {
						reason = domain.EndAnsweredElsewhere // another phone of the extension took it
					}
				}
				a.end(c, reason)
			case c.snap.Phase.Kind == domain.Terminating:
				a.end(c, c.ending)
			case c.snap.Transfer.Kind == domain.TransferPending:
				// A PBX that completes a transfer ends this dialog right after its verdict: the
				// transfer's result, which is on its way, says how the call ended.
				c.gone = true
			default:
				a.end(c, domain.EndNormal)
			}
		})
	}()
}

func (a *actor) hangup(id domain.CallID) {
	c := a.find(id)
	if c == nil {
		return
	}
	switch c.snap.Phase.Kind {
	case domain.Dialing, domain.Ringing:
		if c.server != nil { // an incoming call that still rings: refuse it
			if c.answering {
				a.notice(domain.NoticeInvalidState)
				return
			}
			a.refuse(c)
			return
		}
		a.move(c, domain.CallPhase{Kind: domain.Terminating})
		c.cancel() // the dial returns canceled and ends the call
	case domain.Established, domain.Held:
		a.terminate(c, domain.EndNormal)
	default:
		a.notice(domain.NoticeInvalidState)
	}
}

func (a *actor) dtmf(id domain.CallID, key domain.DTMFKey) {
	c := a.find(id)
	if c == nil {
		return
	}
	if c.snap.Phase.Kind != domain.Established || !c.sound || !c.keyed {
		a.notice(domain.NoticeInvalidState) // also a call whose other side takes no keys
		return
	}
	select {
	case c.keys <- key:
	default:
		a.notice(domain.NoticeOverloaded)
	}
}

func (a *actor) hold(id domain.CallID, hold bool) {
	c := a.find(id)
	if c == nil {
		return
	}
	want := domain.Established
	if !hold {
		want = domain.Held
	}
	if c.snap.Phase.Kind != want || c.busy {
		a.notice(domain.NoticeInvalidState)
		return
	}
	c.busy = true
	if hold {
		c.private(onHold, true) // the microphone stops before the other side is told
	}
	d := c.dialog
	go func() {
		ctx, done := context.WithTimeout(context.Background(), operationTimeout)
		var err error
		if hold {
			err = d.Hold(ctx)
		} else {
			err = d.Unhold(ctx)
		}
		done()
		a.e.post(func() {
			c.busy = false
			if c.snap.Phase.IsFinal() || c.snap.Phase.Kind == domain.Terminating {
				return
			}
			if err != nil {
				a.e.opts.Log.Warn("hold or resume failed", "error", err)
				c.private(onHold, !hold)
				a.notice(domain.NoticeInvalidState)
				return
			}
			c.private(onHold, hold)
			if hold {
				a.move(c, domain.CallPhase{Kind: domain.Held})
			} else {
				a.move(c, domain.CallPhase{Kind: domain.Established})
			}
		})
	}()
}

func (a *actor) mute(id domain.CallID, muted bool) {
	c := a.find(id)
	if c == nil {
		return
	}
	if k := c.snap.Phase.Kind; k != domain.Established && k != domain.Held {
		a.notice(domain.NoticeInvalidState)
		return
	}
	if c.snap.Muted != muted {
		c.snap.Muted = muted
		c.private(onMute, muted)
		a.publish(c)
	}
}

// transfer asks the other side to call the target instead (blind transfer) and follows the result.
func (a *actor) transfer(id domain.CallID, target domain.DialTarget) {
	c := a.find(id)
	if c == nil {
		return
	}
	k := c.snap.Phase.Kind
	if (k != domain.Established && k != domain.Held) || c.busy || c.snap.Transfer.Kind == domain.TransferPending || a.endpoint == nil {
		a.notice(domain.NoticeInvalidState)
		return
	}
	s := a.endpoint.Account.Settings
	to := gosip.Uri{User: target.Value(), Host: s.Server, Port: int(s.Port)}
	c.snap.Transfer = domain.TransferState{Kind: domain.TransferPending}
	a.publish(c)
	// Only the verdict matters: progress reports may be many, the final status is kept apart from
	// them so it can never be crowded out.
	var verdict atomic.Int32
	arrived := make(chan struct{}, 1)
	refer, gone := c.refer, c.dialog.Context().Done()
	go func() {
		ctx, done := context.WithTimeout(context.Background(), operationTimeout)
		err := refer(ctx, to, func(code int) {
			if code >= 200 {
				verdict.Store(int32(code))
				select {
				case arrived <- struct{}{}:
				default:
				}
			}
		})
		done()
		code := 500
		if err == nil {
			select {
			case <-arrived:
			case <-gone: // the dialog ended: a verdict sent before that is here, or about to be
				select {
				case <-arrived:
				case <-time.After(time.Second):
				}
			case <-time.After(transferTimeout):
			}
			if code = int(verdict.Load()); code == 0 {
				code = 408
			}
		}
		a.e.post(func() {
			if c.snap.Phase.IsFinal() {
				return
			}
			if code != 200 {
				c.snap.Transfer = domain.TransferState{Kind: domain.TransferFailed, Code: uint16(code)}
				if c.gone { // the other side hung up meanwhile
					a.end(c, domain.EndNormal)
					return
				}
				a.publish(c) // the call goes on
				return
			}
			c.snap.Transfer = domain.TransferState{Kind: domain.TransferSucceeded}
			if c.gone {
				a.end(c, domain.EndTransferred)
				return
			}
			a.publish(c)
			if c.snap.Phase.Kind != domain.Terminating {
				a.terminate(c, domain.EndTransferred)
			}
		})
	}()
}

// --- shutdown -------------------------------------------------------------------------------

// within runs f beside the actor and waits for it until the deadline, no longer.
func within(deadline time.Time, f func()) {
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Until(deadline)):
	}
}

// shutdown ends the call, unregisters, closes the sound devices and returns within
// shutdownTimeout, whatever the network or the sound service do. The time is shared out so that
// no step can use up another's: the call's goodbye, then the unregistration, then the devices.
func (a *actor) shutdown() {
	start := time.Now()
	callBy, accountBy, deadline := start.Add(shutdownTimeout*2/5), start.Add(shutdownTimeout*4/5), start.Add(shutdownTimeout)
	if c := a.call; c != nil {
		reason := domain.EndNormal
		c.stopAudio() // local and at once: the microphone stops before anything waits for the network
		switch {
		case c.cancel != nil && c.dialog == nil: // still dialing
			c.cancel()
			reason = domain.EndCanceled
		case c.server != nil && c.snap.Phase.Kind == domain.Ringing && c.answering:
			// The 200 is on its way and is the only final response: the answered dialog is hung
			// up when the answer reports back.
		case c.server != nil && c.snap.Phase.Kind == domain.Ringing:
			d, rung := c.server, c.rung
			within(callBy, func() {
				<-rung
				_ = d.Respond(gosip.StatusBusyHere, "Busy Here", nil)
			})
			reason = domain.EndRejected
		case c.dialog != nil:
			d := c.dialog
			within(callBy, func() {
				ctx, done := context.WithDeadline(context.Background(), callBy)
				_ = d.Hangup(ctx)
				done()
			})
		}
		a.end(c, reason)
	}
	a.generation++
	if a.retry != nil {
		a.retry.Stop()
	}
	if a.cancel != nil {
		a.cancel()
	}
	if ep := a.endpoint; ep != nil {
		a.endpoint = nil
		within(accountBy, ep.Close)
	}
	// Stopped means the sound devices are closed too, of every call there was, unless the sound
	// service itself does not answer within the time: the last part of it is theirs alone.
	within(deadline, a.cleanup.Wait)
	a.setRegistration(domain.RegistrationState{Status: domain.Disabled})
}

// release frees what the actor holds after a failure, without waiting for the network.
func (a *actor) release() {
	// Nothing that reports back from now on belongs to an account or a call of this engine: a
	// registration that completes late is closed, not installed.
	a.generation++
	a.configured = false
	if a.retry != nil {
		a.retry.Stop()
	}
	if c := a.call; c != nil {
		c.snap.Phase = domain.EndedWith(domain.EndFailed)
		c.snap.EndedMS = time.Now().UnixMilli()
		c.snap.Version++
		a.emit(domain.CallChanged{Call: c.snap})
		c.release()
		a.call = nil
	}
	if a.cancel != nil {
		a.cancel()
	}
	if ep := a.endpoint; ep != nil {
		a.endpoint = nil
		go ep.Close()
	}
}

func closeDialog(d dialog, med *diago.DialogMedia) {
	ctx, done := context.WithTimeout(context.Background(), operationTimeout)
	_ = d.Hangup(ctx)
	done()
	_ = med.Close()
	_ = d.Close()
}
