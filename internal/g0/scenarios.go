package g0

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
)

// Tones used by the probes: the caller's, the callee's, and the two the lab dialplan plays.
const (
	toneA      = 700.0
	toneB      = 1300.0
	toneLab    = 1000.0
	toneReject = 400.0
)

var codecs = []media.Codec{media.CodecAudioAlaw, media.CodecAudioUlaw}

// Scenarios lists every G0 probe, in the order of design D3's criteria.
func Scenarios() []Scenario {
	var list []Scenario
	for _, user := range []string{"1001", "1101", "1201", "1301"} {
		list = append(list, Scenario{"register/" + user, register(user)})
	}
	list = append(list,
		Scenario{"tls/unknown-ca-refused", tlsUnknownCA},
		Scenario{"tls/wrong-host-refused", tlsWrongHost},
		Scenario{"register/wrong-password-refused", wrongPassword},
	)
	for _, user := range []string{"1001", "1101", "1301", "1201"} {
		for _, c := range codecs {
			list = append(list, Scenario{fmt.Sprintf("echo/%s/%s", user, c.Name), echoCall(user, c)})
		}
	}
	for _, pair := range [][2]string{{"1001", "1002"}, {"1101", "1102"}, {"1301", "1302"}, {"1201", "1202"}} {
		for _, c := range codecs {
			list = append(list, Scenario{fmt.Sprintf("pair/%s-%s/%s", pair[0], pair[1], c.Name), pairCall(pair[0], pair[1], c)})
		}
	}
	return append(append(list, ControlScenarios()...), EngineScenarios()...)
}

func register(user string) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		e, err := l.Up(ctx, l.Account(user, media.CodecAudioAlaw))
		if err != nil {
			return nil, err
		}
		defer e.Close()
		if err := e.Unregister(ctx); err != nil {
			return nil, fmt.Errorf("unregister: %w", err)
		}
		return map[string]any{"transport": e.Account.Transport}, nil
	}
}

// expectRefused registers an account that must be refused and checks why.
func expectRefused(ctx context.Context, l *Lab, a Account, reasons ...string) (map[string]any, error) {
	e, err := l.Up(ctx, a)
	if err == nil {
		e.Close()
		return nil, errors.New("the registration was accepted; it must be refused")
	}
	for _, r := range reasons {
		if strings.Contains(err.Error(), r) {
			return map[string]any{"refusal": r}, nil
		}
	}
	return nil, fmt.Errorf("refused for an unexpected reason: %w", err)
}

// A registrar certificate that does not chain to the configured bundle is refused.
func tlsUnknownCA(ctx context.Context, l *Lab) (map[string]any, error) {
	a := l.Account("1201", media.CodecAudioAlaw)
	a.CAFile = filepath.Join(l.Dir, "untrusted-ca.crt")
	return expectRefused(ctx, l, a, "certificate signed by unknown authority", "x509")
}

// A registrar reached under a name its certificate does not carry is refused. "pbx-alias" resolves
// to the lab PBX (a network alias), whose certificate names only "pbx".
func tlsWrongHost(ctx context.Context, l *Lab) (map[string]any, error) {
	a := l.Account("1201", media.CodecAudioAlaw)
	a.Server = "pbx-alias"
	return expectRefused(ctx, l, a, "certificate is valid for", "not pbx-alias")
}

func wrongPassword(ctx context.Context, l *Lab) (map[string]any, error) {
	a := l.Account("1001", media.CodecAudioAlaw)
	a.Password = "wrong-" + a.Password
	return expectRefused(ctx, l, a, "401", "403", "Unauthorized", "Forbidden")
}

// echoCall dials the lab's echo extension, sends a tone and expects to hear it back: audio leaves
// and arrives, with the negotiated codec, over the account's transport and media security.
func echoCall(user string, codec media.Codec) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		e, err := l.Up(ctx, l.Account(user, codec))
		if err != nil {
			return nil, err
		}
		defer e.Close()
		d, med, err := e.Dial(ctx, "600", nil)
		if err != nil {
			return nil, fmt.Errorf("dial 600: %w", err)
		}
		defer hangup(d, med)
		sctx, stop := context.WithCancel(ctx)
		defer stop()
		go func() { _ = SendTone(sctx, med, toneA) }()
		h, err := Listen(ctx, med, 3*time.Second, toneA, toneLab)
		if err != nil {
			return nil, fmt.Errorf("listen: %w", err)
		}
		data := callData(med, h, toneA)
		if !h.Has(toneA) {
			return data, errors.New("the tone sent to the echo did not come back")
		}
		return data, checkSecurity(e.Account, med)
	}
}

