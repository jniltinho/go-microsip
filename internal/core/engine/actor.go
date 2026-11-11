package engine

import (
	"context"
	"errors"
	"fmt"
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
		case f := <-e.internal:
			f()
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
	a.account = sip.Account{Settings: c.Settings, Password: c.Password, CAFile: a.e.opts.CAFile, UserAgent: a.e.opts.UserAgent, Codecs: a.e.opts.Codecs}
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
				if err == nil {
					_ = med.Close()
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
	audio, err := a.e.opts.Audio.Open(a.e.opts.Input, a.e.opts.Output)
	if err != nil {
		// The call stays up without sound; the user sees why and decides.
		a.e.opts.Log.Warn("audio unavailable", "error", err)
		a.notice(domain.NoticeAudioUnavailable)
	} else if err := c.startAudio(med, audio, a.e.opts.Log); err != nil {
		audio.Close()
		a.e.opts.Log.Warn("audio unavailable", "error", err)
		a.notice(domain.NoticeAudioUnavailable)
	}
	if c.snap.Direction == domain.Outgoing {
		a.watch(c)
	}
}

// watch tells the actor when the other side ends the call.
func (a *actor) watch(c *call) {
	done := c.dialog.Context().Done()
	go func() {
		<-done
		a.e.post(func() {
			switch c.snap.Phase.Kind {
			case domain.Ringing:
				a.end(c, domain.EndCanceled)
			case domain.Terminating:
				a.end(c, c.ending)
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
		c.ending = domain.EndNormal
		a.move(c, domain.CallPhase{Kind: domain.Terminating})
		d := c.dialog
		go func() {
			ctx, done := context.WithTimeout(context.Background(), operationTimeout)
			if err := d.Hangup(ctx); err != nil {
				a.e.opts.Log.Warn("hang-up was not confirmed", "error", err)
			}
			done()
			a.e.post(func() { a.end(c, domain.EndNormal) }) // also when the other side never confirmed
		}()
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
	result := make(chan int, 8)
	refer := c.refer
	go func() {
		ctx, done := context.WithTimeout(context.Background(), operationTimeout)
		err := refer(ctx, to, func(code int) {
			select {
			case result <- code:
			default:
			}
		})
		done()
		code := 0
		if err != nil {
			code = 500
		}
		for timeout := time.After(transferTimeout); code < 200; {
			select {
			case code = <-result:
			case <-timeout:
				code = 408
			}
		}
		a.e.post(func() {
			if c.snap.Phase.IsFinal() {
				return
			}
			if code != 200 {
				c.snap.Transfer = domain.TransferState{Kind: domain.TransferFailed, Code: uint16(code)}
				a.publish(c) // the call goes on
				return
			}
			c.snap.Transfer = domain.TransferState{Kind: domain.TransferSucceeded}
			a.publish(c)
			c.ending = domain.EndTransferred
			a.move(c, domain.CallPhase{Kind: domain.Terminating})
			d := c.dialog
			go func() {
				ctx, done := context.WithTimeout(context.Background(), operationTimeout)
				_ = d.Hangup(ctx)
				done()
				a.e.post(func() { a.end(c, domain.EndTransferred) })
			}()
		})
	}()
}

// --- shutdown -------------------------------------------------------------------------------

// shutdown ends the call, unregisters and returns within shutdownTimeout.
func (a *actor) shutdown() {
	deadline := time.Now().Add(shutdownTimeout)
	if c := a.call; c != nil {
		reason := domain.EndNormal
		switch {
		case c.cancel != nil && c.dialog == nil: // still dialing
			c.cancel()
			reason = domain.EndCanceled
		case c.server != nil && c.snap.Phase.Kind == domain.Ringing:
			select {
			case <-c.rung:
			case <-time.After(time.Until(deadline)):
			}
			_ = c.server.Respond(gosip.StatusBusyHere, "Busy Here", nil)
			reason = domain.EndRejected
		case c.dialog != nil:
			ctx, done := context.WithDeadline(context.Background(), deadline)
			_ = c.dialog.Hangup(ctx)
			done()
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
		closed := make(chan struct{})
		go func() { ep.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(time.Until(deadline)):
		}
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

func closeDialog(d *diago.DialogClientSession, med *diago.DialogMedia) {
	ctx, done := context.WithTimeout(context.Background(), operationTimeout)
	_ = d.Hangup(ctx)
	done()
	_ = med.Close()
	_ = d.Close()
}
