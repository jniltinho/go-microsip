package audio

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// traceLine is one line of the file --ui-trace writes: an event sent to the page, or what the page
// shows.
type traceLine struct {
	MS   int64 `json:"ms"`
	Sent *struct {
		Type         string `json:"type"`
		Registration *struct {
			State  string `json:"state"`
			Reason string `json:"reason"`
		} `json:"registration"`
		Call *tracedCall `json:"call"`
	} `json:"sent"`
	Page *tracedPage `json:"page"`
}

type tracedCall struct {
	Direction string `json:"direction"`
	Remote    string `json:"remote"`
	Phase     string `json:"phase"`
	EndReason string `json:"endReason"`
	Muted     bool   `json:"muted"`
	Secure    bool   `json:"secure"`
	Transfer  *struct {
		State string `json:"state"`
	} `json:"transfer"`
}

type tracedPage struct {
	Text   string `json:"text"`
	Images int    `json:"images"`
	Skin   string `json:"skin"`   // the skin chosen in the settings
	Theme  string `json:"theme"`  // the skin drawn: "system" resolved to light or dark
	Height int    `json:"height"` // of the page's visible part
	Fields []struct {
		Label string `json:"label"`
		X     int    `json:"x"`
		Y     int    `json:"y"`
	} `json:"fields"`
	Buttons []struct {
		Label    string `json:"label"`
		X        int    `json:"x"`
		Y        int    `json:"y"`
		Disabled bool   `json:"disabled"`
	} `json:"buttons"`
}

// window is the softphone's window under test: what the engine last told the page, what the page
// last showed, and the keyboard and mouse of the X display.
type window struct {
	t          *testing.T
	lines      chan traceLine
	id         string // the X window
	registered string
	failure    string // why the registration failed, in the engine's word
	call       tracedCall
	stopped    bool
	page       tracedPage
	pageAt     int64    // when the page last reported, in the trace's milliseconds
	story      []string // what happened, for a failure's message
}

// follow reads the trace as the softphone writes it.
func follow(t *testing.T, path string) chan traceLine {
	lines := make(chan traceLine, 4096)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		var f *os.File
		for f == nil {
			select {
			case <-done:
				return
			case <-time.After(100 * time.Millisecond):
				f, _ = os.Open(path)
			}
		}
		defer f.Close()
		r := bufio.NewReader(f)
		var partial string
		for {
			text, err := r.ReadString('\n')
			partial += text
			if err == io.EOF {
				select {
				case <-done:
					return
				case <-time.After(50 * time.Millisecond):
				}
				continue
			}
			var line traceLine
			if json.Unmarshal([]byte(partial), &line) == nil {
				lines <- line
			}
			partial = ""
		}
	}()
	return lines
}

func (w *window) take(line traceLine) {
	switch {
	case line.Page != nil:
		w.page, w.pageAt = *line.Page, line.MS
		if phase := shownPhase(line.Page.Text); phase != "" {
			w.note(line.MS, "shown "+phase)
		}
	case line.Sent == nil:
	case line.Sent.Registration != nil:
		w.registered, w.failure = line.Sent.Registration.State, line.Sent.Registration.Reason
		w.note(line.MS, "sent registration "+w.registered)
	case line.Sent.Call != nil:
		w.call = *line.Sent.Call
		w.note(line.MS, "sent call "+w.call.Phase+" "+w.call.EndReason)
		if w.call.Transfer != nil {
			w.note(line.MS, "sent transfer "+w.call.Transfer.State)
		}
	case line.Sent.Type == "stopped":
		w.stopped = true
		w.note(line.MS, "sent stopped")
	}
}

func (w *window) note(ms int64, what string) {
	if n := len(w.story); n == 0 || !strings.HasSuffix(w.story[n-1], what) {
		w.story = append(w.story, fmt.Sprintf("%d ms: %s", ms, what))
	}
}

// shownPhase is the call phase the page's text shows, in the engine's words; "" when it shows no
// call.
func shownPhase(text string) string {
	for label, phase := range map[string]string{
		"Calling…": "dialing", "Ringing…": "ringing", "Incoming call": "ringing", "Connected": "established", "On hold": "held",
		"Ending…": "terminating",
	} {
		if strings.Contains(text, label) {
			return phase
		}
	}
	return ""
}

