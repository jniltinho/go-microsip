package g0

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	dmedia "github.com/emiago/diago/media"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/engine"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// Scenarios added for the parity gate (openspec/changes/migrate-to-go-wails, task 7.4): clauses
// of the Rust-era scenarios that no run had shown.

// engineDNDDuringACall: do-not-disturb switched on during an established call leaves that call as
// it is, with its audio; a new call is refused as busy and never shown (SIP-07).
func engineDNDDuringACall(ctx context.Context, l *Lab) (map[string]any, error) {
	b, err := l.phone(ctx, "1002", toneB)
	if err != nil {
		return nil, err
	}
	defer b.close()
	first, err := b.dial(ctx, "600")
	if err != nil {
		return nil, err
	}
	b.e.Send(domain.SetDND{On: true})
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
	// Nothing rings, and the first call is neither ended nor changed.
	if err := b.quiet(ctx, time.Second); err != nil {
		return nil, err
	}
	h := listen(time.Second, b)[0]
	data := map[string]any{"first_call_heard": h.share[toneB]}
	if !h.has(toneB) {
		return data, errors.New("the established call lost its audio when do-not-disturb refused another")
	}
	b.e.Send(domain.Hangup{Call: first.ID})
	if _, err := b.ended(ctx, domain.EndNormal); err != nil {
		return data, err
	}
	return data, stopAll(a, b)
}

// engineTransferFailed: a blind transfer to a destination the PBX does not have is reported as
// failed, never as done, and the call it was asked on goes on, with its audio in both
// directions, until it is hung up (SIP-08).
func engineTransferFailed(caller, callee string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		d, err := connectEngines(ctx, l, caller, callee)
		defer d.close()
		if err != nil {
			return nil, err
		}
		target, _ := domain.ParseDialTarget("699") // no such extension in the lab
		d.a.e.Send(domain.Transfer{Call: d.ca.ID, Target: target})
		after, err := d.a.snapshot(ctx, "the transfer's result", func(c domain.CallSnapshot) bool {
			return c.Phase.IsFinal() || c.Transfer.Kind == domain.TransferFailed || c.Transfer.Kind == domain.TransferSucceeded
		})
		if err != nil {
			return nil, err
		}
		data := map[string]any{"transfer": after.Transfer.Kind.String(), "code": after.Transfer.Code, "phase": after.Phase.String()}
		if after.Phase.IsFinal() || after.Transfer.Kind != domain.TransferFailed {
			return data, fmt.Errorf("after a transfer to nowhere the call is %v with transfer %v", after.Phase, after.Transfer)
		}
		h := listen(1500*time.Millisecond, d.a, d.b)
		data["caller_heard"], data["callee_heard"] = h[0].share[toneB], h[1].share[toneA]
		if !h[0].has(toneB) || !h[1].has(toneA) {
			return data, errors.New("the call lost its audio after the failed transfer")
		}
		d.a.e.Send(domain.Hangup{Call: d.ca.ID})
		if _, err := d.a.ended(ctx, domain.EndNormal); err != nil {
			return data, err
		}
		if _, err := d.b.ended(ctx, domain.EndNormal); err != nil {
			return data, err
		}
		return data, stopAll(d.a, d.b)
	}
}

// engineCancelCrossingAnswer: the caller hangs up while the callee answers, a few milliseconds
// before or after, so that the cancellation and the answer cross on the way. Whichever wins, both
// sides reach a final state, nothing rings on, and no sound stream stays open (SIP-04).
func engineCancelCrossingAnswer(ctx context.Context, l *Lab) (map[string]any, error) {
	data := map[string]any{}
	for _, lead := range []time.Duration{-30, -10, -3, 0, 3, 10, 30} {
		lead *= time.Millisecond
		outcome, err := crossing(ctx, l, lead)
		data[fmt.Sprintf("hangup %+d ms from the answer", lead.Milliseconds())] = outcome
		if err != nil {
			return data, fmt.Errorf("hang-up %v from the answer: %w", lead, err)
		}
	}
	return data, nil
}

