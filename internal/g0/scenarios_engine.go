package g0

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	dmedia "github.com/emiago/diago/media"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/engine"
	"github.com/jniltinho/go-microsip/internal/core/media"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// EngineScenarios drive the engine the way a front end does: Commands in, Events out, with the
// in-memory sound standing in for the devices (openspec/changes/migrate-to-go-wails, tasks 3.4, 3.5).
func EngineScenarios() []Scenario {
	var list []Scenario
	for _, user := range []string{"1001", "1101", "1301", "1201"} {
		for _, c := range []media.Codec{media.PCMA, media.PCMU} {
			list = append(list, Scenario{fmt.Sprintf("engine/echo/%s/%s", user, c), engineEcho(user, c)})
		}
	}
	for _, pair := range [][2]string{{"1001", "1002"}, {"1201", "1202"}} {
		name := pair[0] + "-" + pair[1]
		list = append(list,
			Scenario{"engine/pair/" + name, enginePairCall(pair[0], pair[1])},
			Scenario{"engine/hold-mute/" + name, engineHoldMute(pair[0], pair[1])},
			Scenario{"engine/transfer/" + name, engineTransfer(pair[0], pair[1])},
		)
	}
	return append(list,
		Scenario{"engine/dtmf/1001", engineDTMF("1001", "123", toneLab)},
		Scenario{"engine/dtmf/1201", engineDTMF("1201", "123", toneLab)},
		Scenario{"engine/dtmf/wrong-digits-control", engineDTMF("1001", "999", toneReject)},
		Scenario{"engine/reject", engineReject},
		Scenario{"engine/dnd", engineDND},
		Scenario{"engine/busy", engineBusy},
		Scenario{"engine/cancel", engineCancel},
		Scenario{"engine/microphone-lost", engineMicrophoneLost},
		Scenario{"engine/secure/plain-offer-refused", enginePlainOffer},
		Scenario{"engine/shutdown/established", engineShutdown("established")},
		Scenario{"engine/shutdown/dialing", engineShutdown("dialing")},
		Scenario{"engine/shutdown/ringing", engineShutdown("ringing")},
		Scenario{"engine/register/wrong-password", engineRefusedRegistration("1001", domain.Unauthorized)},
		Scenario{"engine/register/unknown-ca", engineRefusedRegistration("1201", domain.Certificate)},
		Scenario{"engine/unregister", engineUnregister},
	)
}

// phone is one engine with its fake sound, seen only through Commands and Events.
type phone struct {
	user    string
	account Account
	e       *engine.Engine
	sound   *memory.Audio
	seen    []string
}

// start runs an engine for the account without registering it.
func (l *Lab) start(a Account, tone float64, codecs ...media.Codec) *phone {
	sound := &memory.Audio{Tone: tone}
	return &phone{user: a.User, account: a, sound: sound, e: engine.Start(engine.Options{
		Audio: sound, CA: a.ca(), Codecs: codecs, Log: l.Log,
	})}
}

// register sends Register and returns the state the engine settles on.
func (p *phone) register(ctx context.Context) (domain.RegistrationState, error) {
	p.e.Send(domain.Register{Settings: p.account.settings(), Password: domain.NewSecret(p.account.Password)})
	ev, err := p.wait(ctx, "the registration result", func(ev domain.Event) bool {
		r, ok := ev.(domain.RegistrationChanged)
		return ok && r.State.Status != domain.Registering
	})
	if err != nil {
		return domain.RegistrationState{}, err
	}
	return ev.(domain.RegistrationChanged).State, nil
}

// phone starts an engine for a lab account and registers it.
func (l *Lab) phone(ctx context.Context, user string, tone float64, codecs ...media.Codec) (*phone, error) {
	return l.phoneFor(ctx, l.Account(user, dmedia.CodecAudioAlaw), tone, codecs...)
}

