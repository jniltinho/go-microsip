// Package memory implements the core's ports without an operating system: a tone generator for a
// microphone and a tone meter for a speaker. The core's tests and the lab runner use it, so they
// need no sound device, display or session bus.
package memory

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

const sampleRate = 8000

// Audio is a fake sound system. Every stream it opens captures a sine of Tone Hz and measures what
// is played to it.
type Audio struct {
	// Tone is the frequency the microphone "hears".
	Tone float64
	// Unavailable makes Open fail with ports.ErrNoAudioService.
	Unavailable bool
	// Denied makes Open fail with ports.ErrMicrophoneDenied.
	Denied bool
	// OpenDelay is how long Open takes, as a sound service that is slow to answer.
	OpenDelay time.Duration
	// OpenGate, when set, holds Open until it is closed: a sound service that does not answer.
	OpenGate chan struct{}

	mu     sync.Mutex
	opened []*Stream
}

// Inputs lists one fake microphone.
func (a *Audio) Inputs() ([]ports.Device, error) {
	return []ports.Device{{ID: "memory-mic", Name: "Memory microphone", Default: true}}, nil
}

// Outputs lists one fake speaker.
func (a *Audio) Outputs() ([]ports.Device, error) {
	return []ports.Device{{ID: "memory-speaker", Name: "Memory speaker", Default: true}}, nil
}

// Open returns a new stream, or ports.ErrNoAudioService when Unavailable is set.
func (a *Audio) Open(input, output string) (ports.MediaIO, error) {
	time.Sleep(a.OpenDelay)
	if a.OpenGate != nil {
		<-a.OpenGate
	}
	if a.Unavailable {
		return nil, ports.ErrNoAudioService
	}
	if a.Denied {
		return nil, ports.ErrMicrophoneDenied
	}
	s := &Stream{tone: a.Tone, Input: input, Output: output}
	a.mu.Lock()
	a.opened = append(a.opened, s)
	a.mu.Unlock()
	return s, nil
}

// OpenOutput returns a new stream that is only played to, or ports.ErrNoAudioService when
// Unavailable is set.
func (a *Audio) OpenOutput(output string) (ports.Player, error) {
	time.Sleep(a.OpenDelay)
	if a.Unavailable {
		return nil, ports.ErrNoAudioService
	}
	s := &Stream{output: true, Output: output}
	a.mu.Lock()
	a.opened = append(a.opened, s)
	a.mu.Unlock()
	return s, nil
}

// Outputs returns the streams that were opened for playback alone, in order: the rings.
func (a *Audio) OutputStreams() []*Stream {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*Stream
	for _, s := range a.opened {
		if s.output {
			out = append(out, s)
		}
	}
	return out
}

// CallStreams reports how many streams were opened with a microphone: a call's audio.
func (a *Audio) CallStreams() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, s := range a.opened {
		if !s.output {
			n++
		}
	}
	return n
}

// Last is the stream opened most recently, or nil.
func (a *Audio) Last() *Stream {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.opened) == 0 {
		return nil
	}
	return a.opened[len(a.opened)-1]
}

// Open streams reports how many streams are open now: none must outlive its call.
func (a *Audio) OpenStreams() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, s := range a.opened {
		if !s.closed.Load() {
			n++
		}
	}
	return n
}

// Stream is one call's fake sound: the engine side is ports.MediaIO, the test side is Heard.
type Stream struct {
	// Input and Output are the device IDs the stream was opened on.
	Input, Output string

	tone      float64
	output    bool // opened for playback alone
	n         int  // engine side only
	closed    atomic.Bool
	unplugged atomic.Bool

	mu     sync.Mutex
	played []int16
}

// Unplug makes the microphone stop delivering, as a device that was removed does.
func (s *Stream) Unplug() { s.unplugged.Store(true) }

// Capture produces the next 20 ms of the tone, or nothing once the microphone is unplugged.
func (s *Stream) Capture(frame *[ports.FrameSamples]int16) bool {
	if s.unplugged.Load() {
		return false
	}
	for i := range frame {
		frame[i] = int16(12000 * math.Sin(2*math.Pi*s.tone*float64(s.n)/sampleRate))
		s.n++
	}
	return true
}

// Play records the frame for Heard. It keeps the last few seconds only.
func (s *Stream) Play(frame *[ports.FrameSamples]int16) {
	s.mu.Lock()
	if len(s.played) > 10*sampleRate {
		s.played = s.played[len(s.played)-5*sampleRate:]
	}
	s.played = append(s.played, frame[:]...)
	s.mu.Unlock()
}

// Discard has nothing to drop: this microphone records only when it is asked.
func (s *Stream) Discard() {}

// CaptureLost reports whether the microphone was unplugged.
func (s *Stream) CaptureLost() bool { return s.unplugged.Load() }

// Close marks the stream closed.
func (s *Stream) Close() { s.closed.Store(true) }

// Closed reports whether the engine released the stream.
func (s *Stream) Closed() bool { return s.closed.Load() }

// Reset forgets what was played so far.
func (s *Stream) Reset() {
	s.mu.Lock()
	s.played = s.played[:0]
	s.mu.Unlock()
}

// Heard returns how many frames were played since the last Reset and, for each frequency, the
// share of 20 ms frames that carried it (0 to 1).
func (s *Stream) Heard(freqs ...float64) (frames int, share map[float64]float64) {
	s.mu.Lock()
	played := append([]int16(nil), s.played...)
	s.mu.Unlock()
	share = map[float64]float64{}
	for ; len(played) >= ports.FrameSamples; played = played[ports.FrameSamples:] {
		frames++
		for _, f := range freqs {
			if tone(played[:ports.FrameSamples], f) > 0.5 {
				share[f]++
			}
		}
	}
	for f := range share {
		share[f] /= float64(frames)
	}
	return frames, share
}

// tone is the part of the frame's energy at freq (Goertzel); 1 is a pure tone.
func tone(samples []int16, freq float64) float64 {
	coeff := 2 * math.Cos(2*math.Pi*freq/sampleRate)
	var s1, s2, total float64
	for _, v := range samples {
		x := float64(v)
		total += x * x
		s1, s2 = x+coeff*s1-s2, s1
	}
	if total == 0 {
		return 0
	}
	return (s1*s1 + s2*s2 - coeff*s1*s2) / (float64(len(samples)) / 2 * total)
}
