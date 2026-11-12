package app

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/engine"
	"github.com/jniltinho/go-microsip/internal/core/media"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// soon waits for what a running sound test shows a moment after it started.
func soon(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no %s within 5 s", what)
		}
	}
}

// The settings' sound test, with no call and no account: the tone goes to the speaker chosen in
// the settings and the level is the chosen microphone's; a second start opens nothing more, and
// the stop releases the devices.
func TestTheAudioTestPlaysOnTheChosenSpeakerAndShowsTheChosenMicrophone(t *testing.T) {
	dir := t.TempDir()
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	sound := &memory.Audio{Tone: 1000} // its microphone hears a tone at 12000 of 32767
	p, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	mic, speaker := "headset-mic", "headset-speaker"
	if err := p.SaveSettings(SettingsPatch{AudioInput: &mic, AudioOutput: &speaker}); err != nil {
		t.Fatal(err)
	}
	if running, level := p.AudioTest(); running || level != 0 {
		t.Fatalf("before any test: running %v, level %v", running, level)
	}
	if err := p.StartAudioTest(); err != nil {
		t.Fatal(err)
	}
	if err := p.StartAudioTest(); err != nil {
		t.Fatalf("a second start: %v", err)
	}
	if n := sound.OpenStreams(); n != 1 {
		t.Fatalf("%d streams open after two starts, want the one test", n)
	}
	s := sound.Last()
	if s.Input != mic || s.Output != speaker {
		t.Errorf("the test opened %q and %q, want the saved %q and %q", s.Input, s.Output, mic, speaker)
	}
	soon(t, "half a second of the tone", func() bool { frames, _ := s.Heard(); return frames >= 25 })
	if _, share := s.Heard(AudioTestHz); share[AudioTestHz] < 0.95 {
		t.Errorf("the speaker got the test's tone in %.0f %% of its frames", 100*share[AudioTestHz])
	}
	if running, level := p.AudioTest(); !running || level < 0.33 || level > 0.40 {
		t.Errorf("during the test: running %v, level %.2f, want the microphone's 0.37", running, level)
	}
	p.StopAudioTest()
	if running, level := p.AudioTest(); running || level != 0 || !s.Closed() {
		t.Errorf("after the stop: running %v, level %v, devices released %v", running, level, s.Closed())
	}
}

// A call takes the sound: a running test stops when a call begins, none starts while the call
// lasts, and one can start again when it is over.
func TestTheAudioTestGivesWayToACall(t *testing.T) {
	o, _ := options(t, ringingRegistrar(t))
	sound := &memory.Audio{Tone: 1000}
	p, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	if err := p.StartAudioTest(); err != nil {
		t.Fatal(err)
	}
	test := sound.Last()
	target, _ := domain.ParseDialTarget("1002")
	if err := p.Send(domain.Dial{Target: target}); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the call ringing", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.Kind == domain.Ringing
	})
	if running, _ := p.AudioTest(); running || !test.Closed() {
		t.Errorf("with a call ringing the test is running: %v, its devices released: %v", running, test.Closed())
	}
	var busy Refused
	if err := p.StartAudioTest(); !errors.As(err, &busy) || err.Error() != "busy" {
		t.Errorf("a test started during a call: %v, want busy", err)
	}
	if running, _ := p.AudioTest(); running {
		t.Error("the refused test is running")
	}
	if err := p.Send(domain.Hangup{Call: 1}); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the call's end", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.IsFinal()
	})
	if err := p.StartAudioTest(); err != nil {
		t.Errorf("a test after the call: %v", err)
	}
}

