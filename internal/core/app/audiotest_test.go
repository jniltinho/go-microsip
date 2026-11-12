package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
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