func (l *Lab) phoneFor(ctx context.Context, a Account, tone float64, codecs ...media.Codec) (*phone, error) {
	p := l.start(a, tone, codecs...)
	state, err := p.register(ctx)
	if err == nil && state.Status != domain.Registered {
		err = fmt.Errorf("%s: registration ended as %v", p.user, state)
	}
	if err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

// wait reads events until one matches.
func (p *phone) wait(ctx context.Context, what string, match func(domain.Event) bool) (domain.Event, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		select {
		case ev, ok := <-p.e.Events():
			if !ok {
				return nil, fmt.Errorf("%s: the engine stopped before %s; saw %v", p.user, what, p.seen)
			}
			if p.seen = append(p.seen, fmt.Sprintf("%T%v", ev, ev)); len(p.seen) > 12 {
				p.seen = p.seen[1:]
			}
			if match(ev) {
				return ev, nil
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("%s: %s was not reported; saw %v", p.user, what, p.seen)
		}
	}
}

// snapshot waits for a call snapshot that matches.
func (p *phone) snapshot(ctx context.Context, what string, match func(domain.CallSnapshot) bool) (domain.CallSnapshot, error) {
	ev, err := p.wait(ctx, what, func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && match(c.Call)
	})
	if err != nil {
		return domain.CallSnapshot{}, err
	}
	return ev.(domain.CallChanged).Call, nil
}

// phase waits for the call to reach a phase; a call that ends first is an error.
func (p *phone) phase(ctx context.Context, kind domain.PhaseKind) (domain.CallSnapshot, error) {
	c, err := p.snapshot(ctx, "phase "+kind.String(), func(c domain.CallSnapshot) bool {
		return c.Phase.Kind == kind || c.Phase.IsFinal()
	})
	if err == nil && c.Phase.Kind != kind {
		err = fmt.Errorf("%s: the call ended (%v) before phase %v", p.user, c.Phase, kind)
	}
	return c, err
}

// ended waits for the call's final snapshot and checks why it ended.
func (p *phone) ended(ctx context.Context, want domain.EndReason) (domain.CallSnapshot, error) {
	c, err := p.snapshot(ctx, "the end of the call", func(c domain.CallSnapshot) bool { return c.Phase.IsFinal() })
	if err == nil && c.Phase.End != want {
		err = fmt.Errorf("%s: the call ended as %v, want %v", p.user, c.Phase, domain.EndedWith(want))
	}
	return c, err
}

// quiet checks that no call is reported for a while.
func (p *phone) quiet(ctx context.Context, d time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	if c, err := p.snapshot(ctx, "", func(domain.CallSnapshot) bool { return true }); err == nil {
		return fmt.Errorf("%s: an unexpected call was reported: %v", p.user, c)
	}
	return nil
}

// dial sends Dial and waits for the answer.
func (p *phone) dial(ctx context.Context, number string) (domain.CallSnapshot, error) {
	if err := p.send(number); err != nil {
		return domain.CallSnapshot{}, err
	}
	return p.phase(ctx, domain.Established)
}

// send sends Dial and does not wait.
func (p *phone) send(number string) error {
	target, err := domain.ParseDialTarget(number)
	if err != nil {
		return err
	}
	if !p.e.Send(domain.Dial{Target: target}) {
		return fmt.Errorf("%s: the engine did not take Dial", p.user)
	}
	return nil
}

// stop shuts the engine down and checks what every shutdown owes: Stopped is the last event, the
// stream closes after it and no sound stream stays open. It returns the events of the shutdown.
func (p *phone) stop() ([]domain.Event, error) {
	p.e.Send(domain.Shutdown{})
	var got []domain.Event
	timeout := time.After(8 * time.Second)
	for {
		select {
		case ev, ok := <-p.e.Events():
			if ok {
				got = append(got, ev)
				continue
			}
			if len(got) == 0 {
				return got, fmt.Errorf("%s: the event stream closed without Stopped", p.user)
			}
			if _, stopped := got[len(got)-1].(domain.Stopped); !stopped {
				return got, fmt.Errorf("%s: the last event is %T, want Stopped", p.user, got[len(got)-1])
			}
			// The sound is released beside the actor: give it a moment.
			for wait := time.Now().Add(3 * time.Second); p.sound.OpenStreams() > 0; time.Sleep(20 * time.Millisecond) {
				if time.Now().After(wait) {
					return got, fmt.Errorf("%s: %d sound streams stayed open after Stopped", p.user, p.sound.OpenStreams())
				}
			}
			return got, nil
		case <-timeout:
			return got, fmt.Errorf("%s: the engine did not stop; got %v", p.user, got)
		}
	}
}