// until follows the trace until cond holds, or fails with what was seen.
func (w *window) until(d time.Duration, what string, cond func() bool) {
	w.t.Helper()
	timeout := time.After(d)
	for !cond() {
		select {
		case line := <-w.lines:
			w.take(line)
		case <-timeout:
			w.t.Fatalf("no %s within %v\nthe page shows:\n%s\nwhat happened:\n  %s", what, d, w.page.Text, strings.Join(w.story, "\n  "))
		}
	}
}

func (w *window) shows(texts ...string) bool {
	for _, text := range texts {
		if !strings.Contains(w.page.Text, text) {
			return false
		}
	}
	return true
}

func (w *window) xdotool(args ...string) {
	w.t.Helper()
	if out, err := exec.Command("xdotool", args...).CombinedOutput(); err != nil {
		w.t.Fatalf("xdotool %v: %v: %s", args, err, out)
	}
}

// keys presses keys in the window, as a person at the keyboard.
func (w *window) keys(keys ...string) {
	w.t.Helper()
	w.xdotool("windowactivate", "--sync", w.id)
	for _, key := range keys {
		w.xdotool("key", "--clearmodifiers", key)
	}
}

// write types text in the window, as a person at the keyboard.
func (w *window) write(text string) {
	w.t.Helper()
	w.xdotool("windowactivate", "--sync", w.id)
	w.xdotool("type", "--delay", "30", text)
}

// fill clicks the field with this label where the page says it is, and types text in it.
func (w *window) fill(label, text string) {
	w.t.Helper()
	found := func() (x, y int, ok bool) {
		for _, f := range w.page.Fields {
			if f.Label == label {
				return f.X, f.Y, true
			}
		}
		return 0, 0, false
	}
	w.until(5*time.Second, "field "+label, func() bool { _, _, ok := found(); return ok })
	x, y, _ := found()
	w.xdotool("mousemove", "--window", w.id, strconv.Itoa(x), strconv.Itoa(y), "click", "1")
	w.write(text)
}

// choose clicks the field with this label (a radio button, say), scrolling the view to it first.
func (w *window) choose(label string) {
	w.t.Helper()
	for range 20 {
		var x, y int
		w.until(5*time.Second, "field "+label, func() bool {
			for _, f := range w.page.Fields {
				if f.Label == label {
					x, y = f.X, f.Y
					return true
				}
			}
			return false
		})
		// The view scrolls between the tabs and the status bar: well inside the page is in sight.
		if y > 90 && y < w.page.Height-90 {
			w.xdotool("mousemove", "--window", w.id, strconv.Itoa(x), strconv.Itoa(y), "click", "1")
			return
		}
		wheel, seen := "5", w.pageAt // down
		if y <= 90 {
			wheel = "4"
		}
		w.xdotool("mousemove", "--window", w.id, strconv.Itoa(w.page.Height/2), strconv.Itoa(w.page.Height/2), "click", wheel)
		w.until(5*time.Second, "the view scrolled", func() bool { return w.pageAt != seen })
	}
	w.t.Fatalf("the field %q could not be scrolled into view", label)
}

// click presses the button with this label where the page says it is. A button that holds more
// than its label (the strip of a call on another tab) is found by how its text ends.
func (w *window) click(label string) {
	w.t.Helper()
	find := func() (x, y int, ok bool) {
		for _, exact := range []bool{true, false} {
			for _, b := range w.page.Buttons {
				if !b.Disabled && (b.Label == label || !exact && strings.HasSuffix(b.Label, label)) {
					return b.X, b.Y, true
				}
			}
		}
		return 0, 0, false
	}
	w.until(5*time.Second, "enabled button "+label, func() bool { _, _, ok := find(); return ok })
	x, y, _ := find()
	w.xdotool("mousemove", "--window", w.id, strconv.Itoa(x), strconv.Itoa(y), "click", "1")
}

// webProcesses are the processes that draw the page.
func webProcesses() (pids []int) {
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if comm, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "comm")); strings.HasPrefix(string(comm), "WebKitWebProces") {
			pids = append(pids, pid)
		}
	}
	return pids
}

