package audio

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jfreymuth/pulse"
)

// callEvent is one line of `go-microsip run --json`.
type callEvent struct {
	Event  string `json:"event"`
	State  string `json:"state"`
	Secure *bool  `json:"secure"`
}

// The softphone's own binary places a lab call with real sound devices (openspec
// migrate-to-go-wails, task 3.8). A voice goes into a virtual microphone chosen by ID, the lab's
// echo sends it back, and a listener measures it on a virtual speaker chosen by ID: the frame-loss
// threshold over the whole path, then the microphone is unplugged with another one in reach.
//
// It needs an audio service, the lab PBX and the binary: build/qa/audio-call.sh inside the lab
// network (docker/asterisk/go-call.sh, `make go-call`).
func TestCallThroughTheBinary(t *testing.T) {
	bin, lab, server := os.Getenv("GO_MICROSIP_CALL_BIN"), os.Getenv("GO_MICROSIP_LAB"), os.Getenv("GO_MICROSIP_PBX")
	if bin == "" || lab == "" || server == "" {
		t.Skip("set GO_MICROSIP_CALL_BIN, GO_MICROSIP_LAB and GO_MICROSIP_PBX (make go-call)")
	}
	raw, err := os.ReadFile(filepath.Join(lab, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	var passwords map[string]string
	if err := json.Unmarshal(raw, &passwords); err != nil {
		t.Fatal(err)
	}
	for _, account := range []struct {
		user, settings string
		secure         bool
	}{
		{"1001", "port = 5060\ntransport = \"udp\"\n", false},
		{"1201", fmt.Sprintf("port = 5061\ntransport = \"tls\"\nmedia = \"srtp\"\nca_file = %q\n", filepath.Join(lab, "ca.crt")), true},
	} {
		t.Run(account.user, func(t *testing.T) {
			callThroughTheBinary(t, bin, server, account.user, passwords[account.user], account.settings, account.secure)
		})
	}
}

func callThroughTheBinary(t *testing.T, bin, server, user, password, settings string, secure bool) {
	fx, err := pulse.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fx.Close) // runs after the sound cards are unloaded
	spk, mic, other := "gmspk"+user, "gmmic"+user, "gmother"+user
	nullSink(t, fx, spk)
	unplugMic := nullSink(t, fx, mic)
	nullSink(t, fx, other) // another microphone the call must never move to
	// The service publishes the new devices in its own time.
	sinkByID := func(id string) *pulse.Sink {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(200 * time.Millisecond) {
			sink, err := fx.SinkByID(id)
			if err == nil {
				return sink
			}
			if time.Now().After(deadline) {
				t.Fatalf("the virtual sound card %s did not appear: %v", id, err)
			}
		}
	}

	play := func(sink string, tone float64) *pulse.PlaybackStream {
		t.Helper()
		target := sinkByID(sink)
		n := 0
		s, err := fx.NewPlayback(pulse.Int16Reader(func(p []int16) (int, error) { sine(tone, &n, p); return len(p), nil }),
			pulse.PlaybackSampleRate(SampleRate), pulse.PlaybackSink(target), pulse.PlaybackLatency(0.04))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		s.Start()
		return s
	}
	// The meter looks for speakerTone, so that is what the voice says; the decoy says micTone.
	voice := play(mic, speakerTone)
	play(other, micTone)
	spkSink := sinkByID(spk)
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
	listener.Start()

	// The softphone: a configuration file with the devices by ID, the password in the environment.
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = %q\ndomain = %q\n%s[audio]\ninput_device = %q\noutput_device = %q\n",
		user, server, settings, mic+".monitor", spk)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--config", file, "run", "--call", "600", "--json", "--verbose")
	// Only what the softphone needs: where the session's audio service is, where to keep its
	// history, and the password.
	cmd.Env = []string{
		"XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR"), "HOME=" + os.Getenv("HOME"),
		"XDG_DATA_HOME=" + filepath.Join(dir, "data"), "GO_MICROSIP_SIP_PASSWORD=" + password,
	}
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	events := make(chan callEvent, 64)
	go func() {
		lines := bufio.NewScanner(stdout)
		for lines.Scan() {
			var ev callEvent
			if json.Unmarshal(lines.Bytes(), &ev) == nil {
				events <- ev
			}
		}
		close(events)
		exited <- cmd.Wait()
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	var seen []string
	// expect reads events until one matches; ok is false when d passes or the softphone exits first.
	expect := func(d time.Duration, match func(callEvent) bool) (callEvent, bool) {
		timeout := time.After(d)
		for {
			select {
			case ev, open := <-events:
				if !open {
					return callEvent{}, false
				}
				seen = append(seen, ev.Event+":"+ev.State)
				if match(ev) {
					return ev, true
				}
			case <-timeout:
				return callEvent{}, false
			}
		}
	}
	answered, ok := expect(20*time.Second, func(ev callEvent) bool { return ev.Event == "call" && ev.State == "established" })
	if !ok {
		t.Fatalf("the call was not answered; events: %v", seen)
	}
	if answered.Secure == nil || *answered.Secure != secure {
		t.Errorf("the call reports secure=%v, the account needs %v", answered.Secure, secure)
	}

	// listen judges what the speaker plays for d, in the 20 ms rhythm of a media tick, and keeps the
	// samples.
	listen := func(d time.Duration) (*meter, []int16) {
		for len(heard) > 0 {
			<-heard
		}
		m := newMeter(time.Now())
		var all, got []int16
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for end := time.Now().Add(d); time.Now().Before(end); <-tick.C {
			got = got[:0]
			for len(heard) > 0 {
				got = append(got, <-heard...)
			}
			m.heard(got, time.Now())
			all = append(all, got...)
		}
		return m, all
	}
	listen(2 * time.Second) // the streams and the echo settle
	seconds := testSeconds()
	m, _ := listen(time.Duration(seconds) * time.Second)
	summary, bad := m.speakerVerdict(time.Now())
	t.Logf("%s, %d s through the binary and the echo: %s", user, seconds, summary)
	for _, b := range bad {
		t.Error(b)
	}

	// Unplug the microphone. The fixture's voice ends first: the audio service would move that
	// stream to another sink when its own goes.
	voice.Close()
	unplugMic()
	if _, ok := expect(3*time.Second, func(ev callEvent) bool { return ev.Event == "notice" && ev.State == "microphone_lost" }); !ok {
		t.Errorf("the loss of the microphone was not reported; events: %v", seen)
	}
	listen(time.Second) // what was on its way still comes out
	m, samples := listen(2 * time.Second)
	if m.good > 10 {
		t.Errorf("the voice is still heard %d times after the microphone was unplugged", m.good)
	}
	if decoy := toneBlocks(samples, micTone); decoy > 0.05 {
		t.Errorf("the other microphone is heard in %.0f %% of the blocks: the call moved to it without the user", decoy*100)
	}
	t.Logf("%s after the unplug: voice in %d pieces of 2 s, other microphone in %.0f %% of the blocks", user, m.good, toneBlocks(samples, micTone)*100)
	// The call is still up: nothing ended it, and the softphone runs until it is told to stop.
	if ev, ended := expect(time.Second, func(ev callEvent) bool { return ev.Event == "call" || ev.Event == "stopped" }); ended {
		t.Errorf("after the unplug the softphone reported %s %s", ev.Event, ev.State)
	}

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if _, ok := expect(10*time.Second, func(ev callEvent) bool { return ev.Event == "stopped" }); !ok {
		t.Errorf("no stopped event after the interrupt; events: %v", seen)
	}
	select {
	case err := <-exited:
		if code := cmd.ProcessState.ExitCode(); code != 130 {
			t.Errorf("exit status %d (%v), want 130 after an interrupt", code, err)
		}
	case <-time.After(10 * time.Second):
		t.Error("the softphone did not exit after the interrupt")
	}
	if all := strings.Join(seen, " "); !strings.Contains(all, "call:ended") {
		t.Errorf("the call's end was not reported: %v", seen)
	}
}