// close stops an engine that a scenario left running.
func (p *phone) close() {
	select {
	case <-p.e.Done():
	default:
		_, _ = p.stop()
	}
}

// stopAll shuts the phones down and reports the first shutdown that broke its promise.
func stopAll(phones ...*phone) error {
	var first error
	for _, p := range phones {
		if _, err := p.stop(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// heard is what one phone's speaker played: frames, and the share of them carrying each tone.
type heard struct {
	frames int
	share  map[float64]float64
}

func (h heard) has(freq float64) bool { return h.share[freq] > 0.5 }

// listen measures what the phones play during dur, after 400 ms for the stream to settle.
func listen(dur time.Duration, phones ...*phone) []heard {
	time.Sleep(400 * time.Millisecond)
	for _, p := range phones {
		if s := p.sound.Last(); s != nil {
			s.Reset()
		}
	}
	time.Sleep(dur)
	out := make([]heard, len(phones))
	for i, p := range phones {
		if s := p.sound.Last(); s != nil {
			out[i].frames, out[i].share = s.Heard(toneA, toneB, toneLab, toneReject)
		}
	}
	return out
}

// engineEcho: an outgoing call to the echo extension carries the microphone out and back, with the
// offered codec, over the account's transport and media security.
func engineEcho(user string, codec media.Codec) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		p, err := l.phone(ctx, user, toneA, codec)
		if err != nil {
			return nil, err
		}
		defer p.close()
		c, err := p.dial(ctx, "600")
		if err != nil {
			return nil, err
		}
		h := listen(2*time.Second, p)[0]
		data := map[string]any{"codec": codec.String(), "heard": h.share[toneA], "frames": h.frames, "secure": c.Secure}
		if !h.has(toneA) {
			return data, errors.New("the microphone's tone did not come back from the echo")
		}
		if c.Secure != p.account.SecureMedia {
			return data, fmt.Errorf("the snapshot says secure=%v for an account with secure media=%v", c.Secure, p.account.SecureMedia)
		}
		p.e.Send(domain.Hangup{Call: c.ID})
		if _, err := p.ended(ctx, domain.EndNormal); err != nil {
			return data, err
		}
		return data, stopAll(p)
	}
}

// duo is two phones in one call: a called b and b answered.
type duo struct {
	a, b   *phone
	ca, cb domain.CallSnapshot
}

func (d *duo) close() {
	for _, p := range []*phone{d.a, d.b} {
		if p != nil {
			p.close()
		}
	}
}

// ring registers both phones and lets a call b, up to b's ringing.
func ring(ctx context.Context, l *Lab, caller, callee string) (*duo, error) {
	d := &duo{}
	var err error
	if d.b, err = l.phone(ctx, callee, toneB); err != nil {
		return d, err
	}
	if d.a, err = l.phone(ctx, caller, toneA); err != nil {
		return d, err
	}
	if err = d.a.send(callee); err != nil {
		return d, err
	}
	if d.cb, err = d.b.phase(ctx, domain.Ringing); err != nil {
		return d, err
	}
	if d.cb.Direction != domain.Incoming || d.cb.Remote != caller {
		return d, fmt.Errorf("the callee sees %v from %q, want an incoming call from %s", d.cb.Direction, d.cb.Remote, caller)
	}
	return d, nil
}

// connectEngines is ring followed by Answer.
func connectEngines(ctx context.Context, l *Lab, caller, callee string) (*duo, error) {
	d, err := ring(ctx, l, caller, callee)
	if err != nil {
		return d, err
	}
	d.b.e.Send(domain.Answer{Call: d.cb.ID})
	if d.cb, err = d.b.phase(ctx, domain.Established); err != nil {
		return d, err
	}
	d.ca, err = d.a.phase(ctx, domain.Established)
	return d, err
}

// enginePairCall: an outgoing and an incoming call with a different tone measured in each direction;
// the callee hangs up and both sides end normally.
func enginePairCall(caller, callee string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		d, err := connectEngines(ctx, l, caller, callee)
		defer d.close()
		if err != nil {
			return nil, err
		}
		h := listen(2*time.Second, d.a, d.b)
		data := map[string]any{
			"caller_heard": h[0].share[toneB], "callee_heard": h[1].share[toneA],
			"caller_secure": d.ca.Secure, "callee_secure": d.cb.Secure,
		}
		switch {
		case !h[0].has(toneB):
			return data, errors.New("the caller did not hear the callee")
		case !h[1].has(toneA):
			return data, errors.New("the callee did not hear the caller")
		case d.ca.Secure != d.a.account.SecureMedia || d.cb.Secure != d.b.account.SecureMedia:
			return data, errors.New("a snapshot's secure flag does not match its account")
		}
		d.b.e.Send(domain.Hangup{Call: d.cb.ID})
		if _, err := d.b.ended(ctx, domain.EndNormal); err != nil {
			return data, err
		}
		if _, err := d.a.ended(ctx, domain.EndNormal); err != nil {
			return data, err
		}
		return data, stopAll(d.a, d.b)
	}
}

