//go:build darwin && coreaudio

package audio

import (
	"testing"
	"time"
)

// Tests of the prototype on a Mac's real devices, from the cross-built test binary. The call
// test needs the microphone, which macOS grants to the program that started the test: run it
// from an application that was allowed, with GO_MICROSIP_AUDIO_TEST=1.

func TestCoreAudioDevices(t *testing.T) {
	var d Devices
	inputs, err := d.Inputs()
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := d.Outputs()
	if err != nil {
		t.Fatal(err)
	}
	for kind, list := range map[string]int{"inputs": len(inputs), "outputs": len(outputs)} {
		t.Logf("%s: %d", kind, list)
	}
	defaults := 0
	for _, device := range append(inputs, outputs...) {
		if device.ID == "" || device.Name == "" {
			t.Errorf("a device without an ID or a name: %+v", device)
		}
		if device.Default {
			defaults++
		}
	}
	t.Logf("default devices among them: %d", defaults)
	if _, err := d.Open("no-such-device", ""); err == nil {
		t.Error("an input that does not exist was opened")
	}
	if _, err := d.OpenOutput("no-such-device"); err == nil {
		t.Error("an output that does not exist was opened")
	}
}

func TestCoreAudioOutputAloneOpensNoMicrophone(t *testing.T) {
	needServer(t)
	var d Devices
	player, err := d.OpenOutput("")
	if err != nil {
		t.Fatal(err)
	}
	var (
		frame [FrameSamples]int16
		n     int
	)
	start := time.Now()
	for i := range 50 { // one second of a tone
		sine(speakerTone, &n, frame[:])
		for j := range frame {
			frame[j] /= 8
		}
		player.Play(&frame)
		time.Sleep(time.Until(start.Add(time.Duration(i+1) * 20 * time.Millisecond)))
	}
	live.RLock()
	capturing := len(live.capture)
	live.RUnlock()
	stats := player.(*Output).Stats()
	player.Close()
	if capturing != 0 {
		t.Errorf("%d capture queues exist while only an output was opened", capturing)
	}
	t.Logf("one second played: %d samples dropped, %d of silence", stats.PlayDropped, stats.PlaySilence)
	if stats.PlayDropped > 2*FrameSamples || stats.PlaySilence > SampleRate/4 {
		t.Errorf("playback did not keep up: %+v", stats)
	}
}

// TestCoreAudioCall is a call's sound without a call: every 20 ms the engine's side takes a
// frame from the microphone and plays it, so a person hears their own voice. Halfway it
// discards, as the end of a mute does.
func TestCoreAudioCall(t *testing.T) {
	needServer(t)
	var d Devices
	opening := time.Now()
	io, err := d.Open("", "")
	if err != nil {
		t.Fatal(err)
	}
	s := io.(*Stream)
	defer s.Close()
	t.Logf("Open returned after %v", time.Since(opening).Round(time.Millisecond))

	seconds := testSeconds()
	ticks := seconds * 50
	var (
		frame, silent                [FrameSamples]int16
		got, missed, peak            int
		first, afterDiscard, lastGot time.Duration
		longest                      time.Duration
		discardedAt, discardReturned time.Duration
		discarded, heardAfterDiscard bool
	)
	start := time.Now()
	for i := range ticks {
		now := time.Since(start)
		if !discarded && i == ticks/2 {
			s.Discard()
			discarded, discardedAt = true, now
			discardReturned = time.Since(start) - now
		}
		if s.Capture(&frame) {
			if got == 0 {
				first = now
			} else if gap := now - lastGot; gap > longest && !(discarded && !heardAfterDiscard) {
				longest = gap
			}
			if discarded && !heardAfterDiscard {
				heardAfterDiscard, afterDiscard = true, now-discardedAt
			}
			got, lastGot = got+1, now
			for _, v := range frame {
				if v > int16(peak) {
					peak = int(v)
				} else if -v > int16(peak) {
					peak = int(-v)
				}
			}
			s.Play(&frame)
		} else {
			missed++
			s.Play(&silent)
		}
		time.Sleep(time.Until(start.Add(time.Duration(i+1) * 20 * time.Millisecond)))
	}
	stats := s.Stats()
	t.Logf("%d s: %d frames captured, %d ticks without one, first after %v, longest gap between two %v, peak %d",
		seconds, got, missed, first.Round(time.Millisecond), longest.Round(time.Millisecond), peak)
	t.Logf("discard: returned after %v, first frame %v later", discardReturned.Round(time.Microsecond), afterDiscard.Round(time.Millisecond))
	t.Logf("dropped: %d captured samples, %d played; %d samples of silence played", stats.CaptureDropped, stats.PlayDropped, stats.PlaySilence)
	if s.CaptureLost() {
		t.Error("the microphone counts as lost at the end")
	}
	if got < ticks*8/10 {
		t.Errorf("%d of %d frames captured", got, ticks)
	}
	if !heardAfterDiscard {
		t.Error("no frame came after the discard")
	}
	if peak == 0 {
		t.Error("only zeros: macOS hands silence to a program that was not given the microphone")
	}
}
