package engine

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	dmedia "github.com/emiago/diago/media"
	gosip "github.com/emiago/sipgo/sip"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/media"
	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// dialog is what the actor needs of a call's SIP dialog, outgoing or incoming.
type dialog interface {
	Context() context.Context
	Hangup(ctx context.Context) error
	Hold(ctx context.Context) error
	Unhold(ctx context.Context) error
	Close() error
}

// call is the engine's one call. The actor owns every field except the atomics, which the audio
// goroutines read.
type call struct {
	snap domain.CallSnapshot

	cancel context.CancelFunc // an outgoing call that is not answered yet
	dialog dialog
	server *diago.DialogServerSession // set for an incoming call
	refer  func(ctx context.Context, to gosip.Uri, notify func(status int)) error
	media  *diago.DialogMedia

	// rung is closed once 180 Ringing is out. The final response waits for it: the library keeps the
	// last response it sent as the dialog's, and a 180 written after the 200 made it refuse the BYE.
	rung      chan struct{}
	answering bool             // Answer is on its way
	busy      bool             // a hold or resume is on its way
	ending    domain.EndReason // why this side is ending the call

	sound       bool // the audio goroutines run
	audio       ports.MediaIO
	stop        context.CancelFunc
	pumps       sync.WaitGroup
	held, muted atomic.Bool
	keys        chan domain.DTMFKey
	// released is closed once the audio is stopped and the sound devices are closed.
	released chan struct{}
	// What the audio goroutines did, for the summary logged when the call's audio ends.
	sent, unfed, received atomic.Uint64
	log                   *slog.Logger
}

// startAudio connects the call's RTP to the sound devices: one goroutine sends the microphone, one
// plays what arrives. Neither touches the actor.
// micLost is called once, from the audio goroutine, when the microphone stops delivering.
func (c *call) startAudio(med *diago.DialogMedia, audio ports.MediaIO, log *slog.Logger, micLost func()) error {
	codec := media.PCMA
	if dmedia.CodecAudioFromSession(med.MediaSession()).PayloadType == media.PCMU.PayloadType() {
		codec = media.PCMU
	}
	// The DTMF writer wraps the audio writer, so keys and audio share one RTP stream in order.
	writer, err := med.AudioWriterDTMF()
	if err != nil {
		return err
	}
	// A fixed, bounded jitter buffer: late and reordered packets are put right, bursts are capped.
	reader, err := med.AudioReader(diago.WithAudioReaderJitterBuffer(dmedia.RTPJitterBufferOptions{DelayPackets: 3, MaxPackets: 10}))
	if err != nil {
		return err
	}
	ctx, stop := context.WithCancel(context.Background())
	c.audio, c.stop, c.sound, c.log = audio, stop, true, log
	c.pumps.Add(2)

	go func() { // microphone -> network
		defer c.pumps.Done()
		var frame, silence [media.FrameSamples]int16
		payload := make([]byte, media.FrameSamples)
		lost := false
		for ctx.Err() == nil {
			select {
			case key := <-c.keys:
				if err := writer.WriteDTMF(key.Rune()); err != nil {
					return
				}
				continue
			default:
			}
			if c.held.Load() {
				// On hold nothing is sent: the library would keep the stream going.
				time.Sleep(20 * time.Millisecond)
				continue
			}
			out := &frame
			if !audio.Capture(&frame) {
				out = &silence // no frame yet: silence keeps the stream alive
				c.unfed.Add(1)
				if !lost && audio.CaptureLost() {
					lost = true // said once; no other microphone is opened here
					micLost()
				}
			} else if c.muted.Load() {
				out = &silence
			}
			codec.Encode(payload, out)
			c.sent.Add(1)
			if _, err := writer.Write(payload); err != nil { // paced by the RTP clock: 20 ms
				if ctx.Err() == nil {
					log.Debug("audio send ended", "error", err)
				}
				return
			}
		}
	}()

	go func() { // network -> speaker
		defer c.pumps.Done()
		var frame [media.FrameSamples]int16
		buf := make([]byte, dmedia.RTPBufSize)
		for ctx.Err() == nil {
			n, err := reader.Read(buf)
			if err != nil {
				if ctx.Err() == nil {
					log.Debug("audio receive ended", "error", err)
				}
				return
			}
			if codec.Decode(&frame, buf[:n]) {
				c.received.Add(1)
				audio.Play(&frame)
			}
		}
	}()
	return nil
}

// release stops the audio and frees the dialog. It does not wait for the network.
func (c *call) release() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.stop != nil {
		c.stop()
	}
	med, dlg, audio, sound := c.media, c.dialog, c.audio, c.sound
	c.sound = false
	released := make(chan struct{})
	c.released = released
	go func() {
		defer close(released)
		if med != nil {
			_ = med.Close() // unblocks the receive goroutine
		}
		if sound {
			c.pumps.Wait()
			audio.Close()
			// Counts only: how many 20 ms frames went out, how many of them had no microphone
			// audio to carry, and how many came in.
			c.log.Info("call audio ended", "frames_sent", c.sent.Load(), "frames_without_capture", c.unfed.Load(), "frames_received", c.received.Load())
		}
		if dlg != nil {
			_ = dlg.Close()
		}
	}()
}
