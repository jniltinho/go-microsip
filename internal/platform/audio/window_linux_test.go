package audio

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	ID        uint64 `json:"id"`
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
	Skin   string `json:"skin"`  // the skin chosen in the settings
	Theme  string `json:"theme"` // the skin drawn: "system" resolved to light or dark
	// Fonts are the bundled faces the page has loaded to draw itself ("family weight"), and
	// FontErrors those that failed to load.
	Fonts      []string `json:"fonts"`
	FontErrors []string `json:"fontErrors"`
	Height     int      `json:"height"` // of the page's visible part
	Width      int      `json:"width"`
	// Scale is the device pixels per CSS pixel; Overflows and Clipped say whether the page is wider
	// than its window and how many of its controls run off its side.
	Scale     float64 `json:"scale"`
	Overflows bool    `json:"overflows"`
	Clipped   int     `json:"clipped"`
	Fields    []struct {
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

// watch reads everything the window reports for d and tells whether cond held at any moment of it.
func (w *window) watch(d time.Duration, cond func() bool) bool {
	timeout := time.After(d)
	for {
		if cond() {
			return true
		}
		select {
		case line := <-w.lines:
			w.take(line)
		case <-timeout:
			return cond()
		}
	}
}

// unregistered follows the trace until the engine has reported itself stopped, which comes after
// it tried to unregister. It does not see the PBX: that the unregistration arrived is shown by the
// next registration of the same extension, which the lab PBX refuses while a contact remains. An AppImage's launcher can exit before its softphone has finished,
// so the process's end alone does not say the extension is free.
func (w *window) unregistered(d time.Duration) bool {
	timeout := time.After(d)
	for !w.stopped {
		select {
		case line := <-w.lines:
			w.take(line)
		case <-timeout:
			return false
		}
	}
	return true
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

// shot keeps a picture of the display for the receipt, when the run asks for them.
func (w *window) shot(name string) {
	dir := os.Getenv("GO_MICROSIP_PRINTS")
	if dir == "" {
		return
	}
	time.Sleep(400 * time.Millisecond) // the page has drawn what it reported
	// The display is larger than the window: the empty part around it is cut off.
	if out, err := exec.Command("import", "-window", "root", "-trim", "+repage", filepath.Join(dir, name+".png")).CombinedOutput(); err != nil {
		w.t.Errorf("screenshot %s: %v: %s", name, err, out)
	}
}

// pointer presses a mouse button at a place of the page. The page counts in CSS pixels; the display
// has Scale device pixels for each.
func (w *window) pointer(x, y int, button string) {
	w.t.Helper()
	scale := w.page.Scale
	if scale <= 0 {
		scale = 1
	}
	w.xdotool("mousemove", "--window", w.id, strconv.Itoa(int(float64(x)*scale)), strconv.Itoa(int(float64(y)*scale)), "click", button)
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
	w.pointer(x, y, "1")
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
			w.pointer(x, y, "1")
			return
		}
		wheel, seen := "5", w.pageAt // down
		if y <= 90 {
			wheel = "4"
		}
		w.pointer(w.page.Height/2, w.page.Height/2, wheel)
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
	w.pointer(x, y, "1")
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
	t.Run("every skin", func(t *testing.T) { skinsInTheWindow(t, bin, server, passwords["1002"]) })
	t.Run("links", func(t *testing.T) { linksInTheWindow(t, bin, server, passwords["1002"]) })
	t.Run("quit during a call", func(t *testing.T) { quitDuringACall(t, bin, server, passwords["1002"]) })
	t.Run("the password is remembered", func(t *testing.T) { passwordIsRemembered(t, bin, server, passwords["1002"]) })
	// The same window on a desktop at 100 and at 200 %, in every skin. An X11 display scales by
	// whole numbers (GDK_SCALE); 150 % cannot be had here.
	for _, skin := range []string{"light", "dark", "xp", "macos", "ubuntu", "win98", "matrix"} {
		for _, desktop := range []struct {
			name string
			env  []string
		}{
			{"100", nil},
			{"200", []string{"GDK_SCALE=2"}},
		} {
			t.Run(skin+" at "+desktop.name, func(t *testing.T) {
				windowAtScale(t, bin, server, passwords["1002"], skin, desktop.name, append(desktop.env, "GO_MICROSIP_UI_THEME="+skin))
			})
		}
	}
}

// A password typed in the settings is kept in the session's keyring and nowhere else: no file of
// the profile holds it, and the next start registers by itself.
func passwordIsRemembered(t *testing.T, bin, server, password string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	stop := func(cmd *exec.Cmd, exited chan error, w *window) {
		t.Helper()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			t.Error("the window did not exit after the termination signal")
		}
		if !w.unregistered(15 * time.Second) {
			t.Error("the window did not unregister after the termination signal")
		}
	}
	// No password is configured anywhere: the account is there and not registered.
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphone(t, bin, dir, "", "--config", file, "--ui-trace", trace)
	w := &window{t: t, lines: follow(t, trace)}
	w.until(60*time.Second, "the account without a password, not registered", func() bool {
		return w.shows("1002@"+server, "Disabled") && w.registered != "registered"
	})
	for deadline := time.Now().Add(10 * time.Second); w.id == ""; time.Sleep(200 * time.Millisecond) {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		if ids := strings.Fields(string(out)); len(ids) > 0 {
			w.id = ids[len(ids)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the window was not found on the display")
		}
	}
	w.click("Menu")
	w.click("Settings")
	w.until(10*time.Second, "the settings", func() bool { return w.shows("Account", "Password") })
	w.fill("Password", password)
	w.click("Save and register")
	w.until(30*time.Second, "registration with the typed password", func() bool { return w.registered == "registered" })
	stop(cmd, exited, w)

	// The password is in no file the session left: not the database, not its journal, not the
	// configuration, not the window's trace or output.
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if content, err := os.ReadFile(path); err == nil && bytes.Contains(content, []byte(password)) {
			t.Errorf("%s holds the password", strings.TrimPrefix(path, dir))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// The next start registers by itself.
	again := filepath.Join(dir, "ui-trace-2.jsonl")
	next, gone := softphone(t, bin, dir, "", "--config", file, "--ui-trace", again)
	after := &window{t: t, lines: follow(t, again)}
	after.until(30*time.Second, "registration by itself at the next start", func() bool { return after.registered == "registered" })
	stop(next, gone, after)
}

// Quitting during a call (desktop-app: "Quit during a call"): the close button asks, the yes ends
// the call, the account unregisters and the process is gone within the engine's bound. The PBX
// then takes the same extension again, which it would refuse had the contact stayed.
func quitDuringACall(t *testing.T, bin, server, password string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	_, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
	w := &window{t: t, lines: follow(t, trace)}
	w.until(60*time.Second, "registration shown", func() bool { return w.registered == "registered" && w.shows("Registered") })
	for deadline := time.Now().Add(10 * time.Second); w.id == ""; time.Sleep(200 * time.Millisecond) {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		if ids := strings.Fields(string(out)); len(ids) > 0 {
			w.id = ids[len(ids)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the window was not found on the display")
		}
	}
	w.keys("ctrl+l", "6", "0", "0", "Return")
	w.until(30*time.Second, "the call connected", func() bool { return w.call.Phase == "established" && w.shows("Connected", "600") })
	w.keys("alt+F4")
	w.until(10*time.Second, "the question before quitting", func() bool { return w.shows("Hang up and quit?") && w.call.Phase == "established" })
	asked := time.Now()
	w.click("Hang up and quit")
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the window was still there 10 s after the yes: the engine's bound is 5 s")
	}
	t.Logf("gone %v after the yes", time.Since(asked).Round(10*time.Millisecond))
	if !w.unregistered(5 * time.Second) {
		t.Error("the engine did not report itself stopped")
	}
	if w.call.Phase != "ended" || w.registered != "disabled" {
		t.Errorf("at the end the call was %q and the registration %q, want ended and disabled", w.call.Phase, w.registered)
	}
	// The PBX's word: one contact per extension, and it takes this one again.
	again := filepath.Join(dir, "ui-trace-2.jsonl")
	probe, left := softphone(t, bin, dir, password, "--config", file, "--ui-trace", again)
	after := &window{t: t, lines: follow(t, again)}
	after.until(30*time.Second, "the extension registered again after the quit", func() bool { return after.registered == "registered" })
	_ = syscall.Kill(-probe.Process.Pid, syscall.SIGTERM)
	select {
	case <-left:
	case <-time.After(15 * time.Second):
		t.Error("the second window did not exit after the termination signal")
	}
	if !after.unregistered(15 * time.Second) {
		t.Error("the second window did not unregister after the termination signal")
	}
}

// sip: and tel: links: the window started with one asks before it calls; a second start with one
// wakes the running window, which asks, and no second window stays; a link that is not one starts
// nothing and reaches no window.
func linksInTheWindow(t *testing.T, bin, server, password string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace, "tel:6-0-0")
	w := &window{t: t, lines: follow(t, trace)}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			t.Error("the window did not exit after the termination signal")
		}
		if !w.unregistered(15 * time.Second) {
			t.Error("the window did not unregister after the termination signal")
		}
	}()
	const question = "A link asks to call 600. Call it?"
	windows := func() int {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		return len(strings.Fields(string(out)))
	}
	// Started with a link: it registers and asks; nothing is dialed until the answer.
	w.until(60*time.Second, "the link's question at the start", func() bool {
		return w.registered == "registered" && w.shows(question)
	})
	if w.call.Phase != "" {
		t.Fatalf("a call (%s) before the question was answered", w.call.Phase)
	}
	for deadline := time.Now().Add(10 * time.Second); w.id == ""; time.Sleep(200 * time.Millisecond) {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		if ids := strings.Fields(string(out)); len(ids) > 0 {
			w.id = ids[len(ids)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the window was not found on the display")
		}
	}
	w.shot("link-1-question")
	w.click("Call")
	w.until(30*time.Second, "the link's call connected after the yes", func() bool {
		return w.call.Phase == "established" && w.shows("Connected", "600")
	})
	w.keys("Escape")
	w.until(10*time.Second, "the link's call ended", func() bool { return w.call.Phase == "ended" && shownPhase(w.page.Text) == "" })

	// A second start with a link: it ends by itself, the running window asks, no second window.
	second := func(link string) (code int) {
		t.Helper()
		again, gone := softphone(t, bin, dir, password, "--config", file, link)
		select {
		case err := <-gone:
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return exit.ExitCode()
			}
			return 0
		case <-time.After(20 * time.Second):
			_ = syscall.Kill(-again.Process.Pid, syscall.SIGKILL)
			t.Fatalf("a second start with %q stayed open", link)
			return -1
		}
	}
	ended := w.call.ID
	if code := second("sip:600@other.invalid:5070"); code != 0 {
		t.Errorf("the second start with a link ended with %d, want 0", code)
	}
	w.until(15*time.Second, "the second link's question in the running window", func() bool { return w.shows(question) })
	if n := windows(); n != 1 {
		t.Errorf("%d windows on the display after the second start, want 1", n)
	}
	w.click("Cancel")
	w.until(10*time.Second, "the question gone after Cancel", func() bool { return !w.shows(question) })
	// Everything the window reports for the next seconds is read: a call placed late would show.
	if w.watch(3*time.Second, func() bool { return w.call.ID != ended }) {
		t.Errorf("a call was placed after Cancel: %+v", w.call)
	}

	// What is not a link is refused before anything starts, as a usage error.
	if code := second("tel:600;phone-context=x"); code != 2 {
		t.Errorf("a link with parameters ended with %d, want the usage error 2", code)
	}
	if w.watch(3*time.Second, func() bool { return w.shows("A link asks") || w.call.ID != ended }) {
		t.Errorf("a refused link reached the window: question shown %v, call %+v", w.shows("A link asks"), w.call)
	}
}