// The built window places a lab call (openspec migrate-to-go-wails, task 4.4): the account
// registers on its own, a person's keys dial the echo, and at every step what the page shows is
// what the engine said. The sound goes through real devices and stays within the threshold while
// the page is frozen for five seconds; mute, hold and the end come from the window; the call is in
// the history the window lists; and a termination signal unregisters before the process ends.
//
// It needs a display, an audio service, the lab PBX and the binary with the window:
// build/qa/window-call.sh inside the lab network (`make go-window-call`).
func TestCallThroughTheWindow(t *testing.T) {
	bin, lab, server := os.Getenv("GO_MICROSIP_CALL_BIN"), os.Getenv("GO_MICROSIP_LAB"), os.Getenv("GO_MICROSIP_PBX")
	if bin == "" || lab == "" || server == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("set DISPLAY, GO_MICROSIP_CALL_BIN, GO_MICROSIP_LAB and GO_MICROSIP_PBX (make go-window-call)")
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
			callThroughTheWindow(t, bin, server, account.user, account.settings, account.secure, passwords)
		})
	}
	t.Run("wrong password", func(t *testing.T) { wrongPasswordInTheWindow(t, bin, server) })
}

// softphone starts the binary with a profile of its own in dir and the password in the
// environment; its output goes to a file that a failed test prints.
func softphone(t *testing.T, bin, dir, password string, args ...string) (*exec.Cmd, chan error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	// Only what it needs: the display, the session's bus and audio service, a profile of its own,
	// and the password.
	cmd.Env = []string{
		"DISPLAY=" + os.Getenv("DISPLAY"), "DBUS_SESSION_BUS_ADDRESS=" + os.Getenv("DBUS_SESSION_BUS_ADDRESS"),
		"XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR"), "HOME=" + dir,
		"XDG_DATA_HOME=" + filepath.Join(dir, "data"), "XDG_CONFIG_HOME=" + filepath.Join(dir, "config"),
		"GO_MICROSIP_SIP_PASSWORD=" + password,
	}
	log, err := os.CreateTemp(dir, "output-*.log")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		log.Close()
		if t.Failed() {
			if text, err := os.ReadFile(log.Name()); err == nil {
				t.Logf("output of %v:\n%s", args, text)
			}
		}
	})
	return cmd, exited
}

// A password the PBX refuses: the window says why the account is not registered, in the engine's
// word, and stays usable.
func wrongPasswordInTheWindow(t *testing.T, bin, server string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	softphone(t, bin, dir, "not-the-password", "--config", file, "--ui-trace", trace)
	w := &window{t: t, lines: follow(t, trace)}
	w.until(60*time.Second, "the refusal shown", func() bool {
		return w.registered == "failed" && w.failure == "unauthorized" && w.shows("Failed: wrong credentials")
	})
	if w.shows("Registered") {
		t.Error("the window shows a registration the engine does not have")
	}
}

