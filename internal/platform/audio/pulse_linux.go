package audio

import (
	"fmt"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// The Linux backend speaks the PulseAudio native protocol, which PipeWire's pipewire-pulse serves
// too. It needs no C library: the client is Go.

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
	*buffers
	rec  *pulse.RecordStream
	play *pulse.PlaybackStream
}

// Open starts capture on the input and playback on the output, by device ID; an empty ID is the
// service's default device. The capture stream is pinned to its device: if the device goes away the
// service must not move the stream to another microphone.
func (c *Client) Open(input, output string) (*Stream, error) {
	s := &Stream{buffers: newBuffers()}

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
	if s.rec, err = c.c.NewRecord(pulse.Int16Writer(func(p []int16) (int, error) { s.captured(p); return len(p), nil }), recOpts...); err != nil {
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

// CaptureLost reports that the microphone stopped delivering: it was unplugged or the service is
// gone. The call goes on without it; choosing another microphone is the user's decision.
func (s *Stream) CaptureLost() bool {
	return s.rec.Closed() || s.stale()
}

// Close stops both streams.
func (s *Stream) Close() {
	s.rec.Close()
	s.play.Close()
}
