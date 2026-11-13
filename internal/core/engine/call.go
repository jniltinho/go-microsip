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
	tone      *tone            // the ring or the ringback that plays while the call rings
	answering bool             // Answer is on its way: the final response is its 200, nobody else's
	busy      bool             // a hold or resume is on its way
	ending    domain.EndReason // why this side is ending the call
	// gone: the other side ended the dialog while a transfer waited for its verdict; the transfer's
	// result ends the call.
	gone bool

	sound bool // the audio goroutines run
	// earlyFailed: the audio that played before the answer ended on its own; the call is ended
	// when it is answered.
	earlyFailed bool
	audio       ports.MediaIO
	stop        context.CancelFunc
	pumps       sync.WaitGroup
	// privacy is hold, mute and the count of their changes in one word (see private), so the send
	// goroutine never sees one without the others: a word it has seen before means that nothing
	// changed in between.
	privacy atomic.Uint64
	keys    chan domain.DTMFKey
	// released is closed once the audio is stopped and the sound devices are closed; nil until the
	// call's audio was told to stop.
	released chan struct{}
	// settled is closed once the sound devices are no longer opening: they run, they failed, or
	// they were closed again. Devices that are open and not yet running are in opened, guarded by
	// handoff together with stopped: whoever takes them out, the actor to start them or stopAudio
	// to close them, owns them, so neither waits for the other.
	settled chan struct{}
	handoff sync.Mutex
	opened  ports.MediaIO
	stopped bool
	// What the audio goroutines did, for the summary logged when the call's audio ends.
	sent, unfed, received atomic.Uint64
	log                   *slog.Logger
}

