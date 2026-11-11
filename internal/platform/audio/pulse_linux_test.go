package audio

import (
	"errors"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
)

// These tests need a PulseAudio-protocol server (build/qa/audio.sh starts PipeWire in the build
// container) and are skipped without GO_MICROSIP_AUDIO_TEST=1.

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

func pactl(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("pactl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("pactl %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// nullSink adds a virtual sound card: a sink and its monitor source.
func nullSink(t *testing.T, name string) (unload func()) {
	t.Helper()
	id := pactl(t, "load-module", "module-null-sink", "sink_name="+name, "rate=48000", "channels=2")
	done := false
	unload = func() {
		if !done {
			done = true
			pactl(t, "unload-module", id)
		}
	}
	t.Cleanup(unload)
	return unload
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

// A session without an audio service is reported as such.
func TestNoService(t *testing.T) {
	t.Setenv("PULSE_SERVER", "unix:"+t.TempDir()+"/absent")
	if _, err := Connect(); !errors.Is(err, ErrNoService) {
		t.Fatalf("Connect = %v, want ErrNoService", err)
	}
}

// The adapter conformance test: enumerate, open by ID, capture and play measured tones within the
// frame-loss threshold, and lose the microphone without switching to another one.
func TestPulseLoopback(t *testing.T) {
	needServer(t)
	nullSink(t, "g0spk")
	unplugMic := nullSink(t, "g0mic")
	nullSink(t, "g0other") // another microphone the stream must never move to

	c, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	inputs, err := c.Inputs()
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := c.Outputs()
	if err != nil {
		t.Fatal(err)
	}
	has := func(list []Device, id string) bool {
		for _, d := range list {
			if d.ID == id && d.Name != "" {
				return true
			}
		}
		return false
	}
	if !has(inputs, "g0mic.monitor") || !has(outputs, "g0spk") {
		t.Fatalf("devices not listed: inputs %v outputs %v", inputs, outputs)
	}
	if _, err := c.Open("no-such-device", ""); err == nil {
		t.Fatal("opening an unknown input succeeded")
	}

	// Test fixtures on a second connection: a "voice" into the virtual microphone and a listener
	// on the virtual speaker.
	fx, err := pulse.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	defer fx.Close()
	micSink, err := fx.SinkByID("g0mic")
	if err != nil {
		t.Fatal(err)
	}
	spkSink, err := fx.SinkByID("g0spk")
	if err != nil {
		t.Fatal(err)
	}
	voiceN := 0
	voice, err := fx.NewPlayback(pulse.Int16Reader(func(p []int16) (int, error) { sine(micTone, &voiceN, p); return len(p), nil }),
		pulse.PlaybackSampleRate(SampleRate), pulse.PlaybackSink(micSink), pulse.PlaybackLatency(0.04))
	if err != nil {
		t.Fatal(err)
	}
	defer voice.Close()
	heard := make(chan []int16, 4096)
	listener, err := fx.NewRecord(pulse.Int16Writer(func(p []int16) (int, error) {
		select {
		case heard <- append([]int16(nil), p...):
		default:
		}
		return len(p), nil
	}), pulse.RecordSampleRate(SampleRate), pulse.RecordMonitor(spkSink), pulse.RecordLatency(0.02))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	voice.Start()
	listener.Start()

	s, err := c.Open("g0mic.monitor", "g0spk")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	seconds := 10
	if v, err := strconv.Atoi(os.Getenv("GO_MICROSIP_AUDIO_SECONDS")); err == nil && v > 0 {
		seconds = v
	}
	toneN := 0
	// The engine's media tick: every 20 ms take one captured frame and play one frame.
	run := func(d time.Duration) (frames, missed, longestMiss int, captured []int16) {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		var in, out [FrameSamples]int16
		run := 0
		for end := time.Now().Add(d); time.Now().Before(end); <-tick.C {
			frames++
			if s.Capture(&in) {
				run = 0
				captured = append(captured, in[:]...)
			} else {
				missed++
				run++
				longestMiss = max(longestMiss, run)
			}
			sine(speakerTone, &toneN, out[:])
			s.Play(&out)
		}
		return
	}
	run(time.Second) // streams start and settle
	before := s.Stats()
	for len(heard) > 0 {
		<-heard
	}
	frames, missed, longest, captured := run(time.Duration(seconds) * time.Second)
	st := s.Stats()
	var played []int16
	for len(heard) > 0 {
		played = append(played, <-heard...)
	}
	silence := st.PlaySilence - before.PlaySilence
	t.Logf("%d s: %d frames, %d capture misses (longest run %d), capture dropped %d, play dropped %d, play silence %d samples, mic tone in %.3f of blocks, speaker tone in %.3f of blocks",
		seconds, frames, missed, longest, st.CaptureDropped-before.CaptureDropped, st.PlayDropped-before.PlayDropped,
		silence, toneBlocks(captured, micTone), toneBlocks(played, speakerTone))

	if got := toneBlocks(captured, micTone); got < 0.98 {
		t.Errorf("the microphone tone was captured in only %.3f of the blocks", got)
	}
	if got := toneBlocks(played, speakerTone); got < 0.98 {
		t.Errorf("the played tone reached the speaker in only %.3f of the blocks", got)
	}
	// Threshold of design D4: at most 0.5 % of frames late or lost, no gap over 60 ms.
	if float64(missed) > 0.005*float64(frames) {
		t.Errorf("%d of %d capture frames missed, over 0.5 %%", missed, frames)
	}
	if longest > 3 {
		t.Errorf("capture gap of %d ms, over 60 ms", longest*20)
	}
	if lost := silence + st.PlayDropped - before.PlayDropped; float64(lost) > 0.005*float64(frames*FrameSamples) {
		t.Errorf("%d playback samples lost or silent, over 0.5 %%", lost)
	}
	if s.CaptureLost() {
		t.Error("the microphone is reported lost while it works")
	}

	// Unplug the microphone: the stream reports the loss, hands out no audio, keeps playing, and is
	// not moved to the other microphone.
	// The fixture's voice ends first: the audio service would move that stream to the speaker when
	// its sink goes, and it would be heard there.
	voice.Close()
	unplugMic()
	time.Sleep(lostAfter + 300*time.Millisecond)
	if !s.CaptureLost() {
		t.Error("the unplugged microphone is not reported lost")
	}
	// The audio graph may reconfigure when a device goes: give playback a second, then measure it.
	_, missed, _, captured = run(time.Second)
	// What was already queued may still come out; a stream moved to another device would bring a
	// full second (8000 samples).
	if len(captured) > SampleRate/10 {
		t.Errorf("%d samples captured after the microphone was unplugged (moved to another device?)", len(captured))
	}
	for len(heard) > 0 {
		<-heard
	}
	run(time.Second)
	played = played[:0]
	for len(heard) > 0 {
		played = append(played, <-heard...)
	}
	if got := toneBlocks(played, speakerTone); got < 0.95 {
		t.Errorf("playback did not continue after the microphone was unplugged (tone in %.3f of blocks)", got)
	}
	t.Logf("after unplug: lost=%v, %d misses in 1 s, %d samples captured, speaker tone in %.3f of blocks", s.CaptureLost(), missed, len(captured), toneBlocks(played, speakerTone))
}