// Every skin in one window, as a person goes through them: chosen in the settings, then a call to
// the echo dialed and hung up in it. The page draws the skin with its bundled font, and the call
// is the engine's in each. (The build image has no Ubuntu font, so that skin loads Inter.)
func skinsInTheWindow(t *testing.T, bin, server, password string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
	w := &window{t: t, lines: follow(t, trace)}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			t.Error("the window did not exit after the termination signal")
		}
		if !w.unregistered(15 * time.Second) {
			t.Error("the window did not unregister after the termination signal")
		}
	}()
	w.until(60*time.Second, "registration shown", func() bool { return w.registered == "registered" && w.shows("Registered") })
	for deadline := time.Now().Add(10 * time.Second); w.id == ""; time.Sleep(200 * time.Millisecond) {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		if ids := strings.Fields(string(out)); len(ids) > 0 {
			w.id = ids[len(ids)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the window was not found on the display")
		}
	}
	inter := []string{"Inter 100 900"}
	for _, skin := range []struct {
		id, label string
		faces     []string // the regular and the heavier face of its bundled font, or the one variable face
	}{
		{"light", "Light", inter},
		{"dark", "Dark", inter},
		{"xp", "Windows XP", []string{"DejaVu Sans 400", "DejaVu Sans 700"}},
		{"macos", "macOS", inter},
		{"ubuntu", "Ubuntu", inter},
		{"win98", "Windows 98", []string{"Liberation Sans 400", "Liberation Sans 700"}},
		{"matrix", "Matrix", []string{"JetBrains Mono 400", "JetBrains Mono 600"}},
	} {
		w.click("Menu")
		w.click("Settings")
		w.choose(skin.label)
		w.until(10*time.Second, "the "+skin.label+" skin drawn in "+strings.Join(skin.faces, " and "), func() bool {
			for _, face := range skin.faces {
				if !slices.Contains(w.page.Fonts, face) {
					return false
				}
			}
			return w.page.Skin == skin.id && w.page.Theme == skin.id
		})
		if len(w.page.FontErrors) != 0 {
			t.Errorf("in %s these fonts failed to load: %v", skin.label, w.page.FontErrors)
		}
		w.keys("Escape") // leaves the settings
		w.keys("ctrl+l", "6", "0", "0", "Return")
		w.until(30*time.Second, "the call connected in "+skin.label, func() bool {
			return w.call.Phase == "established" && w.shows("Connected", "600") && w.page.Theme == skin.id
		})
		if w.page.Overflows || w.page.Clipped != 0 {
			t.Errorf("the call in %s: the page is wider than its window (%v) and %d controls run off its side", skin.label, w.page.Overflows, w.page.Clipped)
		}
		w.shot("skin-" + skin.id + "-call")
		w.keys("Escape") // hangs up
		w.until(10*time.Second, "the call ended in "+skin.label, func() bool {
			return w.call.Phase == "ended" && shownPhase(w.page.Text) == ""
		})
		if w.call.EndReason != "normal" {
			t.Errorf("in %s the call ended as %q, want normal", skin.label, w.call.EndReason)
		}
		w.click("Clear number") // the number stays in the field for a redial: the next call starts clean
	}
}