// startAudio connects the call's RTP to the sound devices: one goroutine sends the microphone, one
// plays what arrives. Neither touches the actor. micLost is called once when the microphone stops
// delivering; failed is called once when a direction of the media ends on its own.
func (c *call) startAudio(med *diago.DialogMedia, audio ports.MediaIO, log *slog.Logger, micLost func(), failed func(direction string, err error)) error {
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
	var once sync.Once
	// A direction that ends while the call is up is the call's problem, said once; one that ends
	// because the call's audio was stopped is not.
	ended := func(direction string, err error) {
		if ctx.Err() == nil {
			once.Do(func() { failed(direction, err) })
		}
	}

	go func() { // microphone -> network
		defer c.pumps.Done()
		mic := microphone{audio: audio}
		payload := make([]byte, media.FrameSamples)
		lost := false
		quiet := 0 // ticks in a row with nothing to send
		var silence [media.FrameSamples]int16
		for ctx.Err() == nil {
			select {
			case key := <-c.keys:
				if err := writer.WriteDTMF(key.Rune()); err != nil {
					ended("send", err)
					return
				}
				continue
			default:
			}
			out, fed := mic.next(c.privacy.Load)
			if out == nil {
				// On hold, and before the answer, nothing of the microphone is sent: the library
				// would keep the stream going.
				time.Sleep(20 * time.Millisecond)
				// One frame of silence a second says that this side is still there. A firewall
				// or a VPN gateway on the way closes a path that only carries the other side's
				// audio (music on hold, an operator's ringing tone) for some seconds.
				if quiet++; quiet%keepaliveTicks == 0 {
					// Best effort: a frame that cannot be sent now is not the call's failure; one
					// that carries the microphone and cannot be sent is, when there is one.
					codec.Encode(payload, &silence)
					_, _ = writer.Write(payload)
				}
				continue
			}
			quiet = 0
			if !fed {
				c.unfed.Add(1)
				if !lost && audio.CaptureLost() {
					lost = true // said once; no other microphone is opened here
					micLost()
				}
			}
			codec.Encode(payload, out)
			c.sent.Add(1)
			if _, err := writer.Write(payload); err != nil { // paced by the RTP clock: 20 ms
				ended("send", err)
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
				ended("receive", err)
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

// offer hands over sound devices that were just opened, from the goroutine that opened them. It
// reports false when the call's audio was stopped meanwhile: the devices are then closed here.
func (c *call) offer(audio ports.MediaIO) bool {
	c.handoff.Lock()
	if c.stopped {
		c.handoff.Unlock()
		audio.Close()
		close(c.settled)
		return false
	}
	c.opened = audio
	c.handoff.Unlock()
	return true
}

// take removes the offered devices; nil when there are none or stopAudio took them.
func (c *call) take() ports.MediaIO {
	c.handoff.Lock()
	defer c.handoff.Unlock()
	audio := c.opened
	c.opened = nil
	return audio
}

// microphone decides what each 20 ms tick of a call sends. What the microphone records while the
// call is held or muted is the user's own: it is never sent, not then and not afterwards.
type microphone struct {
	audio          ports.MediaIO
	frame, silence [media.FrameSamples]int16
	// seen is the privacy word this microphone has acted on: what the device held was dropped
	// after that word was read.
	seen uint64
}

// next returns the frame to send, or nil when nothing is sent this tick. fed is false when the
// frame is silence because the microphone had nothing. privacy reads the call's privacy word; it
// is read again once the frame is taken, as the actor may change it at any moment in between.
//
// A frame is sent only if the word was the same, without hold or mute, when the device's queue was
// last dropped, before the frame was taken and after: it then holds only what was recorded while
// the call was neither held nor muted. A frame taken just before a hold or a mute begins may still
// leave just after it.
func (m *microphone) next(privacy func() uint64) (out *[media.FrameSamples]int16, fed bool) {
	word := privacy()
	if word&(onHold|onMute|onEarly) != 0 {
		m.audio.Capture(&m.frame) // read and dropped, so the device's queue does not fill up
		if word&(onHold|onEarly) != 0 {
			return nil, true
		}
		return &m.silence, true // muted: silence keeps the stream alive
	}
	if word != m.seen {
		// Resumed or unmuted, seen at a tick or not: a hold or a mute that began and ended between
		// two ticks shows only in the count. Whatever the device still holds may have been
		// recorded then: it is dropped at the source, so the frames from here on are made only of
		// what is recorded from now.
		m.seen = word
		m.audio.Discard()
		return &m.silence, true
	}
	got := m.audio.Capture(&m.frame)
	if privacy() != word {
		// A hold or a mute began while the frame was being taken, and may be over already: the
		// frame may hold what was recorded then. It is not sent, and the word, not acted on,
		// makes a later tick drop the rest.
		return &m.silence, true
	}
	if !got {
		return &m.silence, false // no frame yet: silence keeps the stream alive
	}
	return &m.frame, true
}

// keepaliveTicks is how many 20 ms ticks pass between two frames of silence while a call sends
// nothing of its own: one a second.
const keepaliveTicks = 50

// The privacy word: hold, mute, a call that is not answered yet, and above them the count of
// changes. Before the answer a call may hear what the other side plays (early media) and sends
// nothing, as on hold.
const (
	onHold uint64 = 1 << iota
	onMute
	onEarly
	privacyChange
)

// private sets or clears held or muted, for the actor: the flag and the count change at once.
func (c *call) private(flag uint64, on bool) {
	for {
		old := c.privacy.Load()
		word := (old &^ flag) + privacyChange
		if on {
			word |= flag
		}
		if c.privacy.CompareAndSwap(old, word) {
			return
		}
	}
}

// stopAudio stops the call's audio and closes the sound devices, beside the caller. It is safe to
// call more than once; released is closed when the devices are closed.
func (c *call) stopAudio() {
	if c.released != nil {
		return
	}
	released := make(chan struct{})
	c.released = released
	c.handoff.Lock()
	c.stopped = true
	offered := c.opened // opened and not started yet: closed here, whatever the actor is doing
	c.opened = nil
	c.handoff.Unlock()
	if c.stop != nil {
		c.stop()
	}
	med, audio, sound, settled := c.media, c.audio, c.sound, c.settled
	c.sound = false
	go func() {
		defer close(released)
		if offered != nil {
			offered.Close()
			close(settled)
		}
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
	}()
}

// release stops the audio and frees the dialog. It does not wait for the network.
func (c *call) release() {
	if c.cancel != nil {
		c.cancel()
	}
	c.stopAudio()
	if dlg, released := c.dialog, c.released; dlg != nil {
		go func() {
			<-released
			_ = dlg.Close()
		}()
	}
}
