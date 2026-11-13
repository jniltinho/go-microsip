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
// softphone for two and a half seconds, then the answer, whose description differs from the
// 183's in what says nothing of the audio (the origin's version, the session's name). It counts
// what the softphone sends to its audio port, how much of that is anything but silence, and
// what both counts were when it answered. With garbage, the first thing it sends to the
// softphone's audio port is not an RTP packet.
type earlyPBX struct {
	port             int
	sentBack, loud   atomic.Int64
	before, loudThen atomic.Int64 // the two counts at the answer
	answered         chan struct{}
}

func earlyLab(t *testing.T, garbage bool) *earlyPBX {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	pbx := &earlyPBX{port: probe.Addr().(*net.TCPAddr).Port, answered: make(chan struct{})}
	sentBack, loud, answered := &pbx.sentBack, &pbx.loud, pbx.answered
	probe.Close()
	rtp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rtp.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, _, err := rtp.ReadFromUDP(buf)
			if err != nil {
				return
			}
			sentBack.Add(1)
			if n > 12 && strings.Trim(string(buf[12:n]), "\xff") != "" { // PCMU silence is 0xff
				loud.Add(1)
			}
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
		with := func(code int, reason string) *gosip.Response {
			// The same audio in both; the answer has another origin version and session name,
			// its attributes in another order and one of them twice.
			attributes := "a=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=fmtp:101 0-16\r\na=sendrecv\r\n"
			if code == 200 {
				attributes = "a=sendrecv\r\na=fmtp:101 0-16\r\na=rtpmap:101 telephone-event/8000\r\na=rtpmap:0 PCMU/8000\r\na=sendrecv\r\n"
			}
			sdp := fmt.Sprintf("v=0\r\no=- 1 %d IN IP4 127.0.0.1\r\ns=%s\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0 101\r\n%s", code, reason[:2], rtp.LocalAddr().(*net.UDPAddr).Port, attributes)
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
			if garbage {
				_, _ = rtp.WriteToUDP([]byte{0x80}, &to) // one byte: no RTP packet
			}
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
		time.Sleep(2500 * time.Millisecond)
		pbx.before.Store(sentBack.Load())
		pbx.loudThen.Store(loud.Load())
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
	return pbx
}

// What a PBX plays before it answers (early media) is heard: the speaker plays it in place of
// the ringback while the call still rings. Nothing of the microphone is sent before the answer:
// only a frame of silence a second, which keeps a path open that would otherwise carry the
// PBX's audio alone (a VPN gateway was seen to close such a path after twelve seconds). From the
// answer on the same audio goes both ways.
func TestAudioBeforeTheAnswerIsPlayedAndOnlySilenceIsSent(t *testing.T) {
	pbx := earlyLab(t, false)
	sentBack, loud, answered := &pbx.sentBack, &pbx.loud, pbx.answered
	o, _ := options(t, pbx.port)
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
	soon(t, "a frame of silence for the PBX while the call rings", func() bool { return sentBack.Load() >= 1 })
	select {
	case <-answered:
		t.Fatal("the PBX answered before the softphone had sent its silence: the test proved nothing")
	default:
	}
	if n, heard := sentBack.Load(), loud.Load(); n > 3 || heard != 0 {
		t.Errorf("before the answer %d packets went to the PBX's audio, %d of them not silence; want one of silence a second", n, heard)
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
	// The whole time before the answer, as the PBX counted it when it answered: two and a half
	// seconds, so one to three frames of silence and nothing else.
	if n, heard := pbx.before.Load(), pbx.loudThen.Load(); n < 1 || n > 3 || heard != 0 {
		t.Errorf("up to its answer the PBX got %d packets, %d of them not silence; want one of silence a second for two and a half seconds", n, heard)
	}
	soon(t, "the microphone's audio at the PBX after the answer", func() bool { return loud.Load() >= 25 })
	if sound.CallStreams() != 1 || call.Closed() {
		t.Errorf("after the answer the call has %d streams, its first one closed: %v", sound.CallStreams(), call.Closed())
	}
}

// A datagram that is not an RTP packet, arriving first on the call's audio port from the PBX's
// own address, is dropped: it does not stop the audio. What the PBX plays is heard before the
// answer and after it, and the microphone reaches the PBX after the answer.
func TestADatagramThatIsNotRTPDoesNotStopTheCallsAudio(t *testing.T) {
	pbx := earlyLab(t, true)
	o, _ := options(t, pbx.port)
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
	call := sound.Last()
	soon(t, "the PBX's audio on the speaker before the answer", func() bool {
		frames, share := call.Heard(1000)
		return frames >= 25 && share[1000] > 0.9
	})
	select {
	case <-pbx.answered:
		t.Fatal("the PBX answered before its early audio was heard: the test proved nothing")
	default:
	}
	until(t, p, "the answer", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.Kind == domain.Established
	})
	call.Reset()
	soon(t, "the PBX's audio on the speaker after the answer", func() bool {
		frames, share := call.Heard(1000)
		return frames >= 25 && share[1000] > 0.9
	})
	soon(t, "the microphone's audio at the PBX after the answer", func() bool { return pbx.loud.Load() >= 25 })
	if sound.CallStreams() != 1 || call.Closed() {
		t.Errorf("the call has %d streams, its first one closed: %v", sound.CallStreams(), call.Closed())
	}
}

// A sound test that is stopped while its devices are still opening never plays, and none starts
// on a phone that was closed.
func TestAnAudioTestStoppedWhileItOpensNeverPlays(t *testing.T) {
	dir := t.TempDir()
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	sound := &memory.Audio{Tone: 1000, OpenDelay: 300 * time.Millisecond}
	p, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan error, 1)
	go func() { started <- p.StartAudioTest() }()
	soon(t, "a test that is starting", func() bool { running, _ := p.AudioTest(); return running })
	p.StopAudioTest() // does not wait for the devices: they are closed when they do open
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if running, _ := p.AudioTest(); running {
		t.Error("the stopped test is running")
	}
	if s := sound.Last(); s == nil || !s.Closed() {
		t.Errorf("the devices that were opening: %v, want them closed", s)
	} else if frames, _ := s.Heard(); frames != 0 {
		t.Errorf("the stopped test played %d frames", frames)
	}
	p.Close()
	if err := p.StartAudioTest(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if running, _ := p.AudioTest(); running || sound.OpenStreams() != 0 {
		t.Errorf("after Close a test is running: %v, with %d streams open", running, sound.OpenStreams())
	}
}

// A ringback whose speaker is still opening when the call ends plays nothing afterwards.
func TestATonesSpeakerThatOpensAfterTheCallEndedPlaysNothing(t *testing.T) {
	o, _ := options(t, ringingRegistrar(t))
	sound := &memory.Audio{Tone: 1000, OpenDelay: 300 * time.Millisecond}
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
	until(t, p, "the call ringing", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.Kind == domain.Ringing
	})
	if err := p.Send(domain.Hangup{Call: 1}); err != nil { // while the speaker is still opening
		t.Fatal(err)
	}
	soon(t, "the speaker that was opening", func() bool { return len(sound.OutputStreams()) == 1 })
	back := sound.OutputStreams()[0]
	soon(t, "the speaker closed again", back.Closed)
	if frames, _ := back.Heard(); frames != 0 {
		t.Errorf("%d frames of the ringback were played after the call ended", frames)
	}
}

