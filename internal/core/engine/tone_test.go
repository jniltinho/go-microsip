package engine

import (
	"math"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/media"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// The two tones as a speaker gets them: the ring is a second of two notes that alternate, then two
// seconds of silence; the ringback a second of one note, then four of silence; both repeat, stay
// at a quarter of full scale, and start and end each burst from silence.
func TestTheRingAndTheRingbackAreWhatTheyShouldBe(t *testing.T) {
	sound := &memory.Audio{}
	for _, c := range []struct {
		p     pattern
		notes map[float64]int // frames of each note in one period
	}{
		{ring, map[float64]int{RingLowHz: 25, RingHighHz: 25}},
		{ringback, map[float64]int{RingbackHz: 50}},
	} {
		out, err := sound.OpenOutput("")
		if err != nil {
			t.Fatal(err)
		}
		g := generator{p: c.p}
		var frame [media.FrameSamples]int16
		peak, silent := 0.0, 0
		for n := range 2 * c.p.frames {
			g.next(&frame)
			out.Play(&frame)
			loud := 0.0
			for _, v := range frame {
				loud = math.Max(loud, math.Abs(float64(v)))
			}
			peak = math.Max(peak, loud)
			if loud == 0 {
				silent++
			}
			if at := n % c.p.frames; (at == 0 || at == burst-1) && (math.Abs(float64(frame[0])) > 400 && math.Abs(float64(frame[media.FrameSamples-1])) > 400) {
				t.Errorf("%s: frame %d of a burst neither starts nor ends near silence", c.p.name, at)
			}
		}
		streams := sound.OutputStreams()
		var freqs []float64
		for f := range c.notes {
			freqs = append(freqs, f)
		}
		frames, share := streams[len(streams)-1].Heard(freqs...)
		for f, want := range c.notes {
			// The first and the last frame of a burst fade, and may not count as the note.
			if got := int(math.Round(share[f] * float64(frames))); got < 2*want-4 || got > 2*want {
				t.Errorf("%s: %d frames of %v Hz in two periods, want about %d", c.p.name, got, f, 2*want)
			}
		}
		if want := 2 * (c.p.frames - burst); silent != want {
			t.Errorf("%s: %d silent frames in two periods, want %d", c.p.name, silent, want)
		}
		if peak > toneLevel || peak < toneLevel*0.9 {
			t.Errorf("%s: peak %v, want about %v", c.p.name, peak, toneLevel)
		}
		out.Close()
	}
	if open := sound.OpenStreams(); open != 0 {
		t.Errorf("%d streams left open", open)
	}
}
