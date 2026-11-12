package engine

import (
	"math"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/media"
)

// The tones the softphone plays by itself, on the output device, while a call has no audio of
// its own (design D15): a ring for a call that comes in, and a ringback for a call that rings at
// the other end. They are generated: no sound file is shipped. No microphone is opened for them.

// pattern is a tone that repeats: for each 20 ms frame of its period, the frequency that sounds,
// or 0 for silence.
type pattern struct {
	name   string
	frames int               // the period, in 20 ms frames
	freq   func(int) float64 // the frame's frequency
}

const (
	// RingbackHz is the tone of a call that rings at the other end: one second in five.
	RingbackHz = 425
	// RingLowHz and RingHighHz alternate ten times a second in the ring of a call that comes
	// in: one second in three. Both make whole cycles in a 20 ms frame, so a frame of the ring
	// is told from any other sound exactly.
	RingLowHz  = 550
	RingHighHz = 850
	// toneLevel is the tones' peak, a quarter of full scale.
	toneLevel = math.MaxInt16 / 4
	// burst is how long a tone sounds in its period: one second.
	burst = 50
	// toneRate is the samples per second of the output device's frames, and toneFrame how long
	// one of them lasts.
	toneRate  = 8000
	toneFrame = 20 * time.Millisecond
	// toneAhead is how many frames are handed to the device before the first tick.
	toneAhead = 3
)

var (
	ringback = pattern{name: "ringback", frames: 250, freq: func(n int) float64 {
		if n < burst {
			return RingbackHz
		}
		return 0
	}}
	ring = pattern{name: "ring", frames: 150, freq: func(n int) float64 {
		switch {
		case n >= burst:
			return 0
		case n/5%2 == 0:
			return RingLowHz
		}
		return RingHighHz
	}}
)

// generator writes a pattern frame by frame. The phase runs on from one frame to the next, so a
// change of frequency does not click, and a burst fades in and out over its first and last frame.
type generator struct {
	p     pattern
	n     int
	phase float64
}

func (g *generator) next(frame *[media.FrameSamples]int16) {
	at := g.n % g.p.frames
	freq := g.p.freq(at)
	g.n++
	if freq == 0 {
		clear(frame[:])
		g.phase = 0
		return
	}
	first := at == 0 || g.p.freq(at-1) == 0
	last := g.p.freq((at+1)%g.p.frames) == 0
	step := 2 * math.Pi * freq / toneRate
	for i := range frame {
		level := float64(toneLevel)
		switch {
		case first:
			level *= float64(i) / media.FrameSamples
		case last:
			level *= float64(media.FrameSamples-i) / media.FrameSamples
		}
		frame[i] = int16(level * math.Sin(g.phase))
		g.phase += step
	}
	g.phase = math.Mod(g.phase, 2*math.Pi)
}

// tone is a pattern that is playing for a call.
type tone struct {
	name string
	stop chan struct{}
}

// sound plays a pattern for the call until quiet is called. The output device is opened beside
// the actor, as a call's devices are: a sound service that does not answer must not keep the
// engine from taking commands. Without a sound service the call goes on without the tone.
func (a *actor) sound(c *call, p pattern) {
	if c.tone != nil {
		if c.tone.name == p.name {
			return
		}
		c.quiet()
	}
	t := &tone{name: p.name, stop: make(chan struct{})}
	c.tone = t
	opts := a.e.opts
	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		select {
		case <-t.stop:
			return // over before the device was asked for
		default:
		}
		out, err := opts.Audio.OpenOutput(opts.Output)
		if err != nil {
			opts.Log.Debug("no "+p.name+": the output device did not open", "error", err)
			return
		}
		defer out.Close()
		tick := time.NewTicker(toneFrame)
		defer tick.Stop()
		g := generator{p: p}
		var frame [media.FrameSamples]int16
		// A little ahead of the device from the start: it takes sound at its own moments, and
		// must not find the queue empty between two ticks.
		for range toneAhead {
			g.next(&frame)
			out.Play(&frame)
		}
		for {
			select {
			case <-t.stop:
				return
			case <-tick.C:
				g.next(&frame)
				out.Play(&frame)
			}
		}
	}()
}

// quiet stops the call's tone, if one plays.
func (c *call) quiet() {
	if c.tone != nil {
		close(c.tone.stop)
		c.tone = nil
	}
}

// progress is what an outgoing call does with a provisional answer: 180 is a phone ringing at the
// other end, which the softphone makes audible; with 183 the other side sends audio of its own
// and the ringback stops.
func (a *actor) progress(c *call, status int) {
	a.move(c, domain.CallPhase{Kind: domain.Ringing})
	if c.snap.Phase.Kind != domain.Ringing {
		return // hung up meanwhile, or answered
	}
	switch status {
	case 180:
		a.sound(c, ringback)
	case 183:
		c.quiet()
	}
}