// engineHoldMute: on hold neither side hears the other and the microphone is not sent; resumed, both
// do again. Muted, the other side gets silence while the muted side still hears.
func engineHoldMute(caller, callee string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		d, err := connectEngines(ctx, l, caller, callee)
		defer d.close()
		if err != nil {
			return nil, err
		}
		before := listen(1500*time.Millisecond, d.a, d.b)
		d.a.e.Send(domain.Hold{Call: d.ca.ID})
		if _, err := d.a.phase(ctx, domain.Held); err != nil {
			return nil, err
		}
		during := listen(1500*time.Millisecond, d.a, d.b)
		d.a.e.Send(domain.Resume{Call: d.ca.ID})
		if _, err := d.a.phase(ctx, domain.Established); err != nil {
			return nil, err
		}
		after := listen(1500*time.Millisecond, d.a, d.b)
		d.a.e.Send(domain.SetMute{Call: d.ca.ID, Muted: true})
		if _, err := d.a.snapshot(ctx, "muted", func(c domain.CallSnapshot) bool { return c.Muted }); err != nil {
			return nil, err
		}
		muted := listen(1500*time.Millisecond, d.a, d.b)
		d.a.e.Send(domain.SetMute{Call: d.ca.ID, Muted: false})
		if _, err := d.a.snapshot(ctx, "unmuted", func(c domain.CallSnapshot) bool { return !c.Muted }); err != nil {
			return nil, err
		}
		unmuted := listen(1500*time.Millisecond, d.a, d.b)
		data := map[string]any{
			"holder_heard":       []float64{before[0].share[toneB], during[0].share[toneB], after[0].share[toneB]},
			"held_heard":         []float64{before[1].share[toneA], during[1].share[toneA], after[1].share[toneA]},
			"muted_side_heard":   []float64{muted[0].share[toneB], unmuted[0].share[toneB]},
			"other_side_heard":   []float64{muted[1].share[toneA], unmuted[1].share[toneA]},
			"other_side_frames":  muted[1].frames,
			"held_frames_during": during[1].frames,
		}
		switch {
		case !before[0].has(toneB) || !before[1].has(toneA):
			return data, errors.New("no two-way audio before the hold")
		case during[0].has(toneB):
			return data, errors.New("the holder still heard the other party during the hold")
		case during[1].has(toneA):
			return data, errors.New("the held party still heard the holder during the hold")
		case !after[0].has(toneB) || !after[1].has(toneA):
			return data, errors.New("two-way audio did not come back after the resume")
		case muted[1].has(toneA):
			return data, errors.New("the other party still heard a muted microphone")
		case !muted[0].has(toneB):
			return data, errors.New("the muted side stopped hearing the other party")
		case !unmuted[0].has(toneB) || !unmuted[1].has(toneA):
			return data, errors.New("two-way audio did not come back after unmuting")
		}
		return data, stopAll(d.a, d.b)
	}
}

