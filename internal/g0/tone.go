package g0

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/audio"
	"github.com/emiago/diago/media"
)

const (
	sampleRate   = 8000
	frameSamples = 160 // 20 ms
	frameTime    = 20 * time.Millisecond
)

// SendTone writes a sine of freq Hz as 20 ms frames, paced in real time, until ctx ends.
func SendTone(ctx context.Context, med *diago.DialogMedia, freq float64) error {
	w, err := med.AudioWriter()
	if err != nil {
		return err
	}
	return SendToneTo(ctx, med, w, freq)
}

// sendToneGated is SendTone that sends nothing while paused is set.
func sendToneGated(ctx context.Context, med *diago.DialogMedia, freq float64, paused *atomic.Bool) error {
	w, err := med.AudioWriter()
	if err != nil {
		return err
	}
	return SendToneTo(ctx, med, gate{w, paused}, freq)
}

type gate struct {
	w      io.Writer
	paused *atomic.Bool
}

func (g gate) Write(b []byte) (int, error) {
	if g.paused.Load() {
		return len(b), nil
	}
	return g.w.Write(b)
}

// SendToneTo is SendTone on a given writer of encoded audio, such as the DTMF writer, which
// serializes audio frames and digit events on one stream.
func SendToneTo(ctx context.Context, med *diago.DialogMedia, w io.Writer, freq float64) error {
	codec := media.CodecAudioFromSession(med.MediaSession())
	enc, err := audio.NewPCMEncoderWriter(codec.PayloadType, w)
	if err != nil {
		return err
	}
	frame := make([]byte, frameSamples*2)
	tick := time.NewTicker(frameTime)
	defer tick.Stop()
	for n := 0; ; {
		for i := range frameSamples {
			v := int16(12000 * math.Sin(2*math.Pi*freq*float64(n)/sampleRate))
			binary.LittleEndian.PutUint16(frame[i*2:], uint16(v))
			n++
		}
		if _, err := enc.Write(frame); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// Heard is what a listener measured: the share of the signal's energy at each probed frequency.
type Heard struct {
	Frames int
	Share  map[float64]float64
}

// Has reports whether freq carries most of the received energy.
func (h Heard) Has(freq float64) bool { return h.Frames > 0 && h.Share[freq] > 0.5 }

// Listen decodes received audio for dur and measures the energy share of each frequency (Goertzel).
// The first 400 ms are skipped: the stream may start with silence or a codec transient.
func Listen(ctx context.Context, med *diago.DialogMedia, dur time.Duration, freqs ...float64) (Heard, error) {
	r, err := med.AudioReader()
	if err != nil {
		return Heard{}, err
	}
	codec := media.CodecAudioFromSession(med.MediaSession())
	dec, err := audio.NewPCMDecoderReader(codec.PayloadType, r)
	if err != nil {
		return Heard{}, err
	}
	type goertzel struct{ coeff, s1, s2 float64 }
	gs := make([]goertzel, len(freqs))
	for i, f := range freqs {
		gs[i].coeff = 2 * math.Cos(2*math.Pi*f/sampleRate)
	}
	var total float64
	var samples, frames int
	buf := make([]byte, media.RTPBufSize)
	start := time.Now()
	deadline := start.Add(dur)
	// The read deadline ends the loop when the peer sends nothing (a held call, for instance).
	_ = med.StopRTP(1, dur)
	defer func() { _ = med.StartRTP(1, 0) }()
	for time.Now().Before(deadline) && ctx.Err() == nil {
		n, err := dec.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			return Heard{Frames: frames}, err
		}
		if n == 0 || time.Since(start) < 400*time.Millisecond {
			continue
		}
		frames++
		for i := 0; i+1 < n; i += 2 {
			x := float64(int16(binary.LittleEndian.Uint16(buf[i:])))
			total += x * x
			samples++
			for j := range gs {
				s := x + gs[j].coeff*gs[j].s1 - gs[j].s2
				gs[j].s2, gs[j].s1 = gs[j].s1, s
			}
		}
	}
	h := Heard{Frames: frames, Share: map[float64]float64{}}
	if samples == 0 || total == 0 {
		return h, nil
	}
	for j, f := range freqs {
		g := gs[j]
		power := g.s1*g.s1 + g.s2*g.s2 - g.coeff*g.s1*g.s2
		// A pure tone of N samples yields power N/2 times its energy.
		h.Share[f] = power / (float64(samples) / 2 * total)
	}
	return h, nil
}
