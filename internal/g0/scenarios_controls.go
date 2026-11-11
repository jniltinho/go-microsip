package g0

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// ControlScenarios are the probes of design D3 criteria 3 to 5 that go beyond a plain call: DTMF,
// hold, blind transfer, recovery after a dropped transport, and the refusals a softphone owes.
func ControlScenarios() []Scenario {
	list := []Scenario{
		{"dtmf/1001", dtmf("1001", "123", toneLab)},
		{"dtmf/1201", dtmf("1201", "123", toneLab)},
		{"dtmf/wrong-digits-control", dtmf("1001", "999", toneReject)},
		{"hold/1001-1002", hold("1001", "1002")},
		{"hold/1201-1202", hold("1201", "1202")},
		{"transfer/1001-1002", transfer("1001", "1002")},
		{"transfer/1201-1202", transfer("1201", "1202")},
		{"reregister/1101", reregister("1101")},
		{"reregister/1201", reregister("1201")},
		{"secure/plain-offer-refused-488", plainOfferRefused},
		{"secure/tls-account-has-no-listener", tlsAccountListensOnNothing},
		{"peer/sip-foreign-source-dropped", foreignInvite},
	}
	return list
}

// call sets up caller -> callee through the PBX and returns both legs.
type legs struct {
	a, b       *Endpoint
	da         *diago.DialogClientSession
	db         *diago.DialogServerSession
	medA, medB *diago.DialogMedia
}

func (c *legs) close() {
	if c.da != nil {
		hangup(c.da, c.medA)
	}
	if c.db != nil {
		hangup(c.db, c.medB)
	}
	if c.a != nil {
		c.a.Close()
	}
	if c.b != nil {
		c.b.Close()
	}
}

func connect(ctx context.Context, l *Lab, caller, callee string) (*legs, error) {
	c := &legs{}
	var err error
	if c.b, err = l.Up(ctx, l.Account(callee, media.CodecAudioAlaw)); err != nil {
		return c, err
	}
	if c.a, err = l.Up(ctx, l.Account(caller, media.CodecAudioAlaw)); err != nil {
		return c, err
	}
	type answered struct {
		d   *diago.DialogServerSession
		med *diago.DialogMedia
		err error
	}
	answer := make(chan answered, 1)
	go func() {
		select {
		case in := <-c.b.Incoming:
			_ = in.Ringing()
			med, err := in.Answer(diago.AnswerOptions{})
			answer <- answered{in, med, err}
		case <-ctx.Done():
			answer <- answered{err: errors.New("no incoming call reached the callee")}
		}
	}()
	if c.da, c.medA, err = c.a.Dial(ctx, callee); err != nil {
		return c, fmt.Errorf("dial %s: %w", callee, err)
	}
	in := <-answer
	if in.err != nil {
		return c, fmt.Errorf("answer: %w", in.err)
	}
	c.db, c.medB = in.d, in.med
	return c, nil
}

// dtmf dials the lab's digit reader, sends RFC 4733 digits and checks which tone the PBX plays:
// 1000 Hz when it read "123", 400 Hz otherwise.
func dtmf(user, digits string, want float64) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		e, err := l.Up(ctx, l.Account(user, media.CodecAudioAlaw))
		if err != nil {
			return nil, err
		}
		defer e.Close()
		d, med, err := e.Dial(ctx, "601")
		if err != nil {
			return nil, fmt.Errorf("dial 601: %w", err)
		}
		defer hangup(d, med)
		w, err := med.AudioWriterDTMF()
		if err != nil {
			return nil, err
		}
		// A softphone sends audio all the time; the digits interleave with it, which also keeps the
		// RTP timestamp moving between events.
		sctx, stop := context.WithCancel(ctx)
		defer stop()
		go func() { _ = SendToneTo(sctx, med, w, toneA) }()
		time.Sleep(600 * time.Millisecond)
		for _, digit := range digits {
			if err := w.WriteDTMF(digit); err != nil {
				return nil, fmt.Errorf("send digit: %w", err)
			}
			time.Sleep(250 * time.Millisecond)
		}
		h, err := Listen(ctx, med, 2500*time.Millisecond, toneLab, toneReject)
		if err != nil {
			return nil, err
		}
		data := map[string]any{"digits": len(digits), "accepted_tone": h.Share[toneLab], "rejected_tone": h.Share[toneReject], "frames": h.Frames, "proto": mediaProto(med)}
		if !h.Has(want) {
			return data, fmt.Errorf("the PBX did not play the %v Hz tone", want)
		}
		return data, nil
	}
}

