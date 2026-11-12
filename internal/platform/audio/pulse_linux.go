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
	c    *pulse.Client
	rec  *pulse.RecordStream
	play *pulse.PlaybackStream
}

// Open starts capture on the input and playback on the output, by device ID; an empty ID is the
// service's default device. The capture stream is pinned to its device: if the device goes away the
// service must not move the stream to another microphone.
func (c *Client) Open(input, output string) (*Stream, error) {
	s := &Stream{buffers: newBuffers(), c: c.c}

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
	playOpts, err := c.playback(output, "call audio")
	if err != nil {
		return nil, err
	}

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

// playback is how a playback stream is asked for on the output, by device ID.
func (c *Client) playback(output, name string) ([]pulse.PlaybackOption, error) {
	opts := []pulse.PlaybackOption{
		pulse.PlaybackSampleRate(SampleRate),
		pulse.PlaybackLatency(0.04),
		pulse.PlaybackMediaName(name),
	}
	if output != "" {
		sink, err := c.c.SinkByID(output)
		if err != nil {
			return nil, fmt.Errorf("output device %q: %w", output, err)
		}
		opts = append(opts, pulse.PlaybackSink(sink))
	}
	return opts, nil
}

// Output is a playback stream alone: what plays while a call has no audio of its own, such as
// its ring. No microphone is opened for it.
type Output struct {
	*buffers
	play *pulse.PlaybackStream
}

// OpenOutput starts playback alone on the output, by device ID; an empty ID is the service's
// default device.
func (c *Client) OpenOutput(output string) (*Output, error) {
	opts, err := c.playback(output, "ring")
	if err != nil {
		return nil, err
	}
	o := &Output{buffers: newBuffers()}
	if o.play, err = c.c.NewPlayback(pulse.Int16Reader(o.wanted), opts...); err != nil {
		return nil, fmt.Errorf("open playback: %w", err)
	}
	o.play.Start()
	return o, nil
}

// Close stops the stream.
func (o *Output) Close() { o.play.Close() }

// wanted is the device side of playback: it only copies out of the ring, and plays silence when the
// engine has written nothing.
func (b *buffers) wanted(p []int16) (int, error) {
	n := b.out.read(p)
	if n < len(p) {
		clear(p[n:])
		b.silence.Add(uint64(len(p) - n))
	}
	return len(p), nil
}

// Discard drops everything the microphone has recorded so far. The service is asked to drop what
// it holds; its answer comes through the connection that delivers the recordings, in order, so
// when it is here everything the service sent before it has been delivered, and dropped. What the
// device had recorded and not passed on yet is counted out of what follows, by the latency the
// service reports for it. If the service does not answer, nothing more is taken from this
// microphone, and CaptureLost says so.
func (s *Stream) Discard() {
	s.discardBegin()
	index := s.rec.StreamIndex()
	if err := s.c.RawRequest(&proto.FlushRecordStream{StreamIndex: index}, nil); err != nil {
		return
	}
	var latency proto.GetRecordLatencyReply
	if err := s.c.RawRequest(&proto.GetRecordLatency{StreamIndex: index}, &latency); err != nil {
		return
	}
	s.discardEnd(discardDepth + int(latency.Latency)*SampleRate/1_000_000)
}

// CaptureLost reports that the microphone stopped delivering: it was unplugged or the service is
// gone. The call goes on without it; choosing another microphone is the user's decision.
func (s *Stream) CaptureLost() bool {
	return s.rec.Closed() || s.stale()
}

// PlaybackLost reports that the speaker stream ended: the device or the service is gone.
func (s *Stream) PlaybackLost() bool { return s.play.Closed() }

// Close stops both streams.
func (s *Stream) Close() {
	s.rec.Close()
	s.play.Close()
}
