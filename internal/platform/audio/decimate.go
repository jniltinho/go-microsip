package audio

import "math"

// Capture at 48 kHz, brought down to the 8 kHz the engine carries (add-macos-universal, design
// D4: "if it does not convert, or not well enough, conversion goes into the adapter"). macOS's
// AudioQueue hands a capture queue about 1024 frames at a time whatever they last: 128 ms at
// 8 kHz, which is more than the engine waits for, and 21 ms at 48 kHz. Both rates are the
// queue's, on one clock, so one sample in six is exact and nothing drifts.
//
// The file has no build constraint so that its tests run wherever the project's tests run.

const (
	// captureRate is the rate a capture queue is opened at.
	captureRate = 6 * SampleRate
	// decimation is how many captured samples make one of the engine's.
	decimation = captureRate / SampleRate
	// decimatorTaps is the length of the low-pass: 8 ms of history at 48 kHz.
	decimatorTaps = 96
)

// lowPass is a windowed sinc that passes speech and keeps what lies above 4 kHz from folding
// back into it: its cutoff is 3.6 kHz.
var lowPass = func() (taps [decimatorTaps]float32) {
	const cutoff = 3600.0 / captureRate
	sum := 0.0
	var ideal [decimatorTaps]float64
	for i := range ideal {
		x := float64(i) - (decimatorTaps-1)/2.0
		sinc := 2 * cutoff
		if x != 0 {
			sinc = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
		}
		window := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/(decimatorTaps-1)) // Hamming
		ideal[i] = sinc * window
		sum += ideal[i]
	}
	for i := range taps {
		taps[i] = float32(ideal[i] / sum)
	}
	return taps
}()

// decimator is the state of one capture queue's conversion. It belongs to its queue: a discard
// retires both, so no sample recorded before it colours what comes after.
type decimator struct {
	history [decimatorTaps]int16 // the last samples, a ring
	next    int                  // where the next one goes
	phase   int                  // samples taken since the last one given
}

// convert takes captured samples, however many, and writes the engine's samples they complete
// to out, returning how many. out must hold len(in)/decimation+1.
func (d *decimator) convert(in, out []int16) int {
	n := 0
	for _, sample := range in {
		d.history[d.next] = sample
		d.next = (d.next + 1) % decimatorTaps
		if d.phase++; d.phase < decimation {
			continue
		}
		d.phase = 0
		sum := float32(0)
		for i, tap := range lowPass {
			sum += tap * float32(d.history[(d.next+i)%decimatorTaps])
		}
		out[n] = int16(max(-32768, min(32767, sum)))
		n++
	}
	return n
}
