package audio

import (
	"math"
	"os"
	"strconv"
	"testing"
)

// The device tests need a real audio service (build/qa/audio.sh starts PipeWire in the build
// container; build/vm runs them on the distributions and on Windows) and are skipped without
// GO_MICROSIP_AUDIO_TEST=1.

const (
	micTone     = 700.0
	speakerTone = 1300.0
)

func needServer(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_MICROSIP_AUDIO_TEST") != "1" {
		t.Skip("set GO_MICROSIP_AUDIO_TEST=1 with an audio service running (build/qa/audio.sh)")
	}
}

// testSeconds is how long the threshold run lasts: GO_MICROSIP_AUDIO_SECONDS, default 10.
func testSeconds() int {
	if v, err := strconv.Atoi(os.Getenv("GO_MICROSIP_AUDIO_SECONDS")); err == nil && v > 0 {
		return v
	}
	return 10
}

func sine(freq float64, n *int, p []int16) {
	for i := range p {
		p[i] = int16(12000 * math.Sin(2*math.Pi*freq*float64(*n)/SampleRate))
		*n++
	}
}

// share is the part of the signal's energy at freq (Goertzel); 1 is a pure tone.
func share(samples []int16, freq float64) float64 {
	coeff := 2 * math.Cos(2*math.Pi*freq/SampleRate)
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

// toneBlocks is the fraction of 20 ms blocks that carry freq. Unlike share over the whole signal, it
// does not collapse when the tone's phase jumps once.
func toneBlocks(samples []int16, freq float64) float64 {
	blocks, good := 0, 0
	for ; len(samples) >= FrameSamples; samples = samples[FrameSamples:] {
		blocks++
		if share(samples[:FrameSamples], freq) > 0.9 {
			good++
		}
	}
	if blocks == 0 {
		return 0
	}
	return float64(good) / float64(blocks)
}