// A device chosen in the settings is the next call's, with no restart: the call that is up keeps
// the devices it began with, and the one after it opens the new ones, for its sound and for its
// ringback.
func TestDevicesChosenBetweenCallsAreTheNextCalls(t *testing.T) {
	sound := &memory.Audio{Tone: 1000}
	call := func(p *Phone, id domain.CallID) *memory.Stream {
		t.Helper()
		target, _ := domain.ParseDialTarget("1002")
		if err := p.Send(domain.Dial{Target: target}); err != nil {
			t.Fatal(err)
		}
		until(t, p, "the call answered", func(ev domain.Event) bool {
			c, ok := ev.(domain.CallChanged)
			return ok && c.Call.ID == id && c.Call.Phase.Kind == domain.Established
		})
		soon(t, "sound of the call", func() bool { return sound.CallStreams() == int(id) })
		return sound.Last()
	}
	hangUp := func(p *Phone, id domain.CallID) {
		t.Helper()
		if err := p.Send(domain.Hangup{Call: id}); err != nil {
			t.Fatal(err)
		}
		until(t, p, "the call's end", func(ev domain.Event) bool {
			c, ok := ev.(domain.CallChanged)
			return ok && c.Call.ID == id && c.Call.Phase.IsFinal()
		})
		soon(t, "the call's sound released", func() bool { return sound.OpenStreams() == 0 })
	}
	o, _ := options(t, lab(t, 200))
	p, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	first := call(p, 1)
	if first.Input != "" || first.Output != "" {
		t.Errorf("the first call opened %q and %q, want the system's devices", first.Input, first.Output)
	}
	// Chosen during the call: saved, and not this call's.
	mic, speaker := "headset-mic", "headset-speaker"
	if err := p.SaveSettings(SettingsPatch{AudioInput: &mic, AudioOutput: &speaker}); err != nil {
		t.Fatal(err)
	}
	if n := sound.CallStreams(); n != 1 || sound.Last() != first || first.Closed() {
		t.Errorf("after the change the call has %d streams, its first one closed: %v", n, first.Closed())
	}
	hangUp(p, 1)
	second := call(p, 2)
	if second.Input != mic || second.Output != speaker {
		t.Errorf("the call after the change opened %q and %q, want %q and %q", second.Input, second.Output, mic, speaker)
	}
	hangUp(p, 2)

	// The ringback of a call that only rings goes to the chosen speaker too.
	o, _ = options(t, ringingRegistrar(t))
	sound = &memory.Audio{Tone: 1000}
	ringing, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	defer ringing.Close()
	until(t, ringing, "registration", registered)
	if err := ringing.SaveSettings(SettingsPatch{AudioOutput: &speaker}); err != nil {
		t.Fatal(err)
	}
	target, _ := domain.ParseDialTarget("1002")
	if err := ringing.Send(domain.Dial{Target: target}); err != nil {
		t.Fatal(err)
	}
	soon(t, "ringback", func() bool { return len(sound.OutputStreams()) == 1 })
	if out := sound.OutputStreams()[0].Output; out != speaker {
		t.Errorf("the ringback opened %q, want the chosen %q", out, speaker)
	}
}

// A PBX that answers 183 Session Progress, as a trunk does for a mobile number, is a call on its
// way like one that answers 180: the ringback sounds, and goes on until the call ends.
func TestTheRingbackSoundsThroughSessionProgress(t *testing.T) {
	o, _ := options(t, lab(t, 183))
	sound := &memory.Audio{Tone: 1000}
	p, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	target, _ := domain.ParseDialTarget("1002")
	if err := p.Send(domain.Dial{Target: target}); err != nil {
		t.Fatal(err)
	}
	soon(t, "ringback", func() bool { return len(sound.OutputStreams()) == 1 })
	back := sound.OutputStreams()[0]
	soon(t, "a second of the ringback", func() bool { frames, _ := back.Heard(); return frames >= 50 })
	if _, share := back.Heard(engine.RingbackHz); share[engine.RingbackHz] < 0.5 || back.Closed() {
		t.Errorf("after 183 the speaker got the ringback in %.0f %% of its first second, closed: %v", 100*share[engine.RingbackHz], back.Closed())
	}
	if err := p.Send(domain.Hangup{Call: 1}); err != nil {
		t.Fatal(err)
	}
	soon(t, "the ringback's end", back.Closed)
}

