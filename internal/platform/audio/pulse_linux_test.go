package audio

import (
	"errors"
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// nullSink adds a virtual sound card (a sink and its monitor source) through the protocol itself,
// so the test needs no tool on the machine.
func nullSink(t *testing.T, c *pulse.Client, name string) (unload func()) {
	t.Helper()
	var loaded proto.LoadModuleReply
	err := c.RawRequest(&proto.LoadModule{Name: "module-null-sink", Args: "sink_name=" + name + " rate=48000 channels=2"}, &loaded)
	if err != nil {
		t.Fatalf("load a virtual sound card: %v", err)
	}
	done := false
	unload = func() {
		if !done {
			done = true
			if err := c.RawRequest(&proto.UnloadModule{ModuleIndex: loaded.ModuleIndex}, nil); err != nil {
				t.Errorf("unload the virtual sound card: %v", err)
			}
		}
	}
	t.Cleanup(unload)
	return unload
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
	// Test fixtures on their own connection: virtual sound cards, a "voice" into the virtual
	// microphone and a listener on the virtual speaker.
	fx, err := pulse.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fx.Close) // runs after the sound cards are unloaded
	var server proto.GetServerInfoReply
	if err := fx.RawRequest(&proto.GetServerInfo{}, &server); err == nil {
		t.Logf("audio service: %s %s", server.PackageName, server.PackageVersion)
	}
	nullSink(t, fx, "g0spk")
	unplugMic := nullSink(t, fx, "g0mic")
	nullSink(t, fx, "g0other")         // another microphone the stream must never move to
	time.Sleep(300 * time.Millisecond) // the service publishes the new devices

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

	seconds := testSeconds()
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