// hold puts the caller's leg on hold and resumes it: the callee hears the caller's tone, then
// does not, then does again.
func hold(caller, callee string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		c, err := connect(ctx, l, caller, callee)
		defer c.close()
		if err != nil {
			return nil, err
		}
		// Both sides talk. While the call is held the holder's microphone is not sent: the library
		// keeps the stream open (sendonly), so stopping the audio is the engine's duty, done here by
		// pausing the caller's sender exactly as the engine will.
		sctx, stop := context.WithCancel(ctx)
		defer stop()
		var held atomic.Bool
		go func() { _ = sendToneGated(sctx, c.medA, toneA, &held) }()
		go func() { _ = SendTone(sctx, c.medB, toneB) }()
		listen := func() (a, b Heard, err error) {
			done := make(chan Heard, 1)
			go func() {
				h, _ := Listen(ctx, c.medA, 1500*time.Millisecond, toneB)
				done <- h
			}()
			b, err = Listen(ctx, c.medB, 1500*time.Millisecond, toneA)
			return <-done, b, err
		}
		beforeA, beforeB, err := listen()
		if err != nil {
			return nil, err
		}
		held.Store(true)
		if err := c.da.Hold(ctx); err != nil {
			return nil, fmt.Errorf("hold: %w", err)
		}
		holdMode := c.medA.MediaSession().Mode
		duringA, duringB, err := listen()
		if err != nil {
			return nil, err
		}
		if err := c.da.Unhold(ctx); err != nil {
			return nil, fmt.Errorf("resume: %w", err)
		}
		held.Store(false)
		afterA, afterB, err := listen()
		if err != nil {
			return nil, err
		}
		data := map[string]any{
			"holder_heard": []float64{beforeA.Share[toneB], duringA.Share[toneB], afterA.Share[toneB]},
			"held_heard":   []float64{beforeB.Share[toneA], duringB.Share[toneA], afterB.Share[toneA]},
			"hold_mode":    holdMode, "resume_mode": c.medA.MediaSession().Mode, "proto": mediaProto(c.medA),
		}
		switch {
		case !beforeA.Has(toneB) || !beforeB.Has(toneA):
			return data, errors.New("no two-way audio before the hold")
		case holdMode != "sendonly":
			return data, fmt.Errorf("the hold negotiated %q, want sendonly", holdMode)
		case duringA.Has(toneB):
			return data, errors.New("the holder still received the other party during the hold")
		case duringB.Has(toneA):
			return data, errors.New("the held party still heard the holder during the hold")
		case !afterA.Has(toneB) || !afterB.Has(toneA):
			return data, errors.New("two-way audio did not come back after the resume")
		}
		return data, nil
	}
}

// transfer makes a blind transfer: the caller refers the callee to the lab's tone extension, the
// PBX reports success with NOTIFY, and the callee hears the tone of the new destination.
func transfer(caller, callee string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		c, err := connect(ctx, l, caller, callee)
		defer c.close()
		if err != nil {
			return nil, err
		}
		// The call must be bridged before it can be transferred: prove audio first.
		sctx, stop := context.WithCancel(ctx)
		defer stop()
		go func() { _ = SendTone(sctx, c.medA, toneA) }()
		before, err := Listen(ctx, c.medB, 1200*time.Millisecond, toneA)
		if err != nil {
			return nil, err
		}
		if !before.Has(toneA) {
			return nil, errors.New("no audio before the transfer")
		}
		target := sip.Uri{User: "602", Host: c.a.Account.Server, Port: c.a.Account.Port}
		notified := make(chan int, 8)
		err = c.da.ReferOptions(ctx, target, diago.ReferClientOptions{
			Headers:  []sip.Header{},
			OnNotify: func(code int) { notified <- code },
		})
		if err != nil {
			return nil, fmt.Errorf("refer: %w", err)
		}
		final := 0
		for final < 200 {
			select {
			case final = <-notified:
			case <-time.After(8 * time.Second):
				return nil, errors.New("the PBX never reported the transfer result")
			}
		}
		h, err := Listen(ctx, c.medB, 3*time.Second, toneLab)
		if err != nil {
			return nil, err
		}
		data := map[string]any{"notify": final, "callee_heard": h.Share[toneLab], "proto": mediaProto(c.medB)}
		if final != 200 {
			return data, fmt.Errorf("the transfer failed with %d", final)
		}
		if !h.Has(toneLab) {
			return data, errors.New("the callee did not hear the transfer destination")
		}
		return data, nil
	}
}

