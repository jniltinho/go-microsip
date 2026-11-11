package audio

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
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

// meter judges a run against the threshold of design D4 (at most 0.5 % of frames late or lost, no
// gap over 60 ms) from wall-clock time: frames are counted against the time that passed, so a
// stalled loop or a silent interval cannot hide in a smaller denominator.
type meter struct {
	start, lastCapture, lastTone    time.Time
	captured, micBlocks, toneBlocks int
	captureGap, playGap             time.Duration
	pending                         []int16
}

func newMeter() *meter {
	now := time.Now()
	return &meter{start: now, lastCapture: now, lastTone: now}
}

// captureFrame records one media tick's capture result.
func (m *meter) captureFrame(ok bool, frame []int16) {
	if !ok {
		return
	}
	now := time.Now()
	m.captureGap = max(m.captureGap, now.Sub(m.lastCapture))
	m.lastCapture = now
	m.captured++
	if share(frame, micTone) > 0.9 {
		m.micBlocks++
	}
}

// heard records what came out of the speaker since the last call.
func (m *meter) heard(p []int16) {
	m.pending = append(m.pending, p...)
	for ; len(m.pending) >= FrameSamples; m.pending = m.pending[FrameSamples:] {
		if share(m.pending[:FrameSamples], speakerTone) > 0.9 {
			now := time.Now()
			m.playGap = max(m.playGap, now.Sub(m.lastTone))
			m.lastTone = now
			m.toneBlocks++
		}
	}
}

// maxGap is 60 ms of missing audio as this meter sees it: 80 ms between consecutive good frames,
// plus one tick of measuring granularity.
const maxGap = 100 * time.Millisecond

// verdict closes the run and returns a one-line summary and every way it broke the threshold.
func (m *meter) verdict(st, before Stats, micHasTone bool) (string, []string) {
	now := time.Now()
	m.captureGap = max(m.captureGap, now.Sub(m.lastCapture))
	m.playGap = max(m.playGap, now.Sub(m.lastTone))
	expected := int(now.Sub(m.start) / (20 * time.Millisecond))
	allowed := int(0.005 * float64(expected))
	captureDropped, playLost := st.CaptureDropped-before.CaptureDropped, st.PlayDropped-before.PlayDropped+st.PlaySilence-before.PlaySilence
	summary := fmt.Sprintf("%d frames expected: captured %d (longest gap %d ms, %d samples dropped), heard on the speaker %d (longest gap %d ms, %d samples dropped or silent)",
		expected, m.captured, m.captureGap.Milliseconds(), captureDropped, m.toneBlocks, m.playGap.Milliseconds(), playLost)
	var bad []string
	// Two frames of slack at the edges of the run; five on the speaker for the pipeline's delay.
	if m.captured < expected-allowed-2 {
		bad = append(bad, fmt.Sprintf("capture: %d of %d frames missing, over 0.5 %%", expected-m.captured, expected))
	}
	if micHasTone && m.micBlocks < m.captured-allowed {
		bad = append(bad, fmt.Sprintf("capture: the microphone tone is in only %d of %d frames", m.micBlocks, m.captured))
	}
	if m.captureGap > maxGap {
		bad = append(bad, fmt.Sprintf("capture: gap of %d ms", m.captureGap.Milliseconds()))
	}
	if captureDropped > 0 {
		bad = append(bad, fmt.Sprintf("capture: %d samples dropped", captureDropped))
	}
	if m.toneBlocks < expected-allowed-5 {
		bad = append(bad, fmt.Sprintf("playback: %d of %d frames missing on the speaker, over 0.5 %%", expected-m.toneBlocks, expected))
	}
	if m.playGap > maxGap {
		bad = append(bad, fmt.Sprintf("playback: gap of %d ms", m.playGap.Milliseconds()))
	}
	if float64(playLost) > 0.005*float64(expected*FrameSamples) {
		bad = append(bad, fmt.Sprintf("playback: %d samples dropped or silent, over 0.5 %%", playLost))
	}
	return summary, bad
}

// mustCatch fails the test unless the verdict of a deliberately stalled run names both directions:
// the proof that the threshold checks can fail.
func mustCatch(t *testing.T, bad []string) {
	t.Helper()
	all := strings.Join(bad, "; ")
	if !strings.Contains(all, "capture:") || !strings.Contains(all, "playback:") {
		t.Fatalf("a 300 ms stall was not caught in both directions: %q", all)
	}
	t.Logf("control: a 300 ms stall is caught: %s", all)
}