// engineTransfer: a blind transfer of the callee to the lab's tone extension. The transferring side
// ends as transferred; the callee stays in its call and hears the new destination.
func engineTransfer(caller, callee string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		d, err := connectEngines(ctx, l, caller, callee)
		defer d.close()
		if err != nil {
			return nil, err
		}
		if before := listen(1200*time.Millisecond, d.b)[0]; !before.has(toneA) {
			return nil, errors.New("no audio before the transfer")
		}
		target, _ := domain.ParseDialTarget("602")
		d.a.e.Send(domain.Transfer{Call: d.ca.ID, Target: target})
		end, err := d.a.ended(ctx, domain.EndTransferred)
		if err != nil {
			return nil, err
		}
		h := listen(2500*time.Millisecond, d.b)[0]
		data := map[string]any{"transfer": end.Transfer.Kind.String(), "callee_heard": h.share[toneLab]}
		if end.Transfer.Kind != domain.TransferSucceeded {
			return data, fmt.Errorf("the final snapshot's transfer is %v", end.Transfer)
		}
		if !h.has(toneLab) {
			return data, errors.New("the callee did not hear the transfer destination")
		}
		d.b.e.Send(domain.Hangup{Call: d.cb.ID})
		if _, err := d.b.ended(ctx, domain.EndNormal); err != nil {
			return data, err
		}
		return data, stopAll(d.a, d.b)
	}
}

// engineDTMF dials the lab's digit reader and sends keys with SendDTMF: the PBX plays 1000 Hz when it
// read "123" and 400 Hz otherwise.
func engineDTMF(user, digits string, want float64) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		p, err := l.phone(ctx, user, toneA)
		if err != nil {
			return nil, err
		}
		defer p.close()
		c, err := p.dial(ctx, "601")
		if err != nil {
			return nil, err
		}
		time.Sleep(600 * time.Millisecond)
		for _, digit := range digits {
			key, err := domain.NewDTMFKey(digit)
			if err != nil {
				return nil, err
			}
			p.e.Send(domain.SendDTMF{Call: c.ID, Key: key})
			time.Sleep(250 * time.Millisecond)
		}
		h := listen(2100*time.Millisecond, p)[0]
		data := map[string]any{"digits": len(digits), "accepted_tone": h.share[toneLab], "rejected_tone": h.share[toneReject], "frames": h.frames}
		if !h.has(want) {
			return data, fmt.Errorf("the PBX did not play the %v Hz tone", want)
		}
		return data, stopAll(p)
	}
}

// engineReject: Reject answers 486; the callee's call ends as rejected, the caller's as busy.
func engineReject(ctx context.Context, l *Lab) (map[string]any, error) {
	d, err := ring(ctx, l, "1001", "1002")
	defer d.close()
	if err != nil {
		return nil, err
	}
	d.b.e.Send(domain.Reject{Call: d.cb.ID})
	if _, err := d.b.ended(ctx, domain.EndRejected); err != nil {
		return nil, err
	}
	if _, err := d.a.ended(ctx, domain.EndBusy); err != nil {
		return nil, err
	}
	return nil, stopAll(d.a, d.b)
}

// engineDND: with do-not-disturb on, a call is refused as busy and never shown.
func engineDND(ctx context.Context, l *Lab) (map[string]any, error) {
	b, err := l.phone(ctx, "1002", toneB)
	if err != nil {
		return nil, err
	}
	defer b.close()
	a, err := l.phone(ctx, "1001", toneA)
	if err != nil {
		return nil, err
	}
	defer a.close()
	b.e.Send(domain.SetDND{On: true})
	if err := a.send("1002"); err != nil {
		return nil, err
	}
	if _, err := a.ended(ctx, domain.EndBusy); err != nil {
		return nil, err
	}
	if err := b.quiet(ctx, time.Second); err != nil {
		return nil, err
	}
	return nil, stopAll(a, b)
}