// crossing rings 1002 from 1001 and sends the hang-up lead after the answer (before it, when
// negative). It returns how each side ended.
func crossing(ctx context.Context, l *Lab, lead time.Duration) (string, error) {
	d, err := ring(ctx, l, "1001", "1002")
	defer d.close()
	if err != nil {
		return "", err
	}
	outgoing, err := d.a.snapshot(ctx, "the outgoing call", func(domain.CallSnapshot) bool { return true })
	if err != nil {
		return "", err
	}
	answer := func() { d.b.e.Send(domain.Answer{Call: d.cb.ID}) }
	hangup := func() { d.a.e.Send(domain.Hangup{Call: outgoing.ID}) }
	if lead < 0 {
		hangup()
		time.Sleep(-lead)
		answer()
	} else {
		answer()
		time.Sleep(lead)
		hangup()
	}
	enda, err := d.a.snapshot(ctx, "the caller's final state", func(c domain.CallSnapshot) bool { return c.Phase.IsFinal() })
	if err != nil {
		return "", err
	}
	endb, err := d.b.snapshot(ctx, "the callee's final state", func(c domain.CallSnapshot) bool { return c.Phase.IsFinal() })
	if err != nil {
		return "", err
	}
	outcome := fmt.Sprintf("caller %v, callee %v", enda.Phase, endb.Phase)
	// Nothing comes after the final state on either side: no ghost call, no second ring.
	for _, p := range []*phone{d.a, d.b} {
		if err := p.quiet(ctx, 500*time.Millisecond); err != nil {
			return outcome, err
		}
	}
	return outcome, stopAll(d.a, d.b) // stopping reports a sound stream left open
}

// engineRenewal: a registration of thirty seconds, the least the lab PBX grants, is renewed on
// its own. For forty-five seconds the engine reports no change of it, and then the PBX still
// rings the phone: its only allowed contact is neither expired nor refused as a second one
// (SIP-01).
func engineRenewal(user, peer string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		account := l.Account(user, dmedia.CodecAudioAlaw)
		account.Expires = 30
		b, err := l.phoneFor(ctx, account, toneB)
		if err != nil {
			return nil, err
		}
		defer b.close()
		data := map[string]any{"transport": account.Transport, "expires_s": account.Expires, "waited_s": 45}
		if err := b.absent(ctx, 45*time.Second, "a change of the registration", func(ev domain.Event) bool {
			_, changed := ev.(domain.RegistrationChanged)
			return changed
		}); err != nil {
			return data, err
		}
		a, err := l.phone(ctx, peer, toneA)
		if err != nil {
			return data, err
		}
		defer a.close()
		if err := a.send(user); err != nil {
			return data, err
		}
		if _, err := b.phase(ctx, domain.Ringing); err != nil {
			return data, fmt.Errorf("after the first registration's lifetime the PBX does not ring %s: %w", user, err)
		}
		outgoing, err := a.snapshot(ctx, "the outgoing call", func(domain.CallSnapshot) bool { return true })
		if err != nil {
			return data, err
		}
		a.e.Send(domain.Hangup{Call: outgoing.ID})
		if _, err := b.ended(ctx, domain.EndCanceled); err != nil {
			return data, err
		}
		return data, stopAll(a, b)
	}
}

// engineSecureLog: a call with SDES-SRTP over TLS, with the engine's diagnostics taken at their
// most detailed. Neither the key material of the session descriptions nor the account's password
// is in them (AUD-05).
func engineSecureLog(ctx context.Context, l *Lab) (map[string]any, error) {
	account := l.Account("1201", dmedia.CodecAudioAlaw)
	var log bytes.Buffer
	sound := &memory.Audio{Tone: toneA}
	p := &phone{user: account.User, account: account, sound: sound, e: engine.Start(engine.Options{
		Audio: sound, CA: account.ca(), Log: slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})}
	defer p.close()
	if state, err := p.register(ctx); err != nil || state.Status != domain.Registered {
		return nil, fmt.Errorf("registration ended as %v: %v", state, err)
	}
	call, err := p.dial(ctx, "600")
	if err != nil {
		return nil, err
	}
	if !call.Secure {
		return nil, errors.New("the call is not secure")
	}
	h := listen(time.Second, p)[0]
	p.e.Send(domain.Hangup{Call: call.ID})
	if _, err := p.ended(ctx, domain.EndNormal); err != nil {
		return nil, err
	}
	if err := stopAll(p); err != nil {
		return nil, err
	}
	text := log.String()
	data := map[string]any{"heard": h.share[toneA], "log_bytes": len(text)}
	for _, secret := range []string{"inline:", "a=crypto", account.Password} {
		if strings.Contains(text, secret) {
			return data, fmt.Errorf("the engine's log of a secure call holds %q", strings.SplitN(secret, ":", 2)[0])
		}
	}
	if !h.has(toneA) {
		return data, errors.New("no audio in the secure call")
	}
	return data, nil
}

