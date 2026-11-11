package audio

import (
	"strings"
	"testing"
	"time"
)

// The ring keeps order, refuses what does not fit, and its operations do not allocate: they run on
// the device side.
func TestRing(t *testing.T) {
	r := newRing(8)
	if n := r.write([]int16{1, 2, 3, 4, 5, 6}); n != 6 {
		t.Fatalf("wrote %d, want 6", n)
	}
	if n := r.write([]int16{7, 8, 9}); n != 2 {
		t.Fatalf("a full ring took %d, want 2", n)
	}
	got := make([]int16, 5)
	if n := r.read(got); n != 5 || got[0] != 1 || got[4] != 5 {
		t.Fatalf("read %d %v", n, got)
	}
	r.write([]int16{9, 10, 11}) // wraps
	got = make([]int16, 8)
	n := r.read(got)
	if want := []int16{6, 7, 8, 9, 10, 11}; n != len(want) || got[0] != 6 || got[5] != 11 {
		t.Fatalf("after wrapping read %d %v, want %v", n, got[:n], want)
	}
	if r.len() != 0 {
		t.Fatalf("len = %d, want 0", r.len())
	}
	frame := make([]int16, FrameSamples)
	big := newRing(4096)
	if a := testing.AllocsPerRun(100, func() { big.write(frame); big.read(frame) }); a != 0 {
		t.Fatalf("ring operations allocate %v times", a)
	}
}

// The meter passes what the threshold allows and fails what it forbids, in each direction, however
// the samples are batched: the proof that a device test's verdict means something.
func TestMeter(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	// run feeds the meter 5 s of perfect ticks, except what the two hooks change: capture decides
	// per tick whether a frame came, speaker returns the samples played in that tick.
	run := func(capture func(tick int) bool, speaker func(tick int, tone []int16) []int16, batch int) []string {
		m := newMeter(t0)
		toneN := 0
		var frame, held []int16
		frame = make([]int16, FrameSamples)
		var st Stats
		for tick := 0; tick < 250; tick++ {
			now := t0.Add(time.Duration(tick+1) * 20 * time.Millisecond)
			sine(micTone, new(int), frame)
			m.captureFrame(capture == nil || capture(tick), frame, now)
			tone := make([]int16, FrameSamples)
			sine(speakerTone, &toneN, tone)
			if speaker != nil {
				tone = speaker(tick, tone)
			}
			held = append(held, tone...)
			if (tick+1)%batch == 0 { // delivered every batch ticks, all at once
				m.heard(held, now)
				held = held[:0]
			}
		}
		_, bad := m.verdict(t0.Add(250*20*time.Millisecond), st, st, true)
		return bad
	}
	silence := func(from, ticks int) func(int, []int16) []int16 {
		return func(tick int, tone []int16) []int16 {
			if tick >= from && tick < from+ticks {
				clear(tone)
			}
			return tone
		}
	}
	missing := func(from, ticks int) func(int, []int16) []int16 { // nothing delivered at all
		return func(tick int, tone []int16) []int16 {
			if tick >= from && tick < from+ticks {
				return nil
			}
			return tone
		}
	}
	missed := func(from, ticks int) func(int) bool {
		return func(tick int) bool { return tick < from || tick >= from+ticks }
	}
	for name, c := range map[string]struct {
		capture func(int) bool
		speaker func(int, []int16) []int16
		batch   int
		want    string // "" passes; otherwise a problem that must be named
	}{
		"perfect":                         {nil, nil, 1, ""},
		"perfect, batched":                {nil, nil, 4, ""},
		"capture: one tick missed":        {missed(100, 1), nil, 1, ""},
		"capture: 60 ms missed":           {missed(100, 3), nil, 1, "capture: 3 of"}, // 3 of 250 is over 0.5 %, the gap itself is allowed
		"capture: 80 ms missed":           {missed(100, 4), nil, 1, "capture: gap over 60 ms"},
		"speaker: 60 ms of silence":       {nil, silence(100, 3), 1, "playback: 6 of"}, // over 0.5 % of 5 s, but no gap problem
		"speaker: 80 ms of silence":       {nil, silence(100, 4), 1, "playback: gap over 60 ms"},
		"speaker: 80 ms silence, batched": {nil, silence(100, 4), 10, "playback: gap over 60 ms"},
		"speaker: 80 ms never delivered":  {nil, missing(100, 4), 1, "playback: gap over 60 ms"},
		"speaker: nothing":                {nil, missing(0, 250), 1, "playback: nothing was heard"},
	} {
		bad := strings.Join(run(c.capture, c.speaker, c.batch), "; ")
		switch {
		case c.want == "" && bad != "":
			t.Errorf("%s: failed with %q, want a pass", name, bad)
		case c.want != "" && !strings.Contains(bad, c.want):
			t.Errorf("%s: got %q, want it to name %q", name, bad, c.want)
		}
		if strings.Contains(name, "60 ms") && strings.Contains(bad, "gap over 60 ms") {
			t.Errorf("%s: a 60 ms gap was reported as over 60 ms: %q", name, bad)
		}
	}
}

// Discard drops what was captured and not handed out, to the last sample: the next frame is made
// only of what the device delivers afterwards.
func TestDiscardDropsEverythingCaptured(t *testing.T) {
	b := newBuffers()
	fill := func(value int16, samples int) {
		p := make([]int16, samples)
		for i := range p {
			p[i] = value
		}
		b.captured(p)
	}
	var frame [FrameSamples]int16
	fill(7, 3*FrameSamples+FrameSamples/2) // three and a half frames of what must not be sent
	if !b.Capture(&frame) || frame[0] != 7 {
		t.Fatal("the fixture did not capture")
	}
	b.Discard()
	if b.Capture(&frame) {
		t.Fatal("a frame came out right after the discard")
	}
	fill(1, 4*FrameSamples)
	for n := 0; b.Capture(&frame); n++ {
		for i, v := range frame {
			if v != 1 {
				t.Fatalf("frame %d, sample %d is %d: it was captured before the discard", n, i, v)
			}
		}
	}
	if st := b.Stats(); st.CaptureDropped != 0 {
		t.Errorf("%d samples counted as dropped: a discard is not a loss", st.CaptureDropped)
	}
}
