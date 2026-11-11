package engine

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/sip"
)

const (
	// registerTimeout bounds one registration attempt.
	registerTimeout = 10 * time.Second
	// noAnswerTimeout ends an outgoing call nobody answered.
	noAnswerTimeout = 32 * time.Second
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
	attempts   int
	retry      *time.Timer

	dnd      bool
	call     *call
	lastCall uint64
	// released is closed once the sound devices of the call that ended last are closed.
	released chan struct{}
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
		func() {
			defer func() {
				if r := recover(); r != nil {
					e.opts.Log.Error("engine failure at the stop", "panic", fmt.Sprint(r))
				}
			}()
			for _, f := range pending {
				f()
			}
		}()
		e.late.Unlock()
		a.emit(domain.Stopped{})
		close(e.stopped)
		e.events.close()
	}()
	for {
		select {
		case c := <-e.commands:
			if _, stop := c.(domain.Shutdown); stop {
				a.shutdown()
				return
			}
			a.handle(c)
		case <-e.wake:
			for _, f := range e.take() {
				f()
			}
		}
	}
}

func (a *actor) emit(ev domain.Event)   { a.e.events.emit(ev) }
func (a *actor) notice(n domain.Notice) { a.emit(domain.Noticed{Notice: n}) }

func (a *actor) setRegistration(s domain.RegistrationState) {
	if a.reg != s {
		a.reg = s
		a.emit(domain.RegistrationChanged{State: s})
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
		})
	}()
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
		snap: domain.CallSnapshot{ID: domain.CallID(a.lastCall), Direction: dir, Remote: remote, Phase: domain.CallPhase{Kind: phase}},
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
	a.publish(c)
	c.release()
	a.released = c.released
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
		d, med, err := ep.Dial(ctx, target.Value(), func(int) {
			a.e.post(func() { a.move(c, domain.CallPhase{Kind: domain.Ringing}) })
		})
		a.e.post(func() {
			if c.snap.Phase.IsFinal() { // ended meanwhile
				if err == nil {
					go closeDialog(d, med)
				}
				return
			}
			if err != nil {
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
	case errors.Is(err, sip.ErrInsecureAnswer):
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
	// The sound service is asked beside the actor: one that does not answer must not keep the
	// engine from taking commands, Shutdown among them.
	opts := a.e.opts
	go func() {
		audio, err := opts.Audio.Open(opts.Input, opts.Output)
		a.e.post(func() {
			over := c.released != nil || c.snap.Phase.IsFinal()
			if err == nil && over {
				audio.Close() // the call ended while the devices were opening
				return
			}
			if err == nil {
				if err = c.startAudio(med, audio, opts.Log, func() {
					a.e.post(func() {
						opts.Log.Warn("the microphone stopped delivering")
						a.notice(domain.NoticeMicrophoneLost)
					})
				}, func(direction string, cause error) {
					a.e.post(func() { a.mediaFailed(c, direction, cause) })
				}); err != nil {
					audio.Close()
				}
			}
			if err != nil && !over {
				// The call stays up without sound; the user sees why and decides.
				opts.Log.Warn("audio unavailable", "error", err)
				a.notice(domain.NoticeAudioUnavailable)
			}
		})
	}()
}

// mediaFailed ends a call whose audio stopped on its own in one direction: a call that looks
// established and carries nothing is worse than one that ends.
func (a *actor) mediaFailed(c *call, direction string, cause error) {
	if k := c.snap.Phase.Kind; k != domain.Established && k != domain.Held {
		return
	}
	a.e.opts.Log.Warn("the call's audio failed", "direction", direction, "error", cause)
	a.terminate(c, domain.EndFailed)
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
				a.end(c, domain.EndCanceled)
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
	if c.snap.Phase.Kind != domain.Established || !c.sound {
		a.notice(domain.NoticeInvalidState)
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
		c.held.Store(true) // the microphone stops before the other side is told
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
				c.held.Store(!hold)
				a.notice(domain.NoticeInvalidState)
				return
			}
			c.held.Store(hold)
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
		c.muted.Store(muted)
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
	// Stopped means the sound devices are closed too; the last part of the time is theirs alone.
	if released := a.released; released != nil {
		within(deadline, func() { <-released })
	}
	a.setRegistration(domain.RegistrationState{Status: domain.Disabled})
}

// release frees what the actor holds after a failure, without waiting for the network.
func (a *actor) release() {
	if c := a.call; c != nil {
		c.snap.Phase = domain.EndedWith(domain.EndFailed)
		c.snap.Version++
		a.emit(domain.CallChanged{Call: c.snap})
		c.release()
	}
	if a.cancel != nil {
		a.cancel()
	}
	if ep := a.endpoint; ep != nil {
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