func callThroughTheWindow(t *testing.T, bin, server, user, settings string, secure bool, passwords map[string]string) {
	password := passwords[user]
	sound := soundCards(t, user)
	dir := t.TempDir()
	file := sound.config(t, dir, user, server, settings)
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
	w := &window{t: t, lines: follow(t, trace)}

	// The account registers on its own, and the window says so.
	w.until(60*time.Second, "registration shown", func() bool { return w.registered == "registered" && w.shows("Registered", user) })
	for deadline := time.Now().Add(10 * time.Second); w.id == ""; time.Sleep(200 * time.Millisecond) {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--pid", strconv.Itoa(cmd.Process.Pid)).Output()
		if ids := strings.Fields(string(out)); len(ids) > 0 {
			w.id = ids[len(ids)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the window was not found on the display")
		}
	}

	// A person dials the echo: Ctrl+L, the number, Enter.
	w.keys("ctrl+l", "6", "0", "0", "Return")
	protection := "Unencrypted (RTP)"
	if secure {
		protection = "Encrypted (SRTP)"
	}
	w.until(30*time.Second, "answered call shown", func() bool {
		return w.call.Phase == "established" && w.shows("Connected", "600", protection)
	})
	if w.call.Secure != secure {
		t.Errorf("the call reports secure=%v, the account needs %v", w.call.Secure, secure)
	}

	// The sound, with the page frozen for five seconds in the middle: the engine owns the call.
	sound.listen(2 * time.Second) // the streams and the echo settle
	seconds := max(testSeconds(), 10)
	frozen := make(chan int, 1)
	go func() {
		time.Sleep(time.Duration(seconds) * time.Second / 4)
		pids := webProcesses()
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGSTOP)
		}
		time.Sleep(5 * time.Second)
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGCONT)
		}
		frozen <- len(pids)
	}()
	silentSince := w.pageAt
	m, _ := sound.listen(time.Duration(seconds) * time.Second)
	summary, bad := m.speakerVerdict(time.Now())
	if n := <-frozen; n == 0 {
		t.Error("no web process was found to freeze: the freeze was not exercised")
	}
	t.Logf("%s, %d s through the window and the echo, the page frozen for 5 s of them: %s", user, seconds, summary)
	for _, b := range bad {
		t.Error(b)
	}
	// The page is back and still shows the call the engine has.
	w.click("Mute")
	w.until(10*time.Second, "mute shown", func() bool { return w.call.Muted && w.shows("Muted", "Connected") && w.pageAt > silentSince })
	sound.listen(time.Second) // what was on its way still comes out
	if m, _ := sound.listen(2 * time.Second); m.good > 10 {
		t.Errorf("muted in the window, the voice is still heard in %d pieces of 2 s", m.good)
	}
	w.click("Unmute")
	w.until(10*time.Second, "unmute shown", func() bool { return !w.call.Muted && !w.shows("Muted") })
	sound.listen(time.Second) // the microphone starts again
	if m, _ := sound.listen(2 * time.Second); m.good < 150 {
		t.Errorf("unmuted in the window, the voice is heard in only %d pieces of 2 s", m.good)
	}

	w.click("Hold")
	w.until(10*time.Second, "hold shown", func() bool { return w.call.Phase == "held" && w.shows("On hold") })
	w.click("Resume")
	w.until(10*time.Second, "resume shown", func() bool { return w.call.Phase == "established" && w.shows("Connected") })

	// The skin changes during the call: the whole window at once, the call and its sound untouched.
	w.click("Menu")
	w.click("Settings")
	w.choose("Windows XP")
	w.until(10*time.Second, "the Windows XP skin drawn", func() bool { return w.page.Skin == "xp" && w.page.Theme == "xp" })
	m, _ = sound.listen(3 * time.Second)
	summary, bad = m.speakerVerdict(time.Now())
	t.Logf("%s, 3 s after the skin changed during the call: %s", user, summary)
	for _, b := range bad {
		t.Error(b)
	}
	w.keys("Escape") // leaves the settings
	w.until(10*time.Second, "the call still shown, in the new skin", func() bool {
		return w.call.Phase == "established" && w.shows("Connected", "600") && w.page.Theme == "xp"
	})

	// Escape hangs up; the window goes back to the dialpad and lists the call in its history.
	w.keys("Escape")
	w.until(10*time.Second, "the end shown", func() bool {
		return w.call.Phase == "ended" && shownPhase(w.page.Text) == ""
	})
	if w.call.EndReason != "normal" {
		t.Errorf("the call ended as %q, want normal", w.call.EndReason)
	}
	w.click("Calls")
	w.until(10*time.Second, "the call in the history", func() bool { return w.shows("600", "Answered") })

	// What this build cannot do is said, not offered.
	w.click("Menu")
	w.click("Settings")
	w.until(10*time.Second, "the unavailable features named", func() bool {
		return w.shows("Not available in this build", "Speaker and microphone volume")
	})
	w.keys("Escape") // leaves the view

	// A contact whose name is markup, typed by a person, for the number that is about to call.
	const hostile = `<img src=x onerror=alert(1)><b>Ana</b>`
	peer := user[:3] + "2"
	w.click("Contacts")
	w.click("Add")
	w.fill("Name", hostile)
	w.fill("Number 1", peer)
	w.click("Save")
	w.until(10*time.Second, "the contact listed with its name as text", func() bool { return w.shows(hostile, peer) })

	// The other extension calls. The window shows who, with the name as the text it is.
	other := func() (*exec.Cmd, chan error) {
		home := t.TempDir()
		config := filepath.Join(home, "config.toml")
		text := fmt.Sprintf("schema = 1\n[account]\nusername = %q\ndomain = %q\n%s", peer, server, settings)
		if err := os.WriteFile(config, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return softphone(t, bin, home, passwords[peer], "--config", config, "run", "--call", user, "--hangup-after", "60s", "--json")
	}
	caller, transferred := other()
	w.until(30*time.Second, "the incoming call shown", func() bool {
		return w.call.Phase == "ringing" && w.call.Direction == "incoming" && w.call.Remote == peer && w.shows("Incoming call", hostile, peer)
	})
	if w.page.Images != 0 {
		t.Errorf("the page holds %d images: the contact's name became markup", w.page.Images)
	}
	w.keys("Return") // Answer has the focus
	// On another tab the call is a strip with the name; "Show call" opens its panel.
	w.until(15*time.Second, "the answered incoming call shown", func() bool {
		return w.call.Phase == "established" && w.shows("Connected", hostile)
	})
	w.click("Show call")
	w.until(10*time.Second, "the panel of the incoming call", func() bool { return w.shows("Connected", "From", hostile, protection) })
	// A blind transfer of the caller to the lab's tone, from the window.
	w.click("Xfer")
	w.fill("Transfer to", "602")
	w.keys("Return")
	w.until(20*time.Second, "the transferred call ended", func() bool {
		return w.call.Phase == "ended" && shownPhase(w.page.Text) == ""
	})
	if w.call.EndReason != "transferred" || w.call.Transfer == nil || w.call.Transfer.State != "succeeded" {
		t.Errorf("the transferred call ended as %q with transfer %+v", w.call.EndReason, w.call.Transfer)
	}

	// The caller hangs up on the tone it was transferred to and unregisters: the PBX keeps one
	// registration per extension.
	_ = caller.Process.Signal(syscall.SIGINT)
	select {
	case <-transferred:
	case <-time.After(15 * time.Second):
		t.Fatal("the caller's softphone did not stop")
	}

	// It calls again, and Escape refuses the call.
	_, refused := other()
	w.until(30*time.Second, "the second incoming call shown", func() bool {
		return w.call.Phase == "ringing" && w.call.Direction == "incoming" && w.shows("Incoming call")
	})
	w.keys("Escape")
	w.until(15*time.Second, "the refused call ended", func() bool { return w.call.Phase == "ended" && shownPhase(w.page.Text) == "" })
	if w.call.EndReason != "rejected" {
		t.Errorf("the refused call ended as %q", w.call.EndReason)
	}
	select {
	case err := <-refused:
		if err == nil {
			t.Error("the caller's softphone reported a call that the window refused as answered")
		}
	case <-time.After(15 * time.Second):
		t.Error("the caller was not told that its call was refused")
	}
	w.click("Calls")
	w.until(10*time.Second, "the three calls in the history", func() bool { return w.shows("Out", "In", "Answered", "Rejected", hostile) })
	if w.page.Images != 0 {
		t.Errorf("the page holds %d images: the contact's name became markup", w.page.Images)
	}

	// What the page showed never ran ahead of the engine: every phase appears on the page only
	// after the engine sent it.
	sent, over := map[string]bool{}, false
	for _, step := range w.story {
		_, what, _ := strings.Cut(step, " ms: ")
		if phase, ok := strings.CutPrefix(what, "sent call "); ok {
			switch phase = strings.Fields(phase)[0]; {
			case phase == "ended":
				over = true
			case over: // the next call starts from nothing
				sent, over = map[string]bool{phase: true}, false
			default:
				sent[phase] = true
			}
		}
		if phase, ok := strings.CutPrefix(what, "shown "); ok && !sent[phase] {
			t.Errorf("the page showed %q before the engine sent it:\n  %s", phase, strings.Join(w.story, "\n  "))
		}
	}

	// A termination signal: the account is unregistered and the engine stopped before the end.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("the window did not exit after the termination signal")
	}
	w.until(5*time.Second, "unregistration and stop in the trace", func() bool { return w.stopped && w.registered == "disabled" })

	// The skin chosen in the window is the one the next start draws.
	again := filepath.Join(dir, "ui-trace-2.jsonl")
	softphone(t, bin, dir, password, "--config", file, "--ui-trace", again)
	next := &window{t: t, lines: follow(t, again)}
	next.until(60*time.Second, "the saved skin at the next start", func() bool {
		return next.registered == "registered" && next.page.Skin == "xp" && next.page.Theme == "xp"
	})
	t.Logf("%s: %s", user, strings.Join(w.story, "; "))
}
