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

// meter judges a run against the threshold of design D4: at most 0.5 % of frames late or lost and
// no gap over 60 ms, in each direction. It counts against wall-clock time, so a stalled loop or a
// silent interval cannot hide in a smaller denominator, and it reads gaps from the audio itself, so
// samples that arrive together cannot hide the silence between them. Every method takes the time
// of the observation, which makes the meter testable without an audio service (TestMeter).
type meter struct {
	start time.Time

	// capture: one observation per 20 ms media tick
	captured, micFrames, missRun, maxMissRun int
	lastCapture                              time.Time
	captureGap                               time.Duration

	// playback: what the speaker played, judged in 10 ms pieces
	pending                 []int16
	first, last             time.Time // first and latest observation that brought samples
	good, badRun, maxBadRun int
	arrivalGap              time.Duration
}

const (
	piece = FrameSamples / 2 // 10 ms; both test tones have whole cycles in it
	// 60 ms of silence spoils at most seven 10 ms pieces (five whole, two partly); 80 ms at least
	// eight. The same limit in wall-clock time, seen at 20 ms ticks: 60 ms missing shows as 80 ms
	// between observations, 80 ms as 100 ms.
	maxBadPieces = 7
	maxInterval  = 90 * time.Millisecond
	maxMissTicks = 3
)

func newMeter(now time.Time) *meter { return &meter{start: now, lastCapture: now} }

// captureFrame records one media tick's capture result.
func (m *meter) captureFrame(ok bool, frame []int16, now time.Time) {
	if !ok {
		m.missRun++
		m.maxMissRun = max(m.maxMissRun, m.missRun)
		return
	}
	m.missRun = 0
	m.captureGap = max(m.captureGap, now.Sub(m.lastCapture))
	m.lastCapture = now
	m.captured++
	if share(frame, micTone) > 0.9 {
		m.micFrames++
	}
}

// heard records everything that came out of the speaker since the previous observation. The
// samples of the first observation only mark where the measured span begins.
func (m *meter) heard(p []int16, now time.Time) {
	if len(p) == 0 {
		return
	}
	counted := !m.first.IsZero()
	if counted {
		m.arrivalGap = max(m.arrivalGap, now.Sub(m.last))
	} else {
		m.first = now
	}
	m.last = now
	m.pending = append(m.pending, p...)
	for ; len(m.pending) >= piece; m.pending = m.pending[piece:] {
		if share(m.pending[:piece], speakerTone) > 0.9 {
			m.badRun = 0
			if counted {
				m.good++
			}
		} else {
			m.badRun++
			m.maxBadRun = max(m.maxBadRun, m.badRun)
		}
	}
}

// verdict closes the run at now and returns a one-line summary and every way it broke the threshold.
func (m *meter) verdict(now time.Time, st, before Stats, micHasTone bool) (string, []string) {
	m.captureGap = max(m.captureGap, now.Sub(m.lastCapture))
	expected := int(now.Sub(m.start) / (20 * time.Millisecond))
	expectedPieces := int(m.last.Sub(m.first) / (10 * time.Millisecond))
	if !m.last.IsZero() {
		m.arrivalGap = max(m.arrivalGap, now.Sub(m.last))
	}
	captureDropped := st.CaptureDropped - before.CaptureDropped
	playLost := st.PlayDropped - before.PlayDropped + st.PlaySilence - before.PlaySilence
	summary := fmt.Sprintf("capture %d of %d frames (longest miss %d ticks, gap %d ms, %d samples dropped); speaker %d of %d 10 ms pieces (longest silence %d pieces, arrival gap %d ms, %d samples dropped or silent)",
		m.captured, expected, m.maxMissRun, m.captureGap.Milliseconds(), captureDropped,
		m.good, expectedPieces, m.maxBadRun, m.arrivalGap.Milliseconds(), playLost)
	var bad []string
	if missing := expected - m.captured; float64(missing) > 0.005*float64(expected) {
		bad = append(bad, fmt.Sprintf("capture: %d of %d frames missing, over 0.5 %%", missing, expected))
	}
	if micHasTone && float64(m.captured-m.micFrames) > 0.005*float64(m.captured) {
		bad = append(bad, fmt.Sprintf("capture: the microphone tone is in only %d of %d frames", m.micFrames, m.captured))
	}
	if m.maxMissRun > maxMissTicks || m.captureGap > maxInterval {
		bad = append(bad, fmt.Sprintf("capture: gap over 60 ms (%d ticks missed in a row, %d ms between frames)", m.maxMissRun, m.captureGap.Milliseconds()))
	}
	if captureDropped > 0 {
		bad = append(bad, fmt.Sprintf("capture: %d samples dropped", captureDropped))
	}
	if expectedPieces == 0 {
		bad = append(bad, "playback: nothing was heard on the speaker")
	} else if missing := expectedPieces - m.good; float64(missing) > 0.005*float64(expectedPieces) {
		bad = append(bad, fmt.Sprintf("playback: %d of %d 10 ms pieces missing on the speaker, over 0.5 %%", missing, expectedPieces))
	}
	if m.maxBadRun > maxBadPieces || m.arrivalGap > maxInterval {
		bad = append(bad, fmt.Sprintf("playback: gap over 60 ms (%d silent pieces in a row, %d ms without samples)", m.maxBadRun, m.arrivalGap.Milliseconds()))
	}
	if float64(playLost) > 0.005*float64(expected*FrameSamples) {
		bad = append(bad, fmt.Sprintf("playback: %d samples dropped or silent, over 0.5 %%", playLost))
	}
	return summary, bad
}

// speakerVerdict judges only what the speaker played, for a path whose capture side is another
// process: the same threshold, at most 0.5 % of the 10 ms pieces missing and no gap over 60 ms.
func (m *meter) speakerVerdict(now time.Time) (string, []string) {
	expected := int(m.last.Sub(m.first) / (10 * time.Millisecond))
	if !m.last.IsZero() {
		m.arrivalGap = max(m.arrivalGap, now.Sub(m.last))
	}
	summary := fmt.Sprintf("speaker %d of %d 10 ms pieces (longest silence %d pieces, arrival gap %d ms)",
		m.good, expected, m.maxBadRun, m.arrivalGap.Milliseconds())
	var bad []string
	if expected == 0 {
		bad = append(bad, "playback: nothing was heard on the speaker")
	} else if missing := expected - m.good; float64(missing) > 0.005*float64(expected) {
		bad = append(bad, fmt.Sprintf("playback: %d of %d 10 ms pieces missing on the speaker, over 0.5 %%", missing, expected))
	}
	if m.maxBadRun > maxBadPieces || m.arrivalGap > maxInterval {
		bad = append(bad, fmt.Sprintf("playback: gap over 60 ms (%d silent pieces in a row, %d ms without samples)", m.maxBadRun, m.arrivalGap.Milliseconds()))
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
