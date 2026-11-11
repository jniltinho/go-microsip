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
	// The engine's media tick: every 20 ms take one captured frame and play one frame. stall
	// freezes the loop once, as a blocked engine would.
	run := func(d, stall time.Duration) (*meter, Stats) {
		for len(heard) > 0 {
			<-heard
		}
		before := s.Stats()
		m := newMeter()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		var in, out [FrameSamples]int16
		for end := time.Now().Add(d); time.Now().Before(end); <-tick.C {
			if stall > 0 && time.Since(m.start) > d/2 {
				time.Sleep(stall)
				stall = 0
			}
			m.captureFrame(s.Capture(&in), in[:])
			sine(speakerTone, &toneN, out[:])
			s.Play(&out)
			for len(heard) > 0 {
				m.heard(<-heard)
			}
		}
		return m, before
	}
	run(time.Second, 0) // streams start and settle
	m, before := run(time.Duration(seconds)*time.Second, 0)
	summary, bad := m.verdict(s.Stats(), before, true)
	t.Logf("%d s: %s", seconds, summary)
	for _, b := range bad {
		t.Error(b)
	}
	if s.CaptureLost() || s.PlaybackLost() {
		t.Error("a device is reported lost while it works")
	}
	// Control: the same checks must fail when the engine stalls.
	m, before = run(2*time.Second, 300*time.Millisecond)
	_, bad = m.verdict(s.Stats(), before, true)
	mustCatch(t, bad)
	run(time.Second, 0) // recover

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
	m, _ = run(time.Second, 0)
	// What was already queued may still come out; a stream moved to another device would bring a
	// full second (50 frames).
	if m.captured > 5 {
		t.Errorf("%d frames captured after the microphone was unplugged (moved to another device?)", m.captured)
	}
	m, _ = run(time.Second, 0)
	if m.toneBlocks < 45 || m.playGap > maxGap || s.PlaybackLost() {
		t.Errorf("playback did not continue after the microphone was unplugged: %d of 50 frames, gap %d ms", m.toneBlocks, m.playGap.Milliseconds())
	}
	t.Logf("after unplug: lost=%v, %d frames captured in 1 s, speaker %d of 50 frames, gap %d ms", s.CaptureLost(), m.captured, m.toneBlocks, m.playGap.Milliseconds())
}