// A call whose sound cannot be opened stays up and says why: a microphone that the operating
// system denies is told apart from sound devices that are not there, so the user is sent to the
// setting that grants it.
func TestACallWithoutSoundSaysWhy(t *testing.T) {
	for _, c := range []struct {
		sound *memory.Audio
		want  domain.Notice
	}{
		{&memory.Audio{Denied: true}, domain.NoticeMicrophoneDenied},
		{&memory.Audio{Unavailable: true}, domain.NoticeAudioUnavailable},
	} {
		o, _ := options(t, lab(t, 200))
		p, err := OpenPhone(o, PhoneOptions{Audio: c.sound})
		if err != nil {
			t.Fatal(err)
		}
		until(t, p, "registration", registered)
		target, _ := domain.ParseDialTarget("1002")
		if err := p.Send(domain.Dial{Target: target}); err != nil {
			t.Fatal(err)
		}
		established := false
		until(t, p, "the notice "+c.want.String(), func(ev domain.Event) bool {
			if call, ok := ev.(domain.CallChanged); ok {
				established = call.Call.Phase.Kind == domain.Established
			}
			n, ok := ev.(domain.Noticed)
			return ok && n.Notice == c.want
		})
		if !established {
			t.Errorf("%s: the call is not up", c.want)
		}
		p.Close()
	}
}

// What the softphone says of itself in its requests: the Contact address carries the extension,
// as a PBX and whatever inspects SIP on the way to it expect, and the product is in User-Agent.
func TestTheSoftphoneNamesItsExtensionInContactAndItsProductInUserAgent(t *testing.T) {
	port := ringingRegistrar(t)
	o, _ := options(t, port)
	p, err := OpenPhone(o, PhoneOptions{UserAgent: "go-microsip/test"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	target, _ := domain.ParseDialTarget("1002")
	if err := p.Send(domain.Dial{Target: target}); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the call ringing", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.Kind == domain.Ringing
	})
	for _, method := range []string{"REGISTER", "INVITE"} {
		got, _ := sent.Load(fmt.Sprintf("%d %s", port, method))
		if want := method + " contact 1001 product go-microsip/test"; got != want {
			t.Errorf("the softphone sent %q, want %q", got, want)
		}
	}
}