// At every desktop scale the window shows its screens whole: nothing wider than the window, no
// control off its side, and the fictional account and the tabs readable.
func windowAtScale(t *testing.T, bin, server, password, skin, name string, env []string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphoneWith(t, env, bin, dir, password, "--config", file, "--ui-trace", trace)
	// The account is unregistered at the end: the PBX keeps one registration per extension, and the
	// next scale uses the same one.
	w := &window{t: t, lines: follow(t, trace)}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			t.Error("the window did not exit after the termination signal")
		}
		if !w.unregistered(15 * time.Second) {
			t.Error("the window did not unregister after the termination signal")
		}
	}()
	whole := func(screen string) {
		t.Helper()
		time.Sleep(600 * time.Millisecond) // the screen has settled
		w.until(5*time.Second, "a report of the "+screen, func() bool { return w.page.Width > 0 })
		if w.page.Overflows || w.page.Clipped != 0 {
			t.Errorf("%s in %s at %s %%: the page is wider than its window (%v) and %d controls run off its side; page %d px wide at scale %.2f",
				screen, skin, name, w.page.Overflows, w.page.Clipped, w.page.Width, w.page.Scale)
		}
		if w.page.Theme != skin {
			t.Errorf("%s at %s %%: the page draws the skin %q, want %q", screen, name, w.page.Theme, skin)
		}
		if name != "100" {
			w.shot("scale-" + name + "-" + skin + "-" + screen)
		}
	}
	w.until(60*time.Second, "the dialpad with the registered account", func() bool {
		return w.registered == "registered" && w.shows("Home", "Calls", "Contacts", "Keypad", "Registered", "1002@"+server)
	})
	for deadline := time.Now().Add(10 * time.Second); w.id == ""; time.Sleep(200 * time.Millisecond) {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		if ids := strings.Fields(string(out)); len(ids) > 0 {
			w.id = ids[len(ids)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the window was not found on the display")
		}
	}
	if want := map[string]float64{"100": 1, "200": 2}[name]; w.page.Scale != want {
		t.Errorf("at %s %%: the page has %.2f device pixels per CSS pixel, want %.0f: the desktop's scale did not reach it", name, w.page.Scale, want)
	}
	t.Logf("%s at %s %%: the page is %d by %d CSS pixels at %.2f device pixels each", skin, name, w.page.Width, w.page.Height, w.page.Scale)
	whole("home")
	w.click("Keypad")
	w.until(10*time.Second, "the keypad", func() bool { return w.shows("ABC", "WXYZ") })
	whole("keypad")
	w.click("Contacts")
	w.until(10*time.Second, "the contacts tab", func() bool { return w.shows("Add") })
	whole("contacts")
	w.click("Menu")
	w.click("Settings")
	w.until(10*time.Second, "the settings", func() bool { return w.shows("Account", "Appearance") })
	whole("settings")
}

