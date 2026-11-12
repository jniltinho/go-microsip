package audio

import (
	"fmt"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// The devices are listed from a thread that already has COM in the other threading mode, as the
// thread of the command line and of the window does: `doctor` said "no audio service" there.
func TestWASAPIFromAThreadWithCOMInAnotherMode(t *testing.T) {
	needServer(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		t.Fatalf("this thread could not get COM in apartment mode: %v", err)
	}
	defer ole.CoUninitialize()
	c, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	inputs, err := c.Inputs()
	if err != nil || len(inputs) == 0 {
		t.Fatalf("inputs: %v %v", inputs, err)
	}
	outputs, err := c.Outputs()
	if err != nil || len(outputs) == 0 {
		t.Fatalf("outputs: %v %v", outputs, err)
	}
}

// card disables or enables every sound card of the machine and reports whether Windows did it
// (it needs an elevated session, and some virtual sound cards refuse).
func card(verb string) error {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-PnpDevice -Class MEDIA -PresentOnly | "+verb+"-PnpDevice -Confirm:$false").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s-PnpDevice: %v: %.200s", verb, err, out)
	}
	return nil
}

// The speaker alone, as a ring uses it: playback starts on the default device with no microphone
// opened, takes a second and a half of a tone at the media tick without dropping it, and stops.
func TestWASAPIOutputAlone(t *testing.T) {
	needServer(t)
	c, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	out, err := c.OpenOutput("")
	if err != nil {
		t.Fatalf("the speaker alone did not open: %v", err)
	}
	var frame [FrameSamples]int16
	n := 0
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for range 75 {
		<-tick.C
		sine(550, &n, frame[:])
		out.Play(&frame)
	}
	st := out.Stats()
	if out.PlaybackLost() || st.PlayDropped != 0 {
		t.Errorf("the speaker alone: lost %v, %d samples dropped", out.PlaybackLost(), st.PlayDropped)
	}
	out.Close()
	t.Logf("the speaker alone played 1.5 s (%d samples of silence filled in by the device thread)", st.PlaySilence)
	if _, err := c.OpenOutput("no-such-device"); err == nil {
		t.Error("an unknown output device was opened")
	}
}

// The adapter conformance test on Windows: enumerate, open by ID, exchange frames with the devices
// within the frame-loss threshold, hear the played tone on the speaker (loopback), and report the loss
// of the sound card. The microphone of a test VM delivers silence, so
// capture is checked for timing, not content.
func TestWASAPI(t *testing.T) {
	needServer(t)
	c, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	inputs, err := c.Inputs()
	if err != nil || len(inputs) == 0 {
		t.Fatalf("inputs: %v %v", inputs, err)
	}
	outputs, err := c.Outputs()
	if err != nil || len(outputs) == 0 {
		t.Fatalf("outputs: %v %v", outputs, err)
	}
	for _, d := range append(append([]Device{}, inputs...), outputs...) {
		if d.ID == "" || d.Name == "" {
			t.Errorf("device without an ID or a name: %+v", d)
		}
		t.Logf("device %q default=%v", d.Name, d.Default)
	}
	if _, err := c.Open("no-such-device", ""); err == nil {
		t.Fatal("opening an unknown input succeeded")
	}

	// What the speaker plays, captured back from the render endpoint.
	speaker := &Stream{buffers: newBuffers()}
	ready := make(chan error, 1)
	speaker.wg.Add(1)
	go speaker.capture(wca.ERender, outputs[0].ID, wca.AUDCLNT_STREAMFLAGS_LOOPBACK, ready)
	if err := <-ready; err != nil {
		t.Fatalf("loopback: %v", err)
	}
	defer speaker.Close()

	s, err := c.Open(inputs[0].ID, outputs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	toneN := 0
	// The engine's media tick: every 20 ms take one captured frame and play one frame. stall
	// freezes the loop once, as a blocked engine would.
	run := func(d, stall time.Duration) (*meter, Stats) {
		var heard [FrameSamples]int16
		for speaker.Capture(&heard) {
		}
		before := s.Stats()
		m := newMeter(time.Now())
		var got []int16
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		var in, out [FrameSamples]int16
		for end := time.Now().Add(d); time.Now().Before(end); <-tick.C {
			if stall > 0 && time.Since(m.start) > d/2 {
				time.Sleep(stall)
				stall = 0
			}
			now := time.Now()
			m.captureFrame(s.Capture(&in), in[:], now)
			sine(speakerTone, &toneN, out[:])
			s.Play(&out)
			got = got[:0]
			for speaker.Capture(&heard) {
				got = append(got, heard[:]...)
			}
			m.heard(got, now)
		}
		return m, before
	}
	run(time.Second, 0) // streams start and settle
	seconds := testSeconds()
	m, before := run(time.Duration(seconds)*time.Second, 0)
	summary, bad := m.verdict(time.Now(), s.Stats(), before, false) // a VM's microphone is silent
	t.Logf("%d s: %s", seconds, summary)
	for _, b := range bad {
		t.Error(b)
	}
	if s.CaptureLost() || s.PlaybackLost() {
		t.Error("a device is reported lost while it works")
	}
	// Control: the same checks must fail when the engine stalls.
	m, before = run(2*time.Second, 300*time.Millisecond)
	_, bad = m.verdict(time.Now(), s.Stats(), before, false)
	mustCatch(t, bad)
	run(time.Second, 0) // recover

	// Disable the sound card, as unplugging it does: both streams report the loss, capture hands
	// out no audio, and nothing crashes. (A VM has one sound card, so the speaker goes with it.)
	// Enabled again whatever comes of it: Windows can refuse to disable the card now and still
	// leave it disabled at the next start, which left a VM without sound.
	t.Cleanup(func() { _ = card("Enable") })
	if err := card("Disable"); err != nil {
		t.Logf("NOT VERIFIED: the loss of the sound card could not be provoked on this machine: %v", err)
		return
	}
	time.Sleep(lostAfter + time.Second)
	m, _ = run(time.Second, 0)
	if !s.CaptureLost() || m.captured > 5 {
		t.Errorf("the disabled microphone: lost=%v, %d frames captured in 1 s", s.CaptureLost(), m.captured)
	}
	if !s.PlaybackLost() {
		t.Error("the disabled speaker is not reported lost")
	}
	t.Logf("after disabling the sound card: capture lost=%v, playback lost=%v, %d frames captured in 1 s", s.CaptureLost(), s.PlaybackLost(), m.captured)
}