// engineRing: a call that comes in rings on the callee's speaker, and the caller hears a ringback
// on its own, each generated by its softphone: no microphone is opened while the call rings, and
// nothing of either tone crosses the call. Both stop with the answer, after which each side
// hears only the other (task 8.3).
func engineRing(ctx context.Context, l *Lab) (map[string]any, error) {
	d, err := ring(ctx, l, "1001", "1002")
	defer d.close()
	if err != nil {
		return nil, err
	}
	time.Sleep(1500 * time.Millisecond) // the first second of each tone, and a little of its pause
	rings, backs := d.b.sound.OutputStreams(), d.a.sound.OutputStreams()
	if len(rings) != 1 || len(backs) != 1 {
		return nil, fmt.Errorf("the callee opened %d speakers for a ring and the caller %d for a ringback, want one each", len(rings), len(backs))
	}
	if mics := d.a.sound.CallStreams() + d.b.sound.CallStreams(); mics != 0 {
		return nil, fmt.Errorf("%d microphones were opened while the call rang", mics)
	}
	_, rang := rings[0].Heard(engine.RingLowHz, engine.RingHighHz, engine.RingbackHz)
	_, back := backs[0].Heard(engine.RingbackHz, engine.RingLowHz, engine.RingHighHz)
	data := map[string]any{"ring_low": rang[engine.RingLowHz], "ring_high": rang[engine.RingHighHz], "ringback": back[engine.RingbackHz]}
	if rang[engine.RingLowHz] < 0.2 || rang[engine.RingHighHz] < 0.2 || rang[engine.RingbackHz] > 0.02 {
		return data, fmt.Errorf("the callee's speaker did not play the ring: %v", rang)
	}
	if back[engine.RingbackHz] < 0.4 || back[engine.RingLowHz]+back[engine.RingHighHz] > 0.02 {
		return data, fmt.Errorf("the caller's speaker did not play the ringback: %v", back)
	}
	d.b.e.Send(domain.Answer{Call: d.cb.ID})
	if d.cb, err = d.b.phase(ctx, domain.Established); err != nil {
		return data, err
	}
	if d.ca, err = d.a.phase(ctx, domain.Established); err != nil {
		return data, err
	}
	time.Sleep(300 * time.Millisecond)
	if !rings[0].Closed() || !backs[0].Closed() {
		return data, fmt.Errorf("after the answer the ring is closed: %v, the ringback: %v", rings[0].Closed(), backs[0].Closed())
	}
	h := listen(time.Second, d.a, d.b)
	data["caller_heard"], data["callee_heard"] = h[0].share[toneB], h[1].share[toneA]
	if !h[0].has(toneB) || !h[1].has(toneA) {
		return data, errors.New("no audio after the answer")
	}
	for i, p := range []*phone{d.a, d.b} {
		if _, tones := p.sound.Last().Heard(engine.RingLowHz, engine.RingHighHz, engine.RingbackHz); tones[engine.RingLowHz]+tones[engine.RingHighHz]+tones[engine.RingbackHz] > 0.02 {
			return data, fmt.Errorf("a ring or a ringback is in the call's audio of phone %d: %v", i, tones)
		}
	}
	d.a.e.Send(domain.Hangup{Call: d.ca.ID})
	if _, err := d.a.ended(ctx, domain.EndNormal); err != nil {
		return data, err
	}
	if _, err := d.b.ended(ctx, domain.EndNormal); err != nil {
		return data, err
	}
	return data, stopAll(d.a, d.b)
}

// engineRingStops: a ring ends with the call it is for, whoever ends it: the caller who gives
// up, and the callee who refuses. With do-not-disturb on, nothing rings at all (task 8.3).
func engineRingStops(ctx context.Context, l *Lab) (map[string]any, error) {
	data := map[string]any{}
	for _, by := range []string{"the caller gives up", "the callee refuses"} {
		d, err := ring(ctx, l, "1001", "1002")
		if err != nil {
			d.close()
			return data, err
		}
		time.Sleep(500 * time.Millisecond)
		rings, backs := d.b.sound.OutputStreams(), d.a.sound.OutputStreams()
		if len(rings) != 1 || len(backs) != 1 || rings[0].Closed() || backs[0].Closed() {
			d.close()
			return data, fmt.Errorf("%s: nothing rings before it", by)
		}
		outgoing, err := d.a.snapshot(ctx, "the outgoing call", func(domain.CallSnapshot) bool { return true })
		if err != nil {
			d.close()
			return data, err
		}
		if by == "the caller gives up" {
			d.a.e.Send(domain.Hangup{Call: outgoing.ID})
		} else {
			d.b.e.Send(domain.Reject{Call: d.cb.ID})
		}
		for _, p := range []*phone{d.a, d.b} {
			if _, err := p.snapshot(ctx, "the end of the call", func(c domain.CallSnapshot) bool { return c.Phase.IsFinal() }); err != nil {
				d.close()
				return data, fmt.Errorf("%s: %w", by, err)
			}
		}
		time.Sleep(300 * time.Millisecond)
		data[by] = fmt.Sprintf("ring closed %v, ringback closed %v", rings[0].Closed(), backs[0].Closed())
		if !rings[0].Closed() || !backs[0].Closed() {
			d.close()
			return data, fmt.Errorf("%s: the ring is closed: %v, the ringback: %v", by, rings[0].Closed(), backs[0].Closed())
		}
		err = stopAll(d.a, d.b)
		d.close()
		if err != nil {
			return data, err
		}
	}
	// Do-not-disturb: the call is refused and no speaker is opened for it.
	b, err := l.phone(ctx, "1002", toneB)
	if err != nil {
		return data, err
	}
	defer b.close()
	a, err := l.phone(ctx, "1001", toneA)
	if err != nil {
		return data, err
	}
	defer a.close()
	b.e.Send(domain.SetDND{On: true})
	if err := a.send("1002"); err != nil {
		return data, err
	}
	if _, err := a.ended(ctx, domain.EndBusy); err != nil {
		return data, err
	}
	data["do-not-disturb"] = fmt.Sprintf("%d speakers opened by the callee", len(b.sound.OutputStreams()))
	if len(b.sound.OutputStreams()) != 0 {
		return data, errors.New("something rang with do-not-disturb on")
	}
	return data, stopAll(a, b)
}

