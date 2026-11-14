package audio

import (
	"math"
	"testing"
)

// tone48 is a tone at the capture rate.
func tone48(freq float64, samples int) []int16 {
	p := make([]int16, samples)
	for i := range p {
		p[i] = int16(12000 * math.Sin(2*math.Pi*freq*float64(i)/captureRate))
	}
	return p
}

func energy(p []int16) float64 {
	sum := 0.0
	for _, v := range p {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(p)))
}

func TestTheDecimatorKeepsSpeechAndRemovesWhatWouldFoldBack(t *testing.T) {
	convert := func(freq float64) []int16 {
		var d decimator
		in := tone48(freq, captureRate) // one second
		out := make([]int16, len(in)/decimation+1)
		return out[decimatorTaps:d.convert(in, out)] // past the filter's start
	}
	for _, freq := range []float64{300, 1000, 3000} {
		out := convert(freq)
		if level, kept := energy(out)/(12000/math.Sqrt2), share(out, freq); level < 0.85 || level > 1.05 || kept < 0.95 {
			t.Errorf("%v Hz: level %.2f of the input's, %.2f of it at that tone", freq, level, kept)
		}
	}
	// 5 kHz would come out as 3 kHz, 7 kHz as 1 kHz: at least 40 dB down.
	for _, freq := range []float64{5000, 7000, 12000, 20000} {
		if level := energy(convert(freq)) / (12000 / math.Sqrt2); level > 0.01 {
			t.Errorf("%v Hz: level %.4f of the input's passed", freq, level)
		}
	}
}

func TestTheDecimatorGivesTheSameForAnySizeOfBuffer(t *testing.T) {
	in := tone48(700, captureRate/2)
	var whole decimator
	want := make([]int16, len(in)/decimation+1)
	want = want[:whole.convert(in, want)]
	if len(want) != len(in)/decimation {
		t.Fatalf("%d samples from %d", len(want), len(in))
	}
	for _, sizes := range [][]int{{1}, {5, 7}, {64, 320, 960, 1}, {959, 961}} {
		var d decimator
		got := make([]int16, 0, len(want))
		scratch := make([]int16, 1024)
		for at, i := 0, 0; at < len(in); i++ {
			size := min(sizes[i%len(sizes)], len(in)-at)
			got = append(got, scratch[:d.convert(in[at:at+size], scratch)]...)
			at += size
		}
		if len(got) != len(want) {
			t.Fatalf("buffers of %v: %d samples, want %d", sizes, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("buffers of %v: sample %d is %d, want %d", sizes, i, got[i], want[i])
			}
		}
	}
}