// pairCall registers two endpoints, calls one from the other through the PBX and measures a
// different tone in each direction: an outgoing call, an incoming call and two-way audio.
func pairCall(caller, callee string, codec media.Codec) func(context.Context, *Lab) (map[string]any, error) {
	return func(ctx context.Context, l *Lab) (map[string]any, error) {
		b, err := l.Up(ctx, l.Account(callee, codec))
		if err != nil {
			return nil, err
		}
		defer b.Close()
		a, err := l.Up(ctx, l.Account(caller, codec))
		if err != nil {
			return nil, err
		}
		defer a.Close()

		type answered struct {
			d   *diago.DialogServerSession
			med *diago.DialogMedia
			err error
		}
		answer := make(chan answered, 1)
		go func() {
			select {
			case in := <-b.Incoming:
				_ = in.Trying()
				_ = in.Ringing()
				med, err := in.Answer(diago.AnswerOptions{})
				answer <- answered{in, med, err}
			case <-ctx.Done():
				answer <- answered{err: errors.New("no incoming call reached the callee")}
			}
		}()
		da, medA, err := a.Dial(ctx, callee, nil)
		if err != nil {
			return nil, fmt.Errorf("dial %s: %w", callee, err)
		}
		defer hangup(da, medA)
		in := <-answer
		if in.err != nil {
			return nil, fmt.Errorf("answer: %w", in.err)
		}
		defer hangup(in.d, in.med)

		sctx, stop := context.WithCancel(ctx)
		defer stop()
		go func() { _ = SendTone(sctx, medA, toneA) }()
		go func() { _ = SendTone(sctx, in.med, toneB) }()
		heardB := make(chan Heard, 1)
		go func() {
			h, _ := Listen(ctx, in.med, 3*time.Second, toneA, toneB)
			heardB <- h
		}()
		hA, err := Listen(ctx, medA, 3*time.Second, toneA, toneB)
		if err != nil {
			return nil, fmt.Errorf("caller listen: %w", err)
		}
		hB := <-heardB
		data := map[string]any{
			"codec":         media.CodecAudioFromSession(medA.MediaSession()).Name,
			"caller_heard":  hA.Share[toneB],
			"callee_heard":  hB.Share[toneA],
			"caller_frames": hA.Frames,
			"callee_frames": hB.Frames,
			"caller_proto":  mediaProto(medA),
			"callee_proto":  mediaProto(in.med),
		}
		if !hA.Has(toneB) {
			return data, errors.New("the caller did not hear the callee's tone")
		}
		if !hB.Has(toneA) {
			return data, errors.New("the callee did not hear the caller's tone")
		}
		if err := checkSecurity(a.Account, medA); err != nil {
			return data, err
		}
		return data, checkSecurity(b.Account, in.med)
	}
}

func callData(med *diago.DialogMedia, h Heard, freq float64) map[string]any {
	return map[string]any{
		"codec":  media.CodecAudioFromSession(med.MediaSession()).Name,
		"heard":  h.Share[freq],
		"frames": h.Frames,
		"proto":  mediaProto(med),
	}
}

// mediaProto is the transport profile of the audio line in the local session description.
func mediaProto(med *diago.DialogMedia) string {
	for _, line := range strings.Split(string(med.MediaSession().LocalSDP(true)), "\n") {
		if strings.HasPrefix(line, "m=audio ") {
			if f := strings.Fields(line); len(f) > 2 {
				return f[2]
			}
		}
	}
	return ""
}

// checkSecurity fails when the negotiated media does not match what the account requires.
func checkSecurity(a Account, med *diago.DialogMedia) error {
	proto := mediaProto(med)
	if a.SecureMedia && proto != "RTP/SAVP" {
		return fmt.Errorf("a secure-media account negotiated %q", proto)
	}
	if !a.SecureMedia && proto != "RTP/AVP" {
		return fmt.Errorf("a plain-media account negotiated %q", proto)
	}
	return nil
}