// reregister loses the transport connection, registers again and proves the account works by
// making a call on the new connection.
func reregister(user string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		e, err := l.Up(ctx, l.Account(user, media.CodecAudioAlaw))
		if err != nil {
			return nil, err
		}
		defer e.Close()
		if err := e.DropTransport(); err != nil {
			return nil, fmt.Errorf("drop transport: %w", err)
		}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := e.Register(rctx); err != nil {
			return nil, fmt.Errorf("register after the drop: %w", err)
		}
		d, med, err := e.Dial(ctx, "602")
		if err != nil {
			return nil, fmt.Errorf("dial after the drop: %w", err)
		}
		defer hangup(d, med)
		h, err := Listen(ctx, med, 2*time.Second, toneLab)
		if err != nil {
			return nil, err
		}
		if !h.Has(toneLab) {
			return nil, errors.New("no audio on the call after the drop")
		}
		return map[string]any{"heard": h.Share[toneLab]}, nil
	}
}

// plainOfferRefused: 1302 is a TLS endpoint for which the PBX offers plain RTP. Configured here as
// a secure-media account, it must answer 488 and never open audio.
func plainOfferRefused(ctx context.Context, l *Lab) (map[string]any, error) {
	secure := l.Account("1302", media.CodecAudioAlaw)
	secure.SecureMedia = true
	b, err := l.Up(ctx, secure)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	a, err := l.Up(ctx, l.Account("1301", media.CodecAudioAlaw))
	if err != nil {
		return nil, err
	}
	defer a.Close()
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d, med, err := a.Dial(dctx, "1302")
	if err == nil {
		hangup(d, med)
		return nil, errors.New("the call was answered; a plain offer to a secure account must be refused")
	}
	data := map[string]any{"refused_offers": b.RefusedPlainOffers.Load(), "caller_saw": firstLine(err.Error())}
	var refused *sipgo.ErrDialogResponse
	if !errors.As(err, &refused) || refused.Res.StatusCode != sip.StatusNotAcceptableHere {
		return data, fmt.Errorf("the caller saw %q, want the status 488", firstLine(err.Error()))
	}
	if b.RefusedPlainOffers.Load() != 1 {
		return data, fmt.Errorf("the secure account refused %d offers, want 1 (caller: %v)", b.RefusedPlainOffers.Load(), err)
	}
	select {
	case <-b.Incoming:
		return data, errors.New("the refused call still reached the application")
	default:
	}
	return data, nil
}

// tlsAccountListensOnNothing: a registered TLS account has no listening SIP socket of any kind and
// no UDP socket at all while idle; calls arrive on the connection it opened.
func tlsAccountListensOnNothing(ctx context.Context, l *Lab) (map[string]any, error) {
	e, err := l.Up(ctx, l.Account("1201", media.CodecAudioAlaw))
	if err != nil {
		return nil, err
	}
	defer e.Close()
	listening := 0
	for _, network := range []string{"udp", "tcp", "tls", "ws", "wss"} {
		listening += len(e.ListenPorts(network))
	}
	udp, err := boundUDPSockets()
	if err != nil {
		return nil, err
	}
	data := map[string]any{"sip_listeners": listening, "udp_sockets": udp}
	if listening != 0 {
		return data, fmt.Errorf("the TLS account has %d SIP listeners", listening)
	}
	if udp != 0 {
		return data, fmt.Errorf("the idle TLS account has %d UDP sockets", udp)
	}
	return data, nil
}