// earlyLab is a PBX that plays audio of its own before it answers, as a trunk does while a mobile
// number rings: 183 Session Progress with a description of its audio, a 1 kHz tone sent to the
// softphone for a second and a half, then the answer. It counts what the softphone sends to its
// audio port.
func earlyLab(t *testing.T) (port int, sentBack *atomic.Int64, answered chan struct{}) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	port = probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	rtp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rtp.Close() })
	sentBack, answered = new(atomic.Int64), make(chan struct{})
	go func() {
		buf := make([]byte, 2048)
		for {
			if _, _, err := rtp.ReadFromUDP(buf); err != nil {
				return
			}
			sentBack.Add(1)
		}
	}()
	ua, err := sipgo.NewUA()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	srv.OnRegister(func(req *gosip.Request, tx gosip.ServerTransaction) {
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	srv.OnInvite(func(req *gosip.Request, tx gosip.ServerTransaction) {
		sdp := fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=fmtp:101 0-16\r\na=sendrecv\r\n", rtp.LocalAddr().(*net.UDPAddr).Port)
		with := func(code int, reason string) *gosip.Response {
			res := gosip.NewResponseFromRequest(req, code, reason, []byte(sdp))
			res.AppendHeader(gosip.NewHeader("Content-Type", "application/sdp"))
			res.AppendHeader(gosip.NewHeader("Contact", fmt.Sprintf("<sip:lab@%s;transport=tcp>", addr)))
			return res
		}
		_ = tx.Respond(with(183, "Session Progress"))
		// Where the softphone listens for audio, from its offer.
		var to net.UDPAddr
		for _, line := range strings.Split(string(req.Body()), "\r\n") {
			if rest, ok := strings.CutPrefix(line, "c=IN IP4 "); ok {
				to.IP = net.ParseIP(rest)
			}
			if rest, ok := strings.CutPrefix(line, "m=audio "); ok {
				_, _ = fmt.Sscanf(rest, "%d", &to.Port)
			}
		}
		go func() {
			var frame [media.FrameSamples]int16
			packet := make([]byte, 12+media.FrameSamples)
			tick := time.NewTicker(20 * time.Millisecond)
			defer tick.Stop()
			for n := 0; n < 300; n++ { // six seconds: through the answer and after it
				for i := range frame {
					frame[i] = int16(12000 * math.Sin(2*math.Pi*1000*float64(n*media.FrameSamples+i)/8000))
				}
				packet[0], packet[1] = 0x80, 0
				binary.BigEndian.PutUint16(packet[2:], uint16(n))
				binary.BigEndian.PutUint32(packet[4:], uint32(n*media.FrameSamples))
				binary.BigEndian.PutUint32(packet[8:], 0x1234)
				media.PCMU.Encode(packet[12:], &frame)
				if _, err := rtp.WriteToUDP(packet, &to); err != nil {
					return
				}
				<-tick.C
			}
		}()
		time.Sleep(1500 * time.Millisecond)
		close(answered)
		_ = tx.Respond(with(200, "OK"))
	})
	srv.OnAck(func(*gosip.Request, gosip.ServerTransaction) {})
	srv.OnBye(func(req *gosip.Request, tx gosip.ServerTransaction) {
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	srv.OnCancel(func(req *gosip.Request, tx gosip.ServerTransaction) {
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ua.Close() })
	ready := make(chan struct{})
	go func() {
		_ = srv.ListenAndServe(context.WithValue(ctx, sipgo.ListenReadyCtxKey, sipgo.ListenReadyCtxValue(ready)), "tcp", addr)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the PBX did not start")
	}
	return port, sentBack, answered
}

// What a PBX plays before it answers (early media) is heard: the speaker plays it in place of
// the ringback while the call still rings, nothing is sent to the PBX before the answer, and
// from the answer on the same audio goes both ways.
func TestAudioBeforeTheAnswerIsPlayedAndNothingIsSent(t *testing.T) {
	port, sentBack, answered := earlyLab(t)
	o, _ := options(t, port)
	sound := &memory.Audio{Tone: 440}
	p, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	target, _ := domain.ParseDialTarget("1002")
	if err := p.Send(domain.Dial{Target: target}); err != nil {
		t.Fatal(err)
	}
	soon(t, "sound of the ringing call", func() bool { return sound.CallStreams() == 1 })
	var call *memory.Stream
	soon(t, "half a second of the PBX's audio on the speaker", func() bool {
		call = sound.Last()
		frames, _ := call.Heard()
		return frames >= 25
	})
	select {
	case <-answered:
		t.Fatal("the PBX answered before its early audio was heard: the test proved nothing")
	default:
	}
	if _, share := call.Heard(1000); share[1000] < 0.9 {
		t.Errorf("before the answer the speaker got the PBX's tone in %.0f %% of its frames", 100*share[1000])
	}
	if n := sentBack.Load(); n != 0 {
		t.Errorf("%d packets were sent to the PBX's audio before it answered", n)
	}
	for _, back := range sound.OutputStreams() {
		if !back.Closed() {
			t.Error("the ringback still plays beside the PBX's audio")
		}
	}
	until(t, p, "the answer", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.Kind == domain.Established
	})
	soon(t, "the microphone's audio at the PBX after the answer", func() bool { return sentBack.Load() >= 25 })
	if sound.CallStreams() != 1 || call.Closed() {
		t.Errorf("after the answer the call has %d streams, its first one closed: %v", sound.CallStreams(), call.Closed())
	}
}