// engineBusy: a second call to a phone that is in a call is refused as busy; the first call goes on.
func engineBusy(ctx context.Context, l *Lab) (map[string]any, error) {
	b, err := l.phone(ctx, "1002", toneB)
	if err != nil {
		return nil, err
	}
	defer b.close()
	if _, err := b.dial(ctx, "600"); err != nil {
		return nil, err
	}
	a, err := l.phone(ctx, "1001", toneA)
	if err != nil {
		return nil, err
	}
	defer a.close()
	if err := a.send("1002"); err != nil {
		return nil, err
	}
	if _, err := a.ended(ctx, domain.EndBusy); err != nil {
		return nil, err
	}
	if err := b.quiet(ctx, time.Second); err != nil {
		return nil, err
	}
	h := listen(time.Second, b)[0]
	data := map[string]any{"first_call_heard": h.share[toneB]}
	if !h.has(toneB) {
		return data, errors.New("the first call lost its audio")
	}
	return data, stopAll(a, b)
}

// engineCancel: hanging up a call that still rings cancels it on both sides.
func engineCancel(ctx context.Context, l *Lab) (map[string]any, error) {
	d, err := ring(ctx, l, "1001", "1002")
	defer d.close()
	if err != nil {
		return nil, err
	}
	c, err := d.a.snapshot(ctx, "the outgoing call", func(domain.CallSnapshot) bool { return true })
	if err != nil {
		return nil, err
	}
	d.a.e.Send(domain.Hangup{Call: c.ID})
	if _, err := d.a.ended(ctx, domain.EndCanceled); err != nil {
		return nil, err
	}
	if _, err := d.b.ended(ctx, domain.EndCanceled); err != nil {
		return nil, err
	}
	return nil, stopAll(d.a, d.b)
}

// engineMicrophoneLost: when the microphone is unplugged during a call the loss is reported once,
// the call stays up in silence, and no other microphone is opened.
func engineMicrophoneLost(ctx context.Context, l *Lab) (map[string]any, error) {
	p, err := l.phone(ctx, "1001", toneA)
	if err != nil {
		return nil, err
	}
	defer p.close()
	c, err := p.dial(ctx, "600")
	if err != nil {
		return nil, err
	}
	before := listen(time.Second, p)[0]
	p.sound.Last().Unplug()
	if _, err := p.wait(ctx, "the microphone-lost notice", func(ev domain.Event) bool {
		n, ok := ev.(domain.Noticed)
		return ok && n.Notice == domain.NoticeMicrophoneLost
	}); err != nil {
		return nil, err
	}
	after := listen(1500*time.Millisecond, p)[0]
	data := map[string]any{"heard_before": before.share[toneA], "heard_after": after.share[toneA], "frames_after": after.frames, "streams": p.sound.OpenStreams()}
	switch {
	case !before.has(toneA):
		return data, errors.New("no audio before the microphone was unplugged")
	case after.has(toneA):
		return data, errors.New("the echo still carries the microphone after it was unplugged")
	case after.frames == 0:
		return data, errors.New("the call stopped receiving after the microphone was unplugged")
	case p.sound.OpenStreams() != 1:
		return data, fmt.Errorf("%d sound streams are open: another device was opened without the user", p.sound.OpenStreams())
	}
	// The loss is said once, and the call is still there to hang up.
	if err := p.quiet(ctx, time.Second); err != nil {
		return data, err
	}
	p.e.Send(domain.Hangup{Call: c.ID})
	if _, err := p.ended(ctx, domain.EndNormal); err != nil {
		return data, err
	}
	return data, stopAll(p)
}

// enginePlainOffer: 1302 gets plain RTP offers from the PBX. Configured with secure media, its engine
// refuses the call (488) and never shows it; the caller's call ends as not acceptable.
func enginePlainOffer(ctx context.Context, l *Lab) (map[string]any, error) {
	secure := l.Account("1302", dmedia.CodecAudioAlaw)
	secure.SecureMedia = true
	b, err := l.phoneFor(ctx, secure, toneB)
	if err != nil {
		return nil, err
	}
	defer b.close()
	a, err := l.phone(ctx, "1301", toneA)
	if err != nil {
		return nil, err
	}
	defer a.close()
	if err := a.send("1302"); err != nil {
		return nil, err
	}
	if _, err := a.ended(ctx, domain.EndNotAcceptable); err != nil {
		return nil, err
	}
	if err := b.quiet(ctx, time.Second); err != nil {
		return nil, err
	}
	if b.sound.Last() != nil {
		return nil, errors.New("the refused call opened the sound devices")
	}
	return nil, stopAll(a, b)
}

