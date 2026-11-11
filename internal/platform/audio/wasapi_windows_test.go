package audio

import (
	"os/exec"
	"testing"
	"time"

	"github.com/moutend/go-wca/pkg/wca"
)

func pnp(t *testing.T, verb, endpoint string) {
	t.Helper()
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		verb+"-PnpDevice -InstanceId 'SWD\\MMDEVAPI\\"+endpoint+"' -Confirm:$false").CombinedOutput()
	if err != nil {
		t.Fatalf("%s-PnpDevice: %v: %s", verb, err, out)
	}
}

// The adapter conformance test on Windows: enumerate, open by ID, exchange frames with the devices
// within the frame-loss threshold, hear the played tone on the speaker (loopback), and lose the
// microphone without moving to another one. The microphone of a test VM delivers silence, so
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
	run := func(d time.Duration) (frames, missed, longestMiss int, played []int16) {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		var in, out, heard [FrameSamples]int16
		miss := 0
		for end := time.Now().Add(d); time.Now().Before(end); <-tick.C {
			frames++
			if s.Capture(&in) {
				miss = 0
			} else {
				missed++
				miss++
				longestMiss = max(longestMiss, miss)
			}
			sine(speakerTone, &toneN, out[:])
			s.Play(&out)
			for speaker.Capture(&heard) {
				played = append(played, heard[:]...)
			}
		}
		return
	}
	run(time.Second) // streams start and settle
	before := s.Stats()
	seconds := testSeconds()
	frames, missed, longest, played := run(time.Duration(seconds) * time.Second)
	st := s.Stats()
	silence := st.PlaySilence - before.PlaySilence
	t.Logf("%d s: %d frames, %d capture misses (longest run %d), capture dropped %d, play dropped %d, play silence %d samples, speaker tone in %.3f of blocks",
		seconds, frames, missed, longest, st.CaptureDropped-before.CaptureDropped, st.PlayDropped-before.PlayDropped,
		silence, toneBlocks(played, speakerTone))
	if got := toneBlocks(played, speakerTone); got < 0.98 {
		t.Errorf("the played tone reached the speaker in only %.3f of the blocks", got)
	}
	// Threshold of design D4: at most 0.5 % of frames late or lost, no gap over 60 ms.
	if float64(missed) > 0.005*float64(frames) {
		t.Errorf("%d of %d capture frames missed, over 0.5 %%", missed, frames)
	}
	if longest > 3 {
		t.Errorf("capture gap of %d ms, over 60 ms", longest*20)
	}
	if lost := silence + st.PlayDropped - before.PlayDropped; float64(lost) > 0.005*float64(frames*FrameSamples) {
		t.Errorf("%d playback samples lost or silent, over 0.5 %%", lost)
	}
	if s.CaptureLost() {
		t.Error("the microphone is reported lost while it works")
	}

	// Disable the microphone, as unplugging it does: the stream reports the loss, hands out no
	// audio and keeps playing.
	pnp(t, "Disable", inputs[0].ID)
	t.Cleanup(func() { pnp(t, "Enable", inputs[0].ID) })
	time.Sleep(lostAfter + 500*time.Millisecond)
	if !s.CaptureLost() {
		t.Error("the disabled microphone is not reported lost")
	}
	run(time.Second)
	_, missed, _, played = run(time.Second)
	if missed < 45 {
		t.Errorf("only %d of 50 frames missed after the microphone was disabled (moved to another device?)", missed)
	}
	if got := toneBlocks(played, speakerTone); got < 0.95 {
		t.Errorf("playback did not continue after the microphone was disabled (tone in %.3f of blocks)", got)
	}
	t.Logf("after disabling the microphone: lost=%v, %d of 50 frames missed, speaker tone in %.3f of blocks", s.CaptureLost(), missed, toneBlocks(played, speakerTone))
}