// boundUDPSockets counts the UDP sockets of this network namespace (the runner is alone in it).
func boundUDPSockets() (int, error) {
	n := 0
	for _, file := range []string{"/proc/net/udp", "/proc/net/udp6"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			return 0, err
		}
		for i, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			// Skip the header and Docker's embedded resolver, which keeps a socket on 127.0.0.11
			// inside every container's network namespace.
			if f := strings.Fields(line); i > 0 && len(f) > 1 && !strings.HasPrefix(f[1], "0B00007F:") {
				n++
			}
		}
	}
	return n, nil
}

// foreignInvite sends an INVITE to a UDP account from an address that is not the registrar. With
// the filter off (control) the call reaches the application; with it on, nothing does.
func foreignInvite(ctx context.Context, l *Lab) (map[string]any, error) {
	try := func(acceptAny bool) (delivered bool, refused uint64, err error) {
		a := l.Account("1001", media.CodecAudioAlaw)
		e, err := NewEndpoint(a, l.Log)
		if err != nil {
			return false, 0, err
		}
		defer e.Close()
		e.AcceptAnySource = acceptAny
		if err := e.Serve(ctx); err != nil {
			return false, 0, err
		}
		rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := e.Register(rctx); err != nil {
			return false, 0, err
		}
		probe, err := net.Dial("udp", net.JoinHostPort(a.Server, "5060"))
		if err != nil {
			return false, 0, err
		}
		local := probe.LocalAddr().(*net.UDPAddr).IP
		probe.Close()
		intruder, err := net.ListenUDP("udp", &net.UDPAddr{IP: local})
		if err != nil {
			return false, 0, err
		}
		defer intruder.Close()
		from := intruder.LocalAddr().(*net.UDPAddr)
		ports := e.ListenPorts("udp")
		if len(ports) == 0 {
			return false, 0, errors.New("the UDP account has no socket to attack")
		}
		for _, p := range ports {
			body := fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 %[1]s\r\ns=-\r\nc=IN IP4 %[1]s\r\nt=0 0\r\nm=audio 40000 RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n", local)
			msg := fmt.Sprintf("INVITE sip:1001@%[1]s:%[2]d SIP/2.0\r\n"+
				"Via: SIP/2.0/UDP %[1]s:%[3]d;branch=z9hG4bK-g0-intruder-%[2]d;rport\r\n"+
				"From: <sip:intruder@intruder.invalid>;tag=g0\r\nTo: <sip:1001@%[1]s>\r\n"+
				"Call-ID: g0-intruder-%[2]d\r\nCSeq: 1 INVITE\r\nMax-Forwards: 70\r\n"+
				"Contact: <sip:intruder@%[1]s:%[3]d>\r\nContent-Type: application/sdp\r\n"+
				"Content-Length: %[4]d\r\n\r\n%[5]s", local, p, from.Port, len(body), body)
			if _, err := intruder.WriteTo([]byte(msg), &net.UDPAddr{IP: local, Port: p}); err != nil {
				return false, 0, err
			}
		}
		select {
		case in := <-e.Incoming:
			_ = in.Respond(sip.StatusBusyHere, "Busy Here", nil)
			_ = in.Close()
			delivered = true
		case <-time.After(1500 * time.Millisecond):
		}
		return delivered, e.RefusedSources.Load(), nil
	}
	control, _, err := try(true)
	if err != nil {
		return nil, fmt.Errorf("control: %w", err)
	}
	delivered, refused, err := try(false)
	if err != nil {
		return nil, err
	}
	data := map[string]any{"control_delivered": control, "delivered": delivered, "refused_requests": refused}
	switch {
	case !control:
		return data, errors.New("control failed: without the filter the foreign INVITE was not delivered, so the probe proves nothing")
	case delivered:
		return data, errors.New("a foreign INVITE reached the application")
	case refused == 0:
		return data, errors.New("the foreign INVITE was not counted as refused")
	}
	return data, nil
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
