package audio

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// The Linux backend speaks the PulseAudio native protocol, which PipeWire's pipewire-pulse serves
// too. It needs no C library: the client is Go.

const (
	// ringSamples holds half a second: far more than the engine and the device ever drift apart.
	ringSamples = 4096
	// primeSamples is the backlog capture builds before it hands out frames, so a fragment that
	// arrives just after a tick does not become a missed frame. It costs 20 ms of delay.
	primeSamples = 2 * FrameSamples
	// playBacklog is the most the engine may be ahead of the device; beyond it new audio is dropped
	// so delay cannot build up.
	playBacklog = 4 * FrameSamples
	// lostAfter is how long capture may deliver nothing before the microphone counts as lost.
	lostAfter = 300 * time.Millisecond
)

// Client is a connection to the session's audio service.
type Client struct{ c *pulse.Client }

// Connect reaches the audio service of this session, or fails with ErrNoService.
func Connect() (*Client, error) {
	c, err := pulse.NewClient(pulse.ClientApplicationName("go-microsip"))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoService, err)
	}
	return &Client{c}, nil
}

// Close ends the connection and every stream opened on it.
func (c *Client) Close() { c.c.Close() }

// Inputs lists the capture devices.
func (c *Client) Inputs() ([]Device, error) {
	list, err := c.c.ListSources()
	if err != nil {
		return nil, err
	}
	def, _ := c.c.DefaultSource()
	out := make([]Device, len(list))
	for i, s := range list {
		out[i] = Device{ID: s.ID(), Name: s.Name(), Default: def != nil && def.ID() == s.ID()}
	}
	return out, nil
}

// Outputs lists the playback devices.
func (c *Client) Outputs() ([]Device, error) {
	list, err := c.c.ListSinks()
	if err != nil {
		return nil, err
	}
	def, _ := c.c.DefaultSink()
	out := make([]Device, len(list))
	for i, s := range list {
		out[i] = Device{ID: s.ID(), Name: s.Name(), Default: def != nil && def.ID() == s.ID()}
	}
	return out, nil
}

// Stream is one call's audio: a capture and a playback stream on chosen devices.
type Stream struct {
	rec  *pulse.RecordStream
	play *pulse.PlaybackStream

	in, out *ring
	primed  bool // engine side only

	lastCapture                          atomic.Int64 // UnixNano of the last captured fragment
	captureDropped, playDropped, silence atomic.Uint64
}

// Open starts capture on the input and playback on the output, by device ID; an empty ID is the
// service's default device. The capture stream is pinned to its device: if the device goes away the
// service must not move the stream to another microphone.
func (c *Client) Open(input, output string) (*Stream, error) {
	s := &Stream{in: newRing(ringSamples), out: newRing(ringSamples)}
	s.lastCapture.Store(time.Now().UnixNano())

	recOpts := []pulse.RecordOption{
		pulse.RecordSampleRate(SampleRate),
		pulse.RecordLatency(0.02),
		pulse.RecordMediaName("call microphone"),
		pulse.RecordRawOption(func(r *proto.CreateRecordStream) { r.NoMove = true }),
	}
	if input != "" {
		src, err := c.c.SourceByID(input)
		if err != nil {
			return nil, fmt.Errorf("input device %q: %w", input, err)
		}
		recOpts = append(recOpts, pulse.RecordSource(src))
	}
	playOpts := []pulse.PlaybackOption{
		pulse.PlaybackSampleRate(SampleRate),
		pulse.PlaybackLatency(0.04),
		pulse.PlaybackMediaName("call audio"),
	}
	if output != "" {
		sink, err := c.c.SinkByID(output)
		if err != nil {
			return nil, fmt.Errorf("output device %q: %w", output, err)
		}
		playOpts = append(playOpts, pulse.PlaybackSink(sink))
	}

	var err error
	if s.rec, err = c.c.NewRecord(pulse.Int16Writer(s.captured), recOpts...); err != nil {
		return nil, fmt.Errorf("open capture: %w", err)
	}
	if s.play, err = c.c.NewPlayback(pulse.Int16Reader(s.wanted), playOpts...); err != nil {
		s.rec.Close()
		return nil, fmt.Errorf("open playback: %w", err)
	}
	s.rec.Start()
	s.play.Start()
	return s, nil
}

// captured is the device side of capture: it only copies into the ring.
func (s *Stream) captured(p []int16) (int, error) {
	s.lastCapture.Store(time.Now().UnixNano())
	if n := s.in.write(p); n < len(p) {
		s.captureDropped.Add(uint64(len(p) - n))
	}
	return len(p), nil
}

// wanted is the device side of playback: it only copies out of the ring, and plays silence when the
// engine has written nothing.
func (s *Stream) wanted(p []int16) (int, error) {
	n := s.out.read(p)
	if n < len(p) {
		clear(p[n:])
		s.silence.Add(uint64(len(p) - n))
	}
	return len(p), nil
}

// Capture fills frame with the next 20 ms from the microphone and reports whether it had one. The
// engine calls it once per media tick; on false it sends silence or nothing.
func (s *Stream) Capture(frame *[FrameSamples]int16) bool {
	have := s.in.len()
	if !s.primed {
		if have < primeSamples {
			return false
		}
		s.primed = true
	}
	if have < FrameSamples {
		s.primed = false
		return false
	}
	s.in.read(frame[:])
	return true
}

// Play queues 20 ms for the speaker. It never blocks: when the device is behind by more than
// playBacklog, the frame is dropped so delay cannot build up.
func (s *Stream) Play(frame *[FrameSamples]int16) {
	if s.out.len() > playBacklog-FrameSamples {
		s.playDropped.Add(FrameSamples)
		return
	}
	s.out.write(frame[:])
}

// CaptureLost reports that the microphone stopped delivering: it was unplugged or the service is
// gone. The call goes on without it; choosing another microphone is the user's decision.
func (s *Stream) CaptureLost() bool {
	return s.rec.Closed() || time.Since(time.Unix(0, s.lastCapture.Load())) > lostAfter
}

// Stats returns the counters.
func (s *Stream) Stats() Stats {
	return Stats{CaptureDropped: s.captureDropped.Load(), PlayDropped: s.playDropped.Load(), PlaySilence: s.silence.Load()}
}

// Close stops both streams.
func (s *Stream) Close() {
	s.rec.Close()
	s.play.Close()
}
