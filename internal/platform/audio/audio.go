// Package audio is the sound-device adapter: capture and playback of 20 ms frames of 8 kHz mono
// audio on the devices the user chose. One backend per operating system implements it; this file
// is the portable part.
//
// The device side (the backend's threads) and the engine side (the 20 ms media tick) never share a
// lock: samples cross through preallocated single-producer single-consumer rings, so neither side
// blocks or allocates.
package audio

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// SampleRate is the rate of every frame; the audio service converts to the device's own.
	SampleRate = 8000
	// FrameSamples is one 20 ms frame.
	FrameSamples = 160
)

const (
	// ringSamples holds half a second: far more than the engine and the device ever drift apart.
	ringSamples = 4096
	// primeSamples is the backlog capture builds before it hands out frames, so a fragment that
	// arrives just after a tick does not become a missed frame. It costs 20 ms of delay.
	primeSamples = 2 * FrameSamples
	// discardDepth is the least dropped from what the device delivers after it confirmed a
	// discard: what it had recorded and not handed over yet. A backend adds what its device
	// reports on top.
	discardDepth = SampleRate / 10
	// playBacklog is the most the engine may be ahead of the device; beyond it new audio is dropped
	// so delay cannot build up.
	playBacklog = 4 * FrameSamples
	// captureBacklog is the most captured audio kept waiting for the engine; beyond it the oldest is
	// dropped, so a stalled engine does not turn into permanent microphone delay.
	captureBacklog = 6 * FrameSamples
	// lostAfter is how long capture may deliver nothing before the microphone counts as lost.
	lostAfter = 300 * time.Millisecond
)

// ErrNoService means the session offers no audio service this backend can reach. It is reported to
// the user as "audio unavailable", never worked around.
var ErrNoService = errors.New("no audio service is reachable")

// Device is one input or output.
type Device struct {
	// ID is stable for the device and is what settings store.
	ID string
	// Name is what the user sees.
	Name string
	// Default marks the device the audio service uses when none is chosen.
	Default bool
}

// Stats counts what the stream had to drop or invent since it was opened.
type Stats struct {
	// CaptureDropped is the number of captured samples discarded because the engine was not reading.
	CaptureDropped uint64
	// PlayDropped is the number of samples discarded because the engine wrote faster than the
	// device played.
	PlayDropped uint64
	// PlaySilence is the number of silent samples played because the engine had written nothing.
	PlaySilence uint64
}

// ring is a single-producer single-consumer queue of samples. Its capacity is a power of two.
type ring struct {
	buf        []int16
	head, tail atomic.Uint64 // head: next write, tail: next read
}

func newRing(capacity int) *ring { return &ring{buf: make([]int16, capacity)} }

// len is the number of samples waiting.
func (r *ring) len() int { return int(r.head.Load() - r.tail.Load()) }

// write appends as many samples as fit and returns how many it took.
func (r *ring) write(p []int16) int {
	head := r.head.Load()
	n := min(len(p), len(r.buf)-int(head-r.tail.Load()))
	mask := uint64(len(r.buf) - 1)
	for i := range n {
		r.buf[(head+uint64(i))&mask] = p[i]
	}
	r.head.Store(head + uint64(n))
	return n
}

// read removes up to len(p) samples and returns how many it gave.
func (r *ring) read(p []int16) int {
	tail := r.tail.Load()
	n := min(len(p), int(r.head.Load()-tail))
	mask := uint64(len(r.buf) - 1)
	for i := range n {
		p[i] = r.buf[(tail+uint64(i))&mask]
	}
	r.tail.Store(tail + uint64(n))
	return n
}

// buffers is what every backend's stream shares: the two rings, the counters, and the engine side
// of the stream. The backend adds the device side.
type buffers struct {
	in, out *ring
	primed  bool // engine side only

	// discard orders a discard against the device's deliveries. While discarding, everything the
	// device delivers is dropped, whenever it comes; after the backend confirmed that the device
	// is past the discard, skip more samples are.
	discard    sync.Mutex
	discarding bool
	skip       int

	lastCapture                          atomic.Int64 // UnixNano of the last captured fragment
	captureDropped, playDropped, silence atomic.Uint64
}

func newBuffers() *buffers {
	b := &buffers{in: newRing(ringSamples), out: newRing(ringSamples)}
	b.lastCapture.Store(time.Now().UnixNano())
	return b
}

// captured is the device side of capture: it only copies into the ring.
func (b *buffers) captured(p []int16) {
	b.discard.Lock()
	defer b.discard.Unlock()
	if b.discarding {
		return // recorded before the discard, whenever it is delivered; and no sign of life
	}
	b.lastCapture.Store(time.Now().UnixNano())
	if b.skip > 0 {
		n := min(b.skip, len(p))
		b.skip, p = b.skip-n, p[n:]
	}
	if n := b.in.write(p); n < len(p) {
		b.captureDropped.Add(uint64(len(p) - n))
	}
}

// stale reports that capture has delivered nothing for lostAfter.
func (b *buffers) stale() bool {
	return time.Since(time.Unix(0, b.lastCapture.Load())) > lostAfter
}

// Capture fills frame with the next 20 ms from the microphone and reports whether it had one. The
// engine calls it once per media tick; on false it sends silence or nothing.
func (b *buffers) Capture(frame *[FrameSamples]int16) bool {
	have := b.in.len()
	if have > captureBacklog {
		// The engine fell behind (it is this ring's only reader, so it may skip).
		skip := have - primeSamples
		b.in.tail.Add(uint64(skip))
		b.captureDropped.Add(uint64(skip))
		have = primeSamples
	}
	if !b.primed {
		if have < primeSamples {
			return false
		}
		b.primed = true
	}
	if have < FrameSamples {
		b.primed = false
		return false
	}
	b.in.read(frame[:])
	return true
}

// discardBegin is the engine side of a discard: it drops what waits in the ring and, from now on,
// everything the device delivers, however late, until the backend calls discardEnd. The engine
// side is this ring's only reader, so it may skip. A discard that is never confirmed leaves the
// microphone silent, and stale tells so.
func (b *buffers) discardBegin() {
	b.discard.Lock()
	defer b.discard.Unlock()
	b.discarding = true
	b.in.tail.Store(b.in.head.Load())
	b.primed = false
}

// discardEnd is the backend's confirmation: whatever the device handed over before the discard
// has been through captured. Of what it delivers from here, the first skip samples (at least
// discardDepth) are dropped too: the device recorded them before it handed anything over. It does
// nothing when no discard is waiting.
func (b *buffers) discardEnd(skip int) {
	b.discard.Lock()
	defer b.discard.Unlock()
	if b.discarding {
		b.discarding, b.skip = false, max(skip, discardDepth)
	}
}

// Play queues 20 ms for the speaker. It never blocks: when the device is behind by more than
// playBacklog, the frame is dropped so delay cannot build up.
func (b *buffers) Play(frame *[FrameSamples]int16) {
	if b.out.len() > playBacklog-FrameSamples {
		b.playDropped.Add(FrameSamples)
		return
	}
	b.out.write(frame[:])
}

// Stats returns the counters.
func (b *buffers) Stats() Stats {
	return Stats{CaptureDropped: b.captureDropped.Load(), PlayDropped: b.playDropped.Load(), PlaySilence: b.silence.Load()}
}
