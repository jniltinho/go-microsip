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
	"sync/atomic"
)

const (
	// SampleRate is the rate of every frame; the audio service converts to the device's own.
	SampleRate = 8000
	// FrameSamples is one 20 ms frame.
	FrameSamples = 160
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