// engineShutdown: Shutdown during a call always delivers the call's final snapshot, then Stopped,
// and the other side sees the call end.
func engineShutdown(when string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		var (
			d             *duo
			err           error
			quitter, peer *phone
			own, other    domain.EndReason
		)
		switch when {
		case "established":
			d, err = connectEngines(ctx, l, "1001", "1002")
			quitter, peer, own, other = d.a, d.b, domain.EndNormal, domain.EndNormal
		case "dialing":
			d, err = ring(ctx, l, "1001", "1002")
			quitter, peer, own, other = d.a, d.b, domain.EndCanceled, domain.EndCanceled
		default: // ringing
			d, err = ring(ctx, l, "1001", "1002")
			quitter, peer, own, other = d.b, d.a, domain.EndRejected, domain.EndBusy
		}
		defer d.close()
		if err != nil {
			return nil, err
		}
		start := time.Now()
		events, err := quitter.stop()
		data := map[string]any{"events": len(events), "took_ms": time.Since(start).Milliseconds()}
		if err != nil {
			return data, err
		}
		finals := 0
		for _, ev := range events {
			if c, ok := ev.(domain.CallChanged); ok && c.Call.Phase.IsFinal() {
				finals++
				if c.Call.Phase.End != own {
					return data, fmt.Errorf("the final snapshot says %v, want %v", c.Call.Phase, domain.EndedWith(own))
				}
			}
		}
		if finals != 1 {
			return data, fmt.Errorf("%d final snapshots were delivered on shutdown, want 1: %v", finals, events)
		}
		if _, err := peer.ended(ctx, other); err != nil {
			return data, err
		}
		return data, stopAll(peer)
	}
}

// engineRefusedRegistration: a refusal the user must fix is reported with its reason.
func engineRefusedRegistration(user string, want domain.FailureKind) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		a := l.Account(user, dmedia.CodecAudioAlaw)
		if want == domain.Certificate {
			a.CAFile = filepath.Join(l.Dir, "untrusted-ca.crt")
		} else {
			a.Password = "wrong-" + a.Password
		}
		p := l.start(a, toneA)
		defer p.close()
		state, err := p.register(ctx)
		if err != nil {
			return nil, err
		}
		data := map[string]any{"state": state.Status.String(), "failure": state.Failure.String()}
		if state.Status != domain.RegistrationFailed || state.Failure != want {
			return data, fmt.Errorf("the registration ended as %v, want failed: %v", state, want)
		}
		if err := p.send("600"); err != nil {
			return data, err
		}
		if _, err := p.wait(ctx, "the not-registered notice", func(ev domain.Event) bool {
			n, ok := ev.(domain.Noticed)
			return ok && n.Notice == domain.NoticeNotRegistered
		}); err != nil {
			return data, err
		}
		return data, stopAll(p)
	}
}

// engineUnregister: Unregister disables the account, and a Dial is then refused.
func engineUnregister(ctx context.Context, l *Lab) (map[string]any, error) {
	p, err := l.phone(ctx, "1001", toneA)
	if err != nil {
		return nil, err
	}
	defer p.close()
	p.e.Send(domain.Unregister{})
	if _, err := p.wait(ctx, "the disabled registration", func(ev domain.Event) bool {
		r, ok := ev.(domain.RegistrationChanged)
		return ok && r.State.Status == domain.Disabled
	}); err != nil {
		return nil, err
	}
	if err := p.send("600"); err != nil {
		return nil, err
	}
	if _, err := p.wait(ctx, "the not-registered notice", func(ev domain.Event) bool {
		n, ok := ev.(domain.Noticed)
		return ok && n.Notice == domain.NoticeNotRegistered
	}); err != nil {
		return nil, err
	}
	return nil, stopAll(p)
}