// softphone starts the binary with a profile of its own in dir and the password in the
// environment; its output goes to a file that a failed test prints.
func softphone(t *testing.T, bin, dir, password string, args ...string) (*exec.Cmd, chan error) {
	t.Helper()
	return softphoneWith(t, nil, bin, dir, password, args...)
}

// softphoneWith is softphone with more environment, such as the desktop's scale.
func softphoneWith(t *testing.T, env []string, bin, dir, password string, args ...string) (*exec.Cmd, chan error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	// A group of its own: an AppImage is a launcher with the softphone as its child, and a signal
	// has to reach both.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Only what it needs: the display, the session's bus and audio service, a profile of its own,
	// and the password.
	cmd.Env = []string{
		"DISPLAY=" + os.Getenv("DISPLAY"), "DBUS_SESSION_BUS_ADDRESS=" + os.Getenv("DBUS_SESSION_BUS_ADDRESS"),
		"XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR"), "HOME=" + dir,
		"XDG_DATA_HOME=" + filepath.Join(dir, "data"), "XDG_CONFIG_HOME=" + filepath.Join(dir, "config"),
		"GO_MICROSIP_SIP_PASSWORD=" + password,
		"APPIMAGE_EXTRACT_AND_RUN=1", // an AppImage runs without FUSE here
	}
	cmd.Env = append(cmd.Env, env...)
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
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
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
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		if ids := strings.Fields(string(out)); len(ids) > 0 {
			w.id = ids[len(ids)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the window was not found on the display")
		}
	}

	w.shot(user + "-1-registered")

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

	w.shot(user + "-2-call-connected")

	// The sound, with the page frozen for five seconds in the middle: the engine owns the call.
	sound.listen(2 * time.Second) // the streams and the echo settle
	seconds := max(testSeconds(), 10)
	frozen := make(chan int, 1)
	go func() {
		time.Sleep(time.Duration(seconds) * time.Second / 4)
		pids := webProcesses()
		stopped := 0
		for _, pid := range pids {
			if syscall.Kill(pid, syscall.SIGSTOP) != nil {
				continue
			}
			// The kernel says the process is stopped ("T"), a moment after the signal.
			for range 20 {
				time.Sleep(10 * time.Millisecond)
				stat, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
				if i := strings.LastIndexByte(string(stat), ')'); i >= 0 && strings.HasPrefix(string(stat[i+1:]), " T") {
					stopped++
					break
				}
			}
		}
		time.Sleep(5 * time.Second)
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGCONT)
		}
		if stopped != len(pids) {
			stopped = 0 // a web process that kept running: the page was not frozen
		}
		frozen <- stopped
	}()
	silentSince := w.pageAt
	m, _ := sound.listen(time.Duration(seconds) * time.Second)
	summary, bad := m.speakerVerdict(time.Now())
	if n := <-frozen; n == 0 {
		t.Error("the page's processes were not all stopped for the five seconds: the freeze was not exercised")
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
	w.shot(user + "-3-call-on-hold")
	w.click("Resume")
	w.until(10*time.Second, "resume shown", func() bool { return w.call.Phase == "established" && w.shows("Connected") })

	// The skin changes during the call: the whole window at once, the call and its sound untouched.
	w.click("Menu")
	w.click("Settings")
	w.choose("Windows XP")
	w.until(10*time.Second, "the Windows XP skin drawn", func() bool { return w.page.Skin == "xp" && w.page.Theme == "xp" })
	w.shot(user + "-4-skin-changed-during-the-call")
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

	// The close button during a call, on a desktop without a tray: the window asks first, and
	// keeping the call keeps it.
	w.keys("alt+F4")
	w.until(10*time.Second, "the question before quitting", func() bool {
		return w.shows("Hang up and quit?") && w.call.Phase == "established"
	})
	w.shot(user + "-5-close-button-during-the-call")
	w.click("Cancel")
	w.until(10*time.Second, "the call kept", func() bool {
		return !w.shows("Hang up and quit?") && w.shows("Connected") && w.call.Phase == "established"
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
	w.shot(user + "-5-hung-up-history")

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
	w.until(10*time.Second, "the contact form", func() bool { return w.shows("New contact") })
	w.write(hostile) // the name field has the focus: nothing is clicked first
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
	w.shot(user + "-6-incoming-call")
	w.keys("Return") // Answer has the focus
	// The window goes to the call by itself when it is answered.
	w.until(15*time.Second, "the answered incoming call shown", func() bool {
		return w.call.Phase == "established" && w.shows("Connected", "From", hostile, protection)
	})
	w.shot(user + "-7-incoming-call-answered")
	// A blind transfer of the caller to the lab's tone, from the window.
	w.click("Transfer")
	w.until(10*time.Second, "the transfer field", func() bool {
		for _, f := range w.page.Fields {
			if f.Label == "Transfer to" {
				return true
			}
		}
		return false
	})
	w.write("602") // the target field has the focus
	w.keys("Return")
	w.until(20*time.Second, "the transferred call ended", func() bool {
		return w.call.Phase == "ended" && shownPhase(w.page.Text) == ""
	})
	if w.call.EndReason != "transferred" || w.call.Transfer == nil || w.call.Transfer.State != "succeeded" {
		t.Errorf("the transferred call ended as %q with transfer %+v", w.call.EndReason, w.call.Transfer)
	}

	// The caller hangs up on the tone it was transferred to and unregisters: the PBX keeps one
	// registration per extension.
	_ = syscall.Kill(-caller.Process.Pid, syscall.SIGINT)
	select {
	case <-transferred:
	case <-time.After(15 * time.Second):
		t.Fatal("the caller's softphone did not stop")
	}

	// It calls again while the window is out of sight (minimized here; hidden in the tray on a
	// desktop that has one): the window comes back by itself, and Escape refuses the call.
	inSight := func() bool {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		return slices.Contains(strings.Fields(string(out)), w.id)
	}
	w.xdotool("windowminimize", "--sync", w.id)
	if inSight() {
		t.Error("the window is still in sight after it was minimized: the next check would prove nothing")
	}
	_, refused := other()
	w.until(30*time.Second, "the second incoming call shown", func() bool {
		return w.call.Phase == "ringing" && w.call.Direction == "incoming" && w.shows("Incoming call")
	})
	w.until(5*time.Second, "the window back in sight for the incoming call", inSight)
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
	w.until(10*time.Second, "the three calls in the history", func() bool {
		rows := func(what string) int { return strings.Count(w.page.Text, what) }
		return rows("Outgoing · Answered") == 1 && rows("Incoming · Answered") == 1 && rows("Incoming · Rejected") == 1 && w.shows(hostile)
	})
	w.shot(user + "-8-history")
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
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
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
	restarted, gone := softphone(t, bin, dir, password, "--config", file, "--ui-trace", again)
	next := &window{t: t, lines: follow(t, again)}
	next.until(60*time.Second, "the saved skin at the next start", func() bool {
		return next.registered == "registered" && next.page.Skin == "xp" && next.page.Theme == "xp"
	})
	// The close button without a call and without a tray quits: the account unregisters (the
	// extension is free again) and the process ends.
	for deadline := time.Now().Add(10 * time.Second); next.id == ""; time.Sleep(200 * time.Millisecond) {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		if ids := strings.Fields(string(out)); len(ids) > 0 {
			next.id = ids[len(ids)-1]
		} else if time.Now().After(deadline) {
			t.Fatal("the restarted window was not found on the display")
		}
	}
	next.keys("alt+F4")
	select {
	case <-gone:
	case <-time.After(15 * time.Second):
		_ = syscall.Kill(-restarted.Process.Pid, syscall.SIGTERM)
		t.Error("the restarted window did not exit after its close button")
	}
	if !next.unregistered(15 * time.Second) {
		t.Error("the restarted window did not unregister after its close button")
	}
	// The PBX's own word for it: it keeps one contact per extension and refuses a second one
	// (max_contacts=1, remove_existing=no in the lab), so the same extension registers again only
	// if the close button's unregistration reached it.
	third := filepath.Join(dir, "ui-trace-3.jsonl")
	probe, left := softphone(t, bin, dir, password, "--config", file, "--ui-trace", third)
	after := &window{t: t, lines: follow(t, third)}
	after.until(30*time.Second, "the extension registered again after the close button", func() bool {
		return after.registered == "registered"
	})
	_ = syscall.Kill(-probe.Process.Pid, syscall.SIGTERM)
	select {
	case <-left:
	case <-time.After(15 * time.Second):
		t.Error("the third window did not exit after the termination signal")
	}
	if !after.unregistered(15 * time.Second) {
		t.Error("the third window did not unregister after the termination signal")
	}
	t.Logf("%s: %s", user, strings.Join(w.story, "; "))
}