// A sound service that does not answer does not hold the softphone: stopping a test whose
// devices are still opening returns at once, and so does closing the phone. The devices are
// closed, unused, when the service finally answers.
func TestAStalledSoundServiceDoesNotHoldTheStopOrTheClose(t *testing.T) {
	dir := t.TempDir()
	o := Options{Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "XDG_CONFIG_HOME": filepath.Join(dir, "config")}}
	sound := &memory.Audio{Tone: 1000, OpenGate: make(chan struct{})}
	p, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	within := func(what string, f func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { defer close(done); f() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s waits for the sound service", what)
		}
	}
	started := make(chan error, 2)
	go func() { started <- p.StartAudioTest() }()
	soon(t, "a test that is starting", func() bool { running, _ := p.AudioTest(); return running })
	within("stopping the test", p.StopAudioTest)
	// A second test, also held by the service, while the phone is closed.
	go func() { started <- p.StartAudioTest() }()
	soon(t, "a second test that is starting", func() bool { running, _ := p.AudioTest(); return running })
	within("closing the phone", p.Close)
	close(sound.OpenGate) // the service answers at last
	for range 2 {
		if err := <-started; err != nil {
			t.Error(err)
		}
	}
	soon(t, "the devices that opened late closed again", func() bool { return sound.OpenStreams() == 0 })
	if running, _ := p.AudioTest(); running {
		t.Error("a test is running after the stop and the close")
	}
}