// engineEarlyMedia: what the PBX plays before it answers is heard while the call rings, in place
// of the ringback (the call's sound, microphone and speaker, is open for it; what reaches the PBX
// before the answer, frames of silence, this scenario does not see); and the call ends without
// ever having been answered.
func engineEarlyMedia(ctx context.Context, l *Lab) (map[string]any, error) {
	p, err := l.phone(ctx, "1001", toneA)
	if err != nil {
		return nil, err
	}
	defer p.close()
	if err := p.send("603"); err != nil {
		return nil, err
	}
	if _, err := p.phase(ctx, domain.Ringing); err != nil {
		return nil, err
	}
	data := map[string]any{}
	for deadline := time.Now().Add(6 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if p.sound.CallStreams() == 1 {
			frames, share := p.sound.Last().Heard(toneLab)
			data["frames_before_the_answer"], data["with_the_pbx_tone"] = frames, share[toneLab]
			if frames >= 100 && share[toneLab] > 0.9 {
				break
			}
		}
		if time.Now().After(deadline) {
			return data, fmt.Errorf("the PBX's tone was not heard while the call rang: %v", data)
		}
	}
	for _, back := range p.sound.OutputStreams() {
		if !back.Closed() {
			return data, fmt.Errorf("the ringback plays beside the PBX's audio")
		}
	}
	end, err := p.snapshot(ctx, "the call given up by the PBX", func(c domain.CallSnapshot) bool { return c.Phase.IsFinal() })
	if err != nil {
		return data, err
	}
	data["end"] = end.Phase.End.String()
	if end.AnsweredMS != 0 {
		return data, fmt.Errorf("the call that was never answered has an answer time")
	}
	return data, nil
}

// engineAnsweredElsewhere: the PBX rings two phones of a group; one answers, and the other is told
// that the call was answered elsewhere, not that the caller gave up.
func engineAnsweredElsewhere(ctx context.Context, l *Lab) (map[string]any, error) {
	a, err := l.phone(ctx, "1001", toneA)
	if err != nil {
		return nil, err
	}
	defer a.close()
	b, err := l.phone(ctx, "1002", toneB)
	if err != nil {
		return nil, err
	}
	defer b.close()
	caller, err := l.phone(ctx, "1101", toneLab)
	if err != nil {
		return nil, err
	}
	defer caller.close()
	if err := caller.send("604"); err != nil {
		return nil, err
	}
	if _, err := a.phase(ctx, domain.Ringing); err != nil {
		return nil, err
	}
	ringing, err := b.phase(ctx, domain.Ringing)
	if err != nil {
		return nil, err
	}
	b.e.Send(domain.Answer{Call: ringing.ID})
	if _, err := b.phase(ctx, domain.Established); err != nil {
		return nil, err
	}
	end, err := a.snapshot(ctx, "the end of the call on the phone that did not answer", func(c domain.CallSnapshot) bool { return c.Phase.IsFinal() })
	if err != nil {
		return nil, err
	}
	data := map[string]any{"the_other_phone_ended_as": end.Phase.End.String()}
	if end.Phase.End != domain.EndAnsweredElsewhere {
		return data, fmt.Errorf("the phone that did not answer ended the call as %s, want answered_elsewhere", end.Phase.End)
	}
	if _, err := caller.phase(ctx, domain.Established); err != nil {
		return data, err
	}
	return data, nil
}
