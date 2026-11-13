package audio

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	gosip "github.com/emiago/sipgo/sip"
	"github.com/godbus/dbus/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jniltinho/go-microsip/internal/control"
	"github.com/jniltinho/go-microsip/internal/core/app"
	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/engine"
	"github.com/jniltinho/go-microsip/internal/core/ports"
	"github.com/jniltinho/go-microsip/internal/core/store"
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

// notifications stands in for the desktop's notification service on the lab's session bus: the
// softphone asks it to show a call that rings, and the test presses its buttons.
type notifications struct {
	conn  *dbus.Conn
	mu    sync.Mutex
	next  uint32
	shown []notice
}

type notice struct {
	id            uint32
	summary, body string
	actions       []string
}

const (
	notifyName = "org.freedesktop.Notifications"
	notifyPath = "/org/freedesktop/Notifications"
)

func serveNotifications(t *testing.T) *notifications {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	n := &notifications{conn: conn}
	if err := conn.Export(n, notifyPath, notifyName); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(notifyName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("the test could not be the notification service of the lab's session: %v %v", reply, err)
	}
	return n
}

// GetCapabilities, Notify and CloseNotification are the service's methods.
func (n *notifications) GetCapabilities() ([]string, *dbus.Error) {
	return []string{"actions", "body"}, nil
}

func (n *notifications) Notify(app string, replaces uint32, icon, summary, body string, actions []string, hints map[string]dbus.Variant, expire int32) (uint32, *dbus.Error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	id := replaces
	if id == 0 {
		n.next++
		id = n.next
	}
	n.shown = append(n.shown, notice{id: id, summary: summary, body: body, actions: actions})
	return id, nil
}

func (n *notifications) CloseNotification(uint32) *dbus.Error { return nil }

// last is the id of the newest notification asked for.
func (n *notifications) last() uint32 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.next
}

func (n *notifications) wait(t *testing.T, what string, is func(notice) bool) notice {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		n.mu.Lock()
		for _, shown := range n.shown {
			if is(shown) {
				n.mu.Unlock()
				return shown
			}
		}
		n.mu.Unlock()
	}
	t.Fatalf("no %s", what)
	return notice{}
}

func (n *notifications) press(id uint32, key string) {
	_ = n.conn.Emit(notifyPath, notifyName+".ActionInvoked", id, key)
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

// choose clicks the field with this label (a radio button, a check box), scrolling the view to
// it first.
func (w *window) choose(label string) {
	w.t.Helper()
	w.inSight("field "+label, func() (int, int, bool) {
		for _, f := range w.page.Fields {
			if f.Label == label {
				return f.X, f.Y, true
			}
		}
		return 0, 0, false
	})
}

// press clicks the enabled button with this label, scrolling the view to it first.
func (w *window) press(label string) {
	w.t.Helper()
	w.inSight("button "+label, func() (int, int, bool) {
		for _, b := range w.page.Buttons {
			if !b.Disabled && b.Label == label {
				return b.X, b.Y, true
			}
		}
		return 0, 0, false
	})
}

// inSight scrolls the view until what find names is in it, and clicks it there.
func (w *window) inSight(what string, find func() (x, y int, ok bool)) {
	w.t.Helper()
	for range 30 {
		var x, y int
		w.until(5*time.Second, what, func() bool {
			var ok bool
			x, y, ok = find()
			return ok
		})
		// The view scrolls between the tabs and the status bar: well inside the page is in sight.
		if y > 90 && y < w.page.Height-90 {
			// Where the page still says it is a moment later: the view may be moving, and the
			// page reports only what changed.
			w.watch(600*time.Millisecond, func() bool { return false })
			if x2, y2, ok := find(); !ok || x2 != x || y2 != y {
				continue
			}
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
	w.t.Fatalf("the %s could not be scrolled into view", what)
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
	desk := serveNotifications(t)
	for _, account := range []struct {
		user, settings string
		secure         bool
	}{
		{"1001", "port = 5060\ntransport = \"udp\"\n", false},
		{"1201", fmt.Sprintf("port = 5061\ntransport = \"tls\"\nmedia = \"srtp\"\nca_file = %q\n", filepath.Join(lab, "ca.crt")), true},
	} {
		t.Run(account.user, func(t *testing.T) {
			callThroughTheWindow(t, bin, server, account.user, account.settings, account.secure, passwords, desk)
		})
	}
	t.Run("wrong password", func(t *testing.T) { wrongPasswordInTheWindow(t, bin, server) })
	t.Run("an untrusted certificate", func(t *testing.T) { untrustedCertificateInTheWindow(t, bin, server, passwords["1202"]) })
	t.Run("the System skin without a color-preference service", func(t *testing.T) { systemSkinWithoutAService(t, bin) })
	t.Run("every skin", func(t *testing.T) { skinsInTheWindow(t, bin, server, passwords["1002"]) })
	t.Run("links", func(t *testing.T) { linksInTheWindow(t, bin, server, passwords["1002"]) })
	t.Run("quit during a call", func(t *testing.T) { quitDuringACall(t, bin, server, passwords["1002"], false) })
	t.Run("a termination signal during a call", func(t *testing.T) { quitDuringACall(t, bin, server, passwords["1002"], true) })
	t.Run("the password is remembered", func(t *testing.T) { passwordIsRemembered(t, bin, server, passwords["1002"]) })
	t.Run("quit with a PBX that stopped answering", func(t *testing.T) { quitWithASilentPBX(t, bin) })
	t.Run("the tray", func(t *testing.T) { theTray(t, bin, server, passwords["1002"]) })
	t.Run("agent control", func(t *testing.T) { agentControl(t, bin, server, passwords["1002"]) })
	t.Run("the command line and MCP", func(t *testing.T) { commandLineAndMCP(t, bin, server, passwords["1002"]) })
	t.Run("the Agent access form", func(t *testing.T) { agentAccessForm(t, bin, server, passwords["1002"]) })
	t.Run("the headless service", func(t *testing.T) { headlessService(t, bin, server, passwords["1002"]) })
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

// silentPBX is a PBX of the test's own on the loopback (TCP): it registers anyone and answers every
// call, until it is told to fall silent; from then on it reads requests and answers none, like a
// PBX that hangs or a network that drops what comes back.
type silentPBX struct {
	port            int
	silent          atomic.Bool
	byes            atomic.Int32
	unregistrations atomic.Int32
}

func startSilentPBX(t *testing.T) *silentPBX {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	p := &silentPBX{port: probe.Addr().(*net.TCPAddr).Port}
	probe.Close()
	sink, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}) // where the softphone's audio goes
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for buf := make([]byte, 2048); ; {
			if _, err := sink.Read(buf); err != nil {
				return
			}
		}
	}()
	ua, err := sipgo.NewUA()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	srv.OnRegister(func(req *gosip.Request, tx gosip.ServerTransaction) {
		if h := req.GetHeader("Expires"); h != nil && h.Value() == "0" {
			p.unregistrations.Add(1)
		}
		if p.silent.Load() {
			return
		}
		res := gosip.NewResponseFromRequest(req, 200, "OK", nil)
		res.AppendHeader(gosip.NewHeader("Expires", "60"))
		_ = tx.Respond(res)
	})
	srv.OnInvite(func(req *gosip.Request, tx gosip.ServerTransaction) {
		if p.silent.Load() {
			return
		}
		sdp := fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\n"+
			"m=audio %d RTP/AVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=sendrecv\r\n",
			sink.LocalAddr().(*net.UDPAddr).Port)
		res := gosip.NewResponseFromRequest(req, 200, "OK", []byte(sdp))
		res.AppendHeader(gosip.NewHeader("Content-Type", "application/sdp"))
		res.AppendHeader(gosip.NewHeader("Contact", fmt.Sprintf("<sip:pbx@127.0.0.1:%d;transport=tcp>", p.port)))
		_ = tx.Respond(res)
	})
	srv.OnAck(func(*gosip.Request, gosip.ServerTransaction) {})
	srv.OnBye(func(req *gosip.Request, tx gosip.ServerTransaction) {
		p.byes.Add(1)
		if p.silent.Load() {
			return
		}
		_ = tx.Respond(gosip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = ua.Close(); _ = sink.Close() })
	ready := make(chan struct{})
	go func() {
		_ = srv.ListenAndServe(context.WithValue(ctx, sipgo.ListenReadyCtxKey, sipgo.ListenReadyCtxValue(ready)), "tcp", addr)
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the test's PBX did not start")
	}
	return p
}

// Quitting during a call when the PBX answers nothing any more (desktop-app: "Quit during a call
// ... even if the PBX does not answer"): the whole process, window included, is gone within the
// engine's bound of five seconds, having sent the hang-up and the unregistration.
func quitWithASilentPBX(t *testing.T, bin string) {
	pbx := startSilentPBX(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = \"127.0.0.1\"\nport = %d\ntransport = \"tcp\"\n", pbx.port)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	_, exited := softphone(t, bin, dir, "fictional-password", "--config", file, "--ui-trace", trace)
	w := &window{t: t, lines: follow(t, trace)}
	w.until(60*time.Second, "registration at the test's PBX", func() bool { return w.registered == "registered" })
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
	pbx.silent.Store(true)
	w.keys("alt+F4")
	w.until(10*time.Second, "the question before quitting", func() bool { return w.shows("Hang up and quit?") })
	asked := time.Now()
	w.click("Hang up and quit")
	select {
	case <-exited:
	case <-time.After(8 * time.Second):
		t.Fatal("the window was still there 8 s after the yes, with a PBX that answers nothing: the bound is 5 s")
	}
	took := time.Since(asked)
	t.Logf("gone %v after the yes, with a PBX that answers nothing", took.Round(10*time.Millisecond))
	if took > 6*time.Second {
		t.Errorf("the process took %v to end, over the engine's bound of 5 s", took.Round(10*time.Millisecond))
	}
	if !w.unregistered(5 * time.Second) {
		t.Error("the engine did not report itself stopped")
	}
	if w.call.Phase != "ended" || w.registered != "disabled" {
		t.Errorf("at the end the call was %q and the registration %q, want ended and disabled", w.call.Phase, w.registered)
	}
	if pbx.byes.Load() == 0 || pbx.unregistrations.Load() == 0 {
		t.Errorf("the silent PBX received %d hang-ups and %d unregistrations: both are sent although nothing answers", pbx.byes.Load(), pbx.unregistrations.Load())
	}
}

// trayHost stands in for a desktop that shows tray icons: it is the StatusNotifierWatcher of the
// lab's session for as long as the test that made it runs, and it talks to the softphone's tray
// item as a panel would.
type trayHost struct {
	conn *dbus.Conn
	mu   sync.Mutex
	item string // the bus name of the softphone's tray item, once it registered
}

const (
	watcherName = "org.kde.StatusNotifierWatcher"
	trayItem    = "/StatusNotifierItem"
	trayMenu    = "/StatusNotifierMenu"
)

// RegisterStatusNotifierItem is the watcher's method an application calls with its item.
func (h *trayHost) RegisterStatusNotifierItem(service string, sender dbus.Sender) *dbus.Error {
	h.mu.Lock()
	h.item = string(sender)
	h.mu.Unlock()
	return nil
}

func serveTrayHost(t *testing.T) *trayHost {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() }) // the session has no tray host again
	h := &trayHost{conn: conn}
	if err := conn.Export(h, "/StatusNotifierWatcher", watcherName); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("the test could not be the tray host of the lab's session: %v %v", reply, err)
	}
	return h
}

// registered waits for the softphone's tray item and returns its bus name.
func (h *trayHost) registered(t *testing.T) string {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		h.mu.Lock()
		item := h.item
		h.mu.Unlock()
		if item != "" {
			return item
		}
	}
	t.Fatal("the softphone did not register a tray item with the tray host")
	return ""
}

// menu reads the tray menu as a panel does: the labels in order ("-" for a separator), and the id
// of each.
func (h *trayHost) menu(t *testing.T, item string) (labels []string, ids map[string]int32) {
	t.Helper()
	var revision uint32
	var layout struct {
		ID       int32
		Props    map[string]dbus.Variant
		Children []dbus.Variant
	}
	err := h.conn.Object(item, trayMenu).Call("com.canonical.dbusmenu.GetLayout", 0, int32(0), int32(-1), []string{}).Store(&revision, &layout)
	if err != nil {
		t.Fatalf("reading the tray menu: %v", err)
	}
	ids = map[string]int32{}
	for _, child := range layout.Children {
		fields, ok := child.Value().([]interface{})
		if !ok || len(fields) < 2 {
			t.Fatalf("a tray menu entry of an unexpected shape: %v", child)
		}
		id, _ := fields[0].(int32)
		props, _ := fields[1].(map[string]dbus.Variant)
		label := "-"
		if kind, _ := props["type"].Value().(string); kind != "separator" {
			label, _ = props["label"].Value().(string)
		}
		labels = append(labels, label)
		ids[label] = id
	}
	return labels, ids
}

// choose clicks a menu entry, as a panel does.
func (h *trayHost) choose(t *testing.T, item string, id int32) {
	t.Helper()
	if err := h.conn.Object(item, trayMenu).Call("com.canonical.dbusmenu.Event", 0, id, "clicked", dbus.MakeVariant(""), uint32(0)).Err; err != nil {
		t.Fatalf("choosing a tray menu entry: %v", err)
	}
}

// activate is a click on the tray icon.
func (h *trayHost) activate(t *testing.T, item string) {
	t.Helper()
	if err := h.conn.Object(item, trayItem).Call("org.kde.StatusNotifierItem.Activate", 0, int32(0), int32(0)).Err; err != nil {
		t.Fatalf("clicking the tray icon: %v", err)
	}
}

// The tray, on a desktop that shows tray icons (the test is its tray host): the softphone
// registers its icon; the close button hides the window and nothing quits; a click on the icon
// brings it back; the menu lists the window, the latest calls and Quit; choosing a call from it
// calls the number again; Quit quits, unregistering.
func theTray(t *testing.T, bin, server, password string) {
	host := serveTrayHost(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
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
	inSight := func() bool {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", "^go-microsip$").Output()
		return slices.Contains(strings.Fields(string(out)), w.id)
	}
	gone := func() bool {
		select {
		case <-exited:
			return true
		default:
			return false
		}
	}
	item := host.registered(t)

	// The menu before any call: the window and Quit.
	if labels, _ := host.menu(t, item); !slices.Equal(labels, []string{"Show go-microsip", "-", "Quit"}) {
		t.Errorf("the tray menu before any call: %q", labels)
	}
	// A call is placed and hung up: the menu lists it.
	w.keys("ctrl+l", "6", "0", "0", "Return")
	w.until(30*time.Second, "the call connected", func() bool { return w.call.Phase == "established" })
	first := w.call.ID
	w.keys("Escape")
	w.until(10*time.Second, "the call ended", func() bool { return w.call.Phase == "ended" && shownPhase(w.page.Text) == "" })
	const recent = "\u2197 600"
	var ids map[string]int32
	w.until(10*time.Second, "the call in the tray menu", func() bool {
		var labels []string
		labels, ids = host.menu(t, item)
		return slices.Equal(labels, []string{"Show go-microsip", recent, "-", "Quit"})
	})

	// The close button hides the window: nothing quits, the account stays registered.
	w.keys("alt+F4")
	for deadline := time.Now().Add(5 * time.Second); inSight(); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the window is still in sight after its close button, on a desktop with a tray")
		}
	}
	if w.watch(2*time.Second, func() bool { return gone() || w.registered != "registered" || w.shows("Hang up and quit?") }) {
		t.Fatalf("after the close button on a desktop with a tray: process gone %v, registration %q", gone(), w.registered)
	}
	// A click on the icon brings it back.
	host.activate(t, item)
	for deadline := time.Now().Add(5 * time.Second); !inSight(); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the window did not come back after a click on its tray icon")
		}
	}
	// Hidden again, a call chosen from the menu shows the window and calls that number.
	w.xdotool("windowactivate", "--sync", w.id)
	w.keys("alt+F4")
	for deadline := time.Now().Add(5 * time.Second); inSight(); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the window is still in sight after its close button")
		}
	}
	host.choose(t, item, ids[recent])
	w.until(30*time.Second, "the call chosen from the tray connected", func() bool {
		return w.call.ID != first && w.call.Phase == "established" && w.call.Remote == "600"
	})
	if !inSight() {
		t.Error("the window is not in sight for the call chosen from the tray")
	}
	w.keys("Escape")
	w.until(10*time.Second, "the second call ended", func() bool { return w.call.Phase == "ended" && shownPhase(w.page.Text) == "" })

	// Quit from the menu: no call, so nothing is asked; the account unregisters and the process ends.
	_, ids = host.menu(t, item)
	host.choose(t, item, ids["Quit"])
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		t.Fatal("the softphone did not quit from its tray menu")
	}
	if !w.unregistered(15 * time.Second) {
		t.Error("the softphone did not unregister after Quit from its tray menu")
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
//
// With signal, the process is told to end (SIGTERM) instead: nobody is asked, and the same holds.
// The call's last event then arrives while the window is already leaving, which must not hold it.
func quitDuringACall(t *testing.T, bin, server, password string, signal bool) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	quitting, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
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
	asked := time.Now()
	if signal {
		if err := syscall.Kill(-quitting.Process.Pid, syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	} else {
		w.keys("alt+F4")
		w.until(10*time.Second, "the question before quitting", func() bool { return w.shows("Hang up and quit?") && w.call.Phase == "established" })
		asked = time.Now()
		w.click("Hang up and quit")
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the window was still there 10 s after it was told to end: the engine's bound is 5 s")
	}
	t.Logf("gone %v after it was told to end", time.Since(asked).Round(10*time.Millisecond))
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

// The System skin on a desktop that has no service to ask for its light or dark preference, as
// this display's session: the built window draws the light skin and stays on it.
func systemSkinWithoutAService(t *testing.T, bin string) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var portal bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.freedesktop.portal.Desktop").Store(&portal); err != nil {
		t.Fatal(err)
	}
	if portal {
		t.Skip("this session has a color-preference service")
	}
	dir := t.TempDir()
	trace := filepath.Join(dir, "ui-trace.jsonl")
	softphoneWith(t, []string{"GO_MICROSIP_UI_THEME=system"}, bin, dir, "", "--ui-trace", trace)
	w := &window{t: t, lines: follow(t, trace)}
	w.until(60*time.Second, "the System skin drawn", func() bool { return w.page.Skin == "system" && w.page.Theme != "" })
	if w.page.Theme != "light" {
		t.Fatalf("without a color-preference service System is drawn as %q, want light", w.page.Theme)
	}
	if w.watch(3*time.Second, func() bool { return w.page.Theme != "light" }) {
		t.Errorf("System changed to %q with nobody to ask", w.page.Theme)
	}
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

// A TLS account whose registrar cannot be vouched for (A129): with a CA bundle that did not
// sign the PBX's certificate, and with no bundle at all, the window says that the certificate
// was rejected, shows no registration and does not go on trying.
func untrustedCertificateInTheWindow(t *testing.T, bin, server, password string) {
	// A certificate authority of the test's own, which signed nothing.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authority := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "another authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, authority, authority, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	for name, bundle := range map[string][]byte{"another authority's bundle": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "no bundle": nil} {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.toml")
		text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1202\"\ndomain = %q\nport = 5061\ntransport = \"tls\"\nmedia = \"srtp\"\n", server)
		if bundle != nil {
			ca := filepath.Join(dir, "other-ca.crt")
			if err := os.WriteFile(ca, bundle, 0o600); err != nil {
				t.Fatal(err)
			}
			text += fmt.Sprintf("ca_file = %q\n", ca)
		}
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		trace := filepath.Join(dir, "ui-trace.jsonl")
		cmd, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
		w := &window{t: t, lines: follow(t, trace)}
		w.until(60*time.Second, name+": the certificate's refusal shown", func() bool {
			return w.registered == "failed" && w.failure == "certificate" && w.shows("Failed: certificate rejected")
		})
		// Not tried again: the first retry of a failure that is retried comes after five seconds.
		if w.watch(7*time.Second, func() bool { return w.registered != "failed" }) {
			t.Errorf("%s: after the refusal the registration is %q: it was tried again", name, w.registered)
		}
		if w.shows("Registered") {
			t.Errorf("%s: the window shows a registration the engine does not have", name)
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			t.Fatalf("%s: the softphone did not end", name)
		}
		w.unregistered(10 * time.Second)
	}
}

func callThroughTheWindow(t *testing.T, bin, server, user, settings string, secure bool, passwords map[string]string, desk *notifications) {
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

	// The settings' sound test, before any call: its tone on the chosen speaker, the level of the
	// chosen microphone (the fixture's voice is in it) on the screen, and silence once the
	// settings are left with the test still running.
	w.click("Menu")
	w.click("Settings")
	w.press("Test audio")
	level := 0
	w.until(10*time.Second, "the running sound test with the microphone's level", func() bool {
		if m := micLevel.FindStringSubmatch(w.page.Text); m != nil {
			level, _ = strconv.Atoi(m[1])
		}
		// The settings also say when a device chosen there is used.
		return level > 0 && w.shows("Stop test", "Used by the next call")
	})
	w.shot(user + "-1a-audio-test")
	_, tested := sound.listen(2 * time.Second)
	if tone, sounding := blocksWith(tested, app.AudioTestHz); tone < 0.9 {
		t.Errorf("the chosen speaker plays the test's tone in %.0f %% of two seconds of the test and anything at all in %.0f %%", tone*100, sounding*100)
	} else {
		t.Logf("the sound test: its tone in %.0f %% of two seconds on the chosen speaker, the microphone's level shown as %d %%", tone*100, level)
	}
	w.click("Close settings")
	w.until(5*time.Second, "the settings left", func() bool { return !w.shows("Stop test") && !w.shows("Appearance") })
	sound.listen(500 * time.Millisecond) // what was on its way to the speaker
	if _, quiet := sound.listen(time.Second); len(quiet) == 0 {
		t.Error("nothing was recorded from the speaker after the settings were left")
	} else if _, sounding := blocksWith(quiet, app.AudioTestHz); sounding > 0.02 {
		t.Errorf("the speaker still plays in %.0f %% of a second after the settings were left", sounding*100)
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

	// Contacts come in from a CSV file, picked in the desktop's own file dialog: the window says
	// what the file holds and what it refuses, and stores nothing before the yes.
	csv := filepath.Join(dir, "contacts.csv")
	if err := os.WriteFile(csv, []byte("name,number,label\nLab Ana,7001,desk\nLab Ana,7001,desk\nLab Bia,7002,\nBad,sip:1@x,\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pick := func() {
		t.Helper()
		w.click("Import CSV")
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(200 * time.Millisecond) {
			out, _ := exec.Command("xdotool", "getactivewindow").Output()
			if id := strings.TrimSpace(string(out)); id != "" && id != w.id {
				break // the file dialog is a window of its own and has the keyboard
			}
			if time.Now().After(deadline) {
				t.Fatal("no file dialog opened for the CSV import")
			}
		}
		time.Sleep(500 * time.Millisecond) // the dialog draws its list
		// A path typed from its first slash goes to the dialog's location field, which the
		// slash opens when it is not there yet (the dialog remembers it between uses).
		w.xdotool("type", "--delay", "30", csv)
		// The dialog completes what is typed with names it knows: what it added after the
		// cursor goes away again.
		w.xdotool("key", "--clearmodifiers", "shift+End")
		w.xdotool("key", "--clearmodifiers", "Delete")
		w.xdotool("key", "--clearmodifiers", "Return")
		w.until(15*time.Second, "the question about the CSV file", func() bool {
			return w.shows("Import 2 contacts from 4 rows?", "1 row repeated, skipped.", "1 row refused, listed below.", "Line 5: invalid number")
		})
	}
	pick()
	w.shot(user + "-5a-csv-import-asked")
	if !w.shows("No contacts yet.") {
		t.Error("contacts are listed while the import is only asked about")
	}
	w.click("Cancel")
	w.until(5*time.Second, "the import given up", func() bool { return !w.shows("Import 2 contacts") })
	if w.watch(time.Second, func() bool { return !w.shows("No contacts yet.") }) {
		t.Error("the import that was given up stored contacts")
	}
	pick()
	w.click("Import")
	w.until(10*time.Second, "the imported contacts listed", func() bool {
		return w.shows("Lab Ana", "Lab Bia", "7001", "Imported: 2 new, 0 updated, 2 numbers added") && !w.shows("Bad")
	})
	w.shot(user + "-5b-csv-imported")

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
		// The caller's own ringback goes to another sound card than the one that is listened to.
		text := fmt.Sprintf("schema = 1\n[account]\nusername = %q\ndomain = %q\n%s[audio]\noutput_device = %q\n", peer, server, settings, sound.other)
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
	// The softphone rings on the speaker chosen for calls while the call waits: one second of
	// its two notes in every three.
	_, rang := sound.listen(3 * time.Second)
	if notes, sounding := ringBlocks(rang); notes < 0.28 || notes > 0.40 || sounding-notes > 0.03 {
		t.Errorf("the speaker plays the ring's notes in %.0f %% of three seconds of a ringing call and anything at all in %.0f %%, want its notes alone in about a third",
			notes*100, sounding*100)
	} else {
		t.Logf("the ring: its notes in %.0f %% of three seconds on the chosen speaker, anything at all in %.0f %%", notes*100, sounding*100)
	}
	w.keys("Return") // Answer has the focus
	// The window goes to the call by itself when it is answered.
	w.until(15*time.Second, "the answered incoming call shown", func() bool {
		return w.call.Phase == "established" && w.shows("Connected", "From", hostile, protection)
	})
	w.shot(user + "-7-incoming-call-answered")
	// The ring stopped with the answer: none of its notes in the call's sound.
	_, talk := sound.listen(time.Second)
	if notes, _ := ringBlocks(talk); notes > 0.02 {
		t.Errorf("the ring's notes are in %.0f %% of the answered call's sound", notes*100)
	}
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
	before := desk.last()
	w.xdotool("windowminimize", "--sync", w.id)
	if inSight() {
		t.Error("the window is still in sight after it was minimized: the next check would prove nothing")
	}
	_, refused := other()
	w.until(30*time.Second, "the second incoming call shown", func() bool {
		return w.call.Phase == "ringing" && w.call.Direction == "incoming" && w.shows("Incoming call")
	})
	w.until(5*time.Second, "the window back in sight for the incoming call", inSight)
	// The desktop was asked to notify the call, with the caller as the network named it and two
	// buttons; Reject, pressed there, refuses the call.
	second := desk.wait(t, "the notification of the second incoming call", func(n notice) bool { return n.id > before })
	if second.summary != "Incoming call" || second.body != peer || !slices.Equal(second.actions, []string{"answer", "Answer", "reject", "Reject"}) {
		t.Errorf("the notification of the incoming call: %+v, want the caller %s and the two buttons", second, peer)
	}
	desk.press(second.id, "reject")
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
	// It calls a third time and gives up while the call rings: the call's notification gives way
	// to a missed call's, with the caller and no button, and the history says Missed.
	before = desk.last()
	gaveUp, left := other()
	w.until(30*time.Second, "the third incoming call shown", func() bool {
		return w.call.Phase == "ringing" && w.call.Direction == "incoming" && w.shows("Incoming call")
	})
	unanswered := desk.wait(t, "the notification of the third incoming call", func(n notice) bool { return n.id > before })
	_ = syscall.Kill(-gaveUp.Process.Pid, syscall.SIGINT)
	select {
	case <-left:
	case <-time.After(15 * time.Second):
		t.Fatal("the caller's softphone did not stop")
	}
	w.until(15*time.Second, "the call the caller gave up ended", func() bool { return w.call.Phase == "ended" && shownPhase(w.page.Text) == "" })
	if w.call.EndReason != "canceled" {
		t.Errorf("the call the caller gave up ended as %q", w.call.EndReason)
	}
	missed := desk.wait(t, "the notification of the missed call", func(n notice) bool { return n.summary == "Missed call" && n.id > before })
	if missed.id != unanswered.id || missed.body != peer || len(missed.actions) != 0 {
		t.Errorf("the notification of the missed call: %+v, want that of the ringing call (%d) replaced, the caller %s and no button", missed, unanswered.id, peer)
	}
	w.click("Calls")
	w.until(10*time.Second, "the four calls in the history", func() bool {
		rows := func(what string) int { return strings.Count(w.page.Text, what) }
		return rows("Outgoing · Answered") == 1 && rows("Incoming · Answered") == 1 && rows("Incoming · Rejected") == 1 &&
			rows("Missed · ") == 1 && w.shows(hostile)
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

// agentControl is the window as a program of the same user reaches it (task 6.4): the control
// socket of the session answers status without a name or a number, refuses personal data without
// a grant, and a call asked for waits for the person at the window. Nothing is sent before the
// yes, a no sends nothing, and each decision is in the audit trail of the database.
func agentControl(t *testing.T, bin, server, password string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
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
	runtime, err := control.RuntimeDir(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	var client *control.Client
	for deadline := time.Now().Add(10 * time.Second); client == nil; time.Sleep(200 * time.Millisecond) {
		if client, err = control.Dial(runtime); err != nil && time.Now().After(deadline) {
			t.Fatalf("the window does not serve agent control: %v", err)
		}
	}
	defer client.Close()
	if welcome := client.Welcome(); welcome.Mode != control.ModeWindow || welcome.Epoch == 0 {
		t.Errorf("the owner's welcome: %+v", welcome)
	}
	n := 0
	ask := func(key string, op control.Operation) control.Response {
		n++
		r := control.Request{SchemaVersion: control.SchemaVersion, RequestID: fmt.Sprintf("lab-%d", n), Operation: op}
		if key != "" {
			r.IdempotencyKey = &key
		}
		return client.Do(r)
	}
	type status struct {
		Registration string `json:"registration"`
		Call         *struct {
			CallID  uint64 `json:"call_id"`
			Version uint64 `json:"version"`
			Phase   string `json:"phase"`
		} `json:"call"`
	}
	read := func() (status, string) {
		t.Helper()
		r := ask("", control.Operation{Op: control.StatusGet})
		var s status
		if r.Status != control.StatusOK || json.Unmarshal(r.Data, &s) != nil {
			t.Fatalf("status_get: %+v (%+v)", r, r.Error)
		}
		return s, string(r.Data)
	}
	state := func(operation string) string {
		t.Helper()
		r := ask("", control.Operation{Op: control.OperationGet, OperationID: operation})
		var data struct {
			State string `json:"state"`
		}
		if r.Status != control.StatusOK || json.Unmarshal(r.Data, &data) != nil {
			t.Fatalf("operation_get: %+v (%+v)", r, r.Error)
		}
		return data.State
	}
	// Status says the account is registered, and names nobody.
	if s, wire := read(); s.Registration != "registered" || s.Call != nil || strings.Contains(wire, "1002") || strings.Contains(wire, server) {
		t.Errorf("the status of an idle window: %s", wire)
	}
	// Personal data needs a grant.
	for _, op := range []string{control.HistoryList, control.ContactsSearch, control.ConfigShow, control.DevicesList} {
		if r := ask("", control.Operation{Op: op}); r.Error == nil || r.Error.Code != control.GrantRequired {
			t.Errorf("%s without a grant: %+v", op, r)
		}
	}
	// A call asked for waits for the person: the window says who asks and for what.
	dial := control.Operation{Op: control.CallDial, Target: "600"}
	asked := ask("lab-dial", dial)
	if asked.Status != control.StatusApprovalRequired || asked.OperationID == nil {
		t.Fatalf("a call asked for without a grant: %+v (%+v)", asked, asked.Error)
	}
	w.until(10*time.Second, "the agent's request shown", func() bool { return w.shows("asks to:", "call 600", "Allow", "Deny") })
	if !w.shows(fmt.Sprintf("process %d", os.Getpid())) {
		t.Errorf("the request does not name the process that asked (%d):\n%s", os.Getpid(), w.page.Text)
	}
	if w.watch(2*time.Second, func() bool { return w.call.ID != 0 }) {
		t.Fatalf("a call (%+v) before the person allowed it", w.call)
	}
	if again := ask("lab-dial", dial); again.Status != control.StatusApprovalRequired || again.OperationID == nil || *again.OperationID != *asked.OperationID {
		t.Errorf("the same request again: %+v", again)
	}
	if got := state(*asked.OperationID); got != "awaiting_approval" {
		t.Errorf("the operation before the decision is %q", got)
	}
	// Allow is the second button; Deny has the focus, and Allow works after a second on screen.
	w.keys("Tab", "Return")
	w.until(30*time.Second, "the call the agent asked for connected", func() bool { return w.call.Phase == "established" && w.call.Remote == "600" })
	if got := state(*asked.OperationID); got != "succeeded" {
		t.Errorf("the operation after the yes is %q", got)
	}
	if again := ask("lab-dial", dial); again.Status != control.StatusOK {
		t.Errorf("the same request after it ran: %+v", again)
	}
	first := w.call.ID
	// The agent sees the call, without the number, and asks to end it: a no keeps the call.
	hangup := func(key string) control.Response {
		t.Helper()
		for range 5 { // the snapshot may move between the status and the request
			s, wire := read()
			if s.Call == nil || s.Call.Phase != "established" || strings.Contains(wire, "600") {
				t.Fatalf("the status during the call: %s", wire)
			}
			r := ask(key, control.Operation{Op: control.CallHangup, CallID: s.Call.CallID, ExpectedVersion: s.Call.Version})
			if r.Error == nil || r.Error.Code != control.NotFound {
				return r
			}
		}
		t.Fatal("the call's version never held still")
		return control.Response{}
	}
	if s, _ := read(); s.Call != nil {
		if r := ask("lab-old", control.Operation{Op: control.CallHangup, CallID: s.Call.CallID, ExpectedVersion: s.Call.Version + 100}); r.Error == nil || r.Error.Code != control.NotFound {
			t.Errorf("a hang-up at a version the call does not have: %+v", r)
		}
	}
	refused := hangup("lab-hangup-1")
	if refused.Status != control.StatusApprovalRequired {
		t.Fatalf("a hang-up asked for: %+v (%+v)", refused, refused.Error)
	}
	w.until(10*time.Second, "the hang-up request shown", func() bool { return w.shows("asks to:", "hang up the call") })
	w.keys("Return") // Deny has the focus
	w.until(10*time.Second, "the request gone", func() bool { return !w.shows("asks to:") })
	if w.watch(2*time.Second, func() bool { return w.call.Phase != "established" }) || state(*refused.OperationID) != "denied" {
		t.Fatalf("after the no the call is %q and the operation %q", w.call.Phase, state(*refused.OperationID))
	}
	allowed := hangup("lab-hangup-2")
	if allowed.Status != control.StatusApprovalRequired {
		t.Fatalf("a second hang-up asked for: %+v (%+v)", allowed, allowed.Error)
	}
	w.until(10*time.Second, "the second hang-up request shown", func() bool { return w.shows("asks to:", "hang up the call") })
	time.Sleep(1500 * time.Millisecond) // Allow takes no key during its first second on screen
	w.keys("Tab", "Return")
	w.until(15*time.Second, "the call ended by the agent's request", func() bool { return w.call.ID == first && w.call.Phase == "ended" })
	if got := state(*allowed.OperationID); got != "succeeded" {
		t.Errorf("the hang-up after the yes is %q", got)
	}
	// The client leaves; the window goes on. Then the window ends, and its database tells what
	// was asked and decided, without the number.
	client.Close()
	if w.watch(time.Second, func() bool { return w.registered != "registered" }) {
		t.Errorf("the registration is %q after the client left", w.registered)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("the softphone did not end on a termination signal")
	}
	// An AppImage's launcher ends before the softphone it started has finished leaving.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		late, err := control.Dial(runtime)
		if errors.Is(err, control.ErrUnavailable) {
			break
		}
		if err == nil {
			late.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("a client after the window ended: %v", err)
		}
	}
	// And the softphone has left the PBX, where the next one registers the same extension.
	if !w.unregistered(15 * time.Second) {
		t.Error("the softphone did not report that it had stopped")
	}
	db, err := store.Open(app.StorePath(map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.AuditRows(50)
	if err != nil {
		t.Fatal(err)
	}
	var trail []string
	for i := len(rows) - 1; i >= 0; i-- {
		trail = append(trail, rows[i].Operation+" "+rows[i].Decision+" "+rows[i].Detail)
	}
	want := []string{"call_dial requested destination ***00", "call_dial approved destination ***00", "call_dial succeeded -",
		"call_hangup requested -", "call_hangup denied -", "call_hangup requested -", "call_hangup approved -", "call_hangup succeeded -"}
	if !slices.Equal(trail, want) {
		t.Errorf("the audit trail:\n  %s\nwant:\n  %s", strings.Join(trail, "\n  "), strings.Join(want, "\n  "))
	}
	t.Logf("the audit trail: %s", strings.Join(trail, "; "))
}

// profile is the environment of one softphone profile of the lab: the session's runtime
// directory and bus, and data and configuration homes of its own.
func profile(dir, password string) []string {
	return []string{
		"DISPLAY=" + os.Getenv("DISPLAY"), "DBUS_SESSION_BUS_ADDRESS=" + os.Getenv("DBUS_SESSION_BUS_ADDRESS"),
		"XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR"), "HOME=" + dir,
		"XDG_DATA_HOME=" + filepath.Join(dir, "data"), "XDG_CONFIG_HOME=" + filepath.Join(dir, "config"),
		"GO_MICROSIP_SIP_PASSWORD=" + password, "APPIMAGE_EXTRACT_AND_RUN=1",
	}
}

// commandLine runs the built softphone's command line and returns its exit status and what it
// printed.
func commandLine(t *testing.T, bin, dir string, args ...string) (int, string) {
	t.Helper()
	return commandLineWith(t, "", bin, dir, args...)
}

// commandLineWith is commandLine for a command that registers: it has the account's password.
func commandLineWith(t *testing.T, password, bin, dir string, args ...string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = profile(dir, password)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exit):
		return exit.ExitCode(), string(out)
	}
	t.Fatalf("go-microsip %v: %v\n%s", args, err, out)
	return -1, ""
}

// answered reads the owner's answer out of a --json command line.
func answered(t *testing.T, out string) control.Response {
	t.Helper()
	var r control.Response
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("not an answer of the owner: %s", out)
	}
	return r
}

// labStatus is what status_get tells.
type labStatus struct {
	Mode         string `json:"mode"`
	Registration string `json:"registration"`
	DND          bool   `json:"dnd"`
	Call         *struct {
		CallID  uint64 `json:"call_id"`
		Version uint64 `json:"version"`
		Phase   string `json:"phase"`
	} `json:"call"`
}

// commandLineAndMCP is task 6.5 on the built artifact: the softphone's own command line and its
// MCP server, as two programs of the session, against the window. A call asked for on the
// command line and a hang-up asked for by an MCP tool wait for Allow; a tool that needs a grant
// is refused without one; and the database tells what was asked and decided.
func commandLineAndMCP(t *testing.T, bin, server, password string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
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
	// The command line: status, a read that needs a grant, a call that waits.
	code, out := commandLine(t, bin, dir, "status", "--json")
	var status labStatus
	if r := answered(t, out); code != 0 || json.Unmarshal(r.Data, &status) != nil || status.Mode != "gui" || status.Registration != "registered" ||
		strings.Contains(out, "1002") || strings.Contains(out, server) {
		t.Fatalf("status --json: %d\n%s", code, out)
	}
	if code, out := commandLine(t, bin, dir, "history", "list"); code != 3 || !strings.Contains(out, "(grant_required)") {
		t.Errorf("history list without a grant: %d\n%s", code, out)
	}
	code, out = commandLine(t, bin, dir, "call", "dial", "600", "--key", "lab-cli-1", "--json")
	asked := answered(t, out)
	if code != 3 || asked.Status != control.StatusApprovalRequired || asked.OperationID == nil {
		t.Fatalf("call dial without a grant: %d\n%s", code, out)
	}
	w.until(10*time.Second, "the command line's request shown", func() bool { return w.shows("asks to:", "call 600") })
	if w.watch(2*time.Second, func() bool { return w.call.ID != 0 }) {
		t.Fatalf("a call (%+v) before the person allowed it", w.call)
	}
	if code, out := commandLine(t, bin, dir, "operation", "get", *asked.OperationID); code != 0 || !strings.Contains(out, `"state": "awaiting_approval"`) {
		t.Errorf("operation get before the decision: %d\n%s", code, out)
	}
	w.keys("Tab", "Return") // Allow, armed by now
	w.until(30*time.Second, "the call asked for on the command line connected", func() bool { return w.call.Phase == "established" && w.call.Remote == "600" })
	if code, out := commandLine(t, bin, dir, "operation", "get", *asked.OperationID); code != 0 || !strings.Contains(out, `"state": "succeeded"`) {
		t.Errorf("operation get after the yes: %d\n%s", code, out)
	}
	if code, out := commandLine(t, bin, dir, "call", "dial", "600", "--key", "lab-cli-1"); code != 0 || !strings.HasPrefix(out, "ok\n") {
		t.Errorf("the same command again, with its key: %d\n%s", code, out)
	}
	first := w.call.ID

	// The MCP server, started as an agent starts it, with no display of its own.
	agent := exec.Command(bin, "mcp")
	agent.Env = []string{"XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR"), "HOME=" + dir, "APPIMAGE_EXTRACT_AND_RUN=1"}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "lab"}, nil).Connect(ctx, &mcp.CommandTransport{Command: agent}, nil)
	if err != nil {
		t.Fatalf("the MCP server did not start: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 15 {
		t.Fatalf("the MCP tools: %v, %v", tools, err)
	}
	tool := func(name string, arguments map[string]any) (control.Response, bool) {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatalf("the tool %s: %v", name, err)
		}
		text, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("the tool %s answered %+v", name, result.Content)
		}
		return answered(t, text.Text), result.IsError
	}
	// A tool that needs a grant is refused without one; no tool approves anything.
	for _, name := range []string{"history_list", "config_show", "devices_list"} {
		if r, failed := tool(name, nil); !failed || r.Error == nil || r.Error.Code != control.GrantRequired {
			t.Errorf("the tool %s without a grant: %+v", name, r)
		}
	}
	if r, failed := tool("contacts_search", map[string]any{"query": ""}); !failed || r.Error == nil || r.Error.Code != control.GrantRequired {
		t.Errorf("the tool contacts_search without a grant: %+v", r)
	}
	if result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "approve", Arguments: map[string]any{"operation_id": *asked.OperationID}}); err == nil && !result.IsError {
		t.Errorf("a tool that approves exists: %+v", result)
	}
	// The agent sees the call, without the number, and asks to end it: that waits for Allow too.
	var hangup control.Response
	for range 5 { // the snapshot may move between the status and the request
		r, failed := tool("status_get", nil)
		if failed || json.Unmarshal(r.Data, &status) != nil || status.Call == nil || strings.Contains(string(r.Data), "600") {
			t.Fatalf("status_get during the call: %+v", r)
		}
		var refused bool
		hangup, refused = tool("call_hangup", map[string]any{"call_id": status.Call.CallID, "expected_version": status.Call.Version, "idempotency_key": "lab-mcp-1"})
		if !refused {
			break
		}
		if hangup.Error.Code != control.NotFound {
			t.Fatalf("call_hangup: %+v", hangup.Error)
		}
	}
	if hangup.Status != control.StatusApprovalRequired || hangup.OperationID == nil {
		t.Fatalf("an MCP hang-up without a grant: %+v (%+v)", hangup, hangup.Error)
	}
	w.until(10*time.Second, "the MCP request shown", func() bool { return w.shows("asks to:", "hang up the call") })
	if w.watch(2*time.Second, func() bool { return w.call.Phase != "established" }) {
		t.Fatalf("the call is %q before the person allowed the hang-up", w.call.Phase)
	}
	if r, _ := tool("operation_get", map[string]any{"operation_id": *hangup.OperationID}); !strings.Contains(string(r.Data), `"state":"awaiting_approval"`) {
		t.Errorf("operation_get before the decision: %s", r.Data)
	}
	w.keys("Tab", "Return")
	w.until(15*time.Second, "the call ended by the MCP request", func() bool { return w.call.ID == first && w.call.Phase == "ended" })
	if r, _ := tool("operation_get", map[string]any{"operation_id": *hangup.OperationID}); !strings.Contains(string(r.Data), `"state":"succeeded"`) {
		t.Errorf("operation_get after the yes: %s", r.Data)
	}
	// The MCP client leaves; the window goes on. Then the window ends and its database tells.
	session.Close()
	if w.watch(time.Second, func() bool { return w.registered != "registered" }) {
		t.Errorf("the registration is %q after the MCP client left", w.registered)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("the softphone did not end on a termination signal")
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if code, _ := commandLine(t, bin, dir, "status"); code == 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the command line still reaches a softphone after the window ended")
		}
	}
	if !w.unregistered(15 * time.Second) {
		t.Error("the softphone did not report that it had stopped")
	}
	trail := auditTrail(t, dir)
	want := []string{"call_dial requested destination ***00", "call_dial approved destination ***00", "call_dial succeeded -",
		"call_hangup requested -", "call_hangup approved -", "call_hangup succeeded -"}
	if !slices.Equal(trail, want) {
		t.Errorf("the audit trail:\n  %s\nwant:\n  %s", strings.Join(trail, "\n  "), strings.Join(want, "\n  "))
	}
	t.Logf("the audit trail: %s", strings.Join(trail, "; "))
}

// ringBlocks is the fraction of 20 ms blocks that carry the ring (one of its two notes, or both
// where one gives way to the other), and the fraction that carry any sound at all.
func ringBlocks(samples []int16) (notes, sounding float64) {
	return blocksWith(samples, engine.RingLowHz, engine.RingHighHz)
}

// micLevel is the microphone's level as the settings' sound test shows it.
var micLevel = regexp.MustCompile(`Microphone level\s+(\d+) %`)

// blocksWith is the share of the 20 ms blocks of a recording that hold these notes, and the share
// that hold any sound at all.
func blocksWith(samples []int16, freqs ...float64) (notes, sounding float64) {
	blocks := 0
	for ; len(samples) >= FrameSamples; samples = samples[FrameSamples:] {
		blocks++
		block := samples[:FrameSamples]
		loud := 0
		for _, v := range block {
			if v > 300 || v < -300 {
				loud++
			}
		}
		if loud > FrameSamples/10 {
			sounding++
			// A block in which one note gives way to the other holds half of each.
			held := 0.0
			for _, f := range freqs {
				held += share(block, f)
			}
			if held > 0.4 {
				notes++
			}
		}
	}
	if blocks == 0 {
		return 0, 0
	}
	return notes / float64(blocks), sounding / float64(blocks)
}

// grantFile is the path of a grant's file as the Agent access screen names it.
var grantFile = regexp.MustCompile(`/\S+/grant-\S+\.grant`)

// The Agent access screen of the built window, used as a person uses it: a permission is
// ticked, a grant file is made, the command line reads with it what it was refused before, the
// grant is revoked and refused again, and the audit log on the screen tells both.
func agentAccessForm(t *testing.T, bin, server, password string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(dir, "ui-trace.jsonl")
	cmd, exited := softphone(t, bin, dir, password, "--config", file, "--ui-trace", trace)
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
	if code, out := commandLine(t, bin, dir, "history", "list"); code != 3 || !strings.Contains(out, "(grant_required)") {
		t.Fatalf("history list without a grant: %d\n%s", code, out)
	}
	w.click("Menu")
	w.click("Settings")
	w.choose("Read call history")
	w.press("Create grant file")
	var grant string
	w.until(10*time.Second, "the grant made and listed", func() bool {
		grant = grantFile.FindString(w.page.Text)
		return grant != "" && w.shows("Grant created", "This session", "1 permission")
	})
	w.shot("agent-access-1-granted")
	if info, err := os.Stat(grant); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the grant's file %s: %v, %v", filepath.Base(grant), info, err)
	}
	if code, out := commandLine(t, bin, dir, "history", "list", "--grant-file", grant); code != 0 {
		t.Errorf("history list with the grant made on the screen: %d\n%s", code, out)
	}
	// What the grant does not cover is not given by it.
	if code, out := commandLine(t, bin, dir, "contacts", "search", "--grant-file", grant); code != 3 || !strings.Contains(out, "(grant_required)") {
		t.Errorf("contacts search with a grant for the history: %d\n%s", code, out)
	}
	w.press("Revoke")
	w.until(10*time.Second, "no grant listed", func() bool { return !w.shows("This session") && w.shows("Grants in use", "None.") })
	if code, out := commandLine(t, bin, dir, "history", "list", "--grant-file", grant); code != 3 || !strings.Contains(out, "(grant_required)") {
		t.Errorf("history list with the revoked grant: %d\n%s", code, out)
	}
	w.press("Show audit log")
	w.until(10*time.Second, "the audit log on the screen", func() bool { return w.shows("Hide audit log", "issued", "revoked") })
	w.shot("agent-access-2-revoked-and-audited")
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("the softphone did not end on a termination signal")
	}
	// An AppImage's launcher ends before its softphone has left the PBX: the next one waits.
	if !w.unregistered(15 * time.Second) {
		t.Error("the softphone did not report that it had stopped")
	}
	trail := auditTrail(t, dir)
	if !slices.Contains(trail, "grant issued 1 scopes, 15 min") || !slices.Contains(trail, "grant revoked -") {
		t.Errorf("the audit trail: %s", strings.Join(trail, "; "))
	}
	t.Logf("the audit trail: %s", strings.Join(trail, "; "))
}

// auditTrail reads the audit trail of a profile's database, oldest first.
func auditTrail(t *testing.T, dir string) []string {
	t.Helper()
	db, err := store.Open(app.StorePath(map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.AuditRows(100)
	if err != nil {
		t.Fatal(err)
	}
	var trail []string
	for i := len(rows) - 1; i >= 0; i-- {
		trail = append(trail, rows[i].Operation+" "+rows[i].Decision+" "+rows[i].Detail)
	}
	return trail
}

// ledgerOnly is the softphone as far as issuing a grant needs it: its database.
type ledgerOnly struct{ db *store.Store }

func (ledgerOnly) Send(domain.Command) error { return nil }
func (ledgerOnly) SetDND(bool) error         { return nil }
func (ledgerOnly) Settings() app.Settings    { return app.Settings{} }
func (l ledgerOnly) ContactsUpTo(search string, limit int) ([]app.Contact, error) {
	return l.db.SearchContacts(search, limit)
}
func (l ledgerOnly) HistoryUpTo(filter app.HistoryFilter, search string, limit int) ([]app.HistoryEntry, error) {
	return l.db.RecentCalls(limit, filter, search)
}
func (ledgerOnly) AudioDevices() (inputs, outputs []ports.Device, err error) { return nil, nil, nil }
func (l ledgerOnly) Ledger() (app.Ledger, error)                             { return l.db, nil }

// headlessService is the softphone without a window (go-microsip service): it starts on a
// bootstrap grant exported from the window, registers, runs what the grant covers and refuses
// the rest without asking anybody, turns a window and a link away, and stops on a signal. The
// bootstrap starts one session only.
//
// The window's screen that exports the bootstrap is not driven here: the test issues it with
// the window's own code, on the profile's database, before the service starts.
func headlessService(t *testing.T, bin, server, password string) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1002\"\ndomain = %q\nport = 5060\ntransport = \"udp\"\n", server)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(app.StorePath(map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data")}))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := control.NewOwner(ledgerOnly{db}, nil)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := issuer.IssueBootstrap(control.GrantSpec{Scopes: []string{"control:dial", "control:hangup", "control:dnd", "read:history"},
		Destinations: []string{"600"}, Minutes: 10, Quota: 1})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := control.WriteGrantFile(filepath.Join(dir, "config", "go-microsip", "bootstrap"), issued)
	if err != nil {
		t.Fatal(err)
	}
	issuer.Close()
	db.Close()

	output := func() string {
		logs, _ := filepath.Glob(filepath.Join(dir, "output-*.log"))
		var all []byte
		for _, name := range logs {
			text, _ := os.ReadFile(name)
			all = append(all, text...)
		}
		return string(all)
	}
	said := func(what string, d time.Duration) {
		t.Helper()
		for deadline := time.Now().Add(d); !strings.Contains(output(), what); time.Sleep(100 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("the service did not say %q:\n%s", what, output())
			}
		}
	}
	service, exited := softphone(t, bin, dir, password, "service", "--grant-file", grant, "--config", file)
	said("service ready (epoch 2)", 30*time.Second)
	with := func(args ...string) (int, string) {
		return commandLine(t, bin, dir, append(args, "--grant-file", grant)...)
	}
	status := func() labStatus {
		t.Helper()
		code, out := with("status", "--json")
		var s labStatus
		if r := answered(t, out); code != 0 || json.Unmarshal(r.Data, &s) != nil || s.Mode != "service" {
			t.Fatalf("the service's status: %d\n%s", code, out)
		}
		return s
	}
	for deadline := time.Now().Add(30 * time.Second); status().Registration != "registered"; time.Sleep(300 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the service did not register: %+v\n%s", status(), output())
		}
	}
	// Without the grant, and outside it, nothing runs and nobody is asked.
	for name, line := range map[string][]string{
		"a call without the grant":          {"call", "dial", "600"},
		"do-not-disturb without the grant":  {"dnd", "on"},
		"history without the grant":         {"history", "list"},
		"a call outside the grant, with it": {"call", "dial", "601", "--grant-file", grant},
		"contacts, which the grant lacks":   {"contacts", "search", "--grant-file", grant},
	} {
		if code, out := commandLine(t, bin, dir, line...); code != 3 || !strings.Contains(out, "(grant_required)") {
			t.Errorf("%s: %d\n%s", name, code, out)
		}
	}
	// What the grant covers runs at once.
	if code, out := with("dnd", "on", "--key", "lab-svc-dnd"); code != 0 || !strings.HasPrefix(out, "accepted\n") {
		t.Errorf("do-not-disturb by the grant: %d\n%s", code, out)
	}
	if !status().DND {
		t.Error("do-not-disturb is not on after the service took it")
	}
	if code, out := with("history", "list", "--json"); code != 0 || !strings.Contains(out, `"items":`) {
		t.Errorf("history by the grant: %d\n%s", code, out)
	}
	if code, out := with("call", "dial", "600", "--key", "lab-svc-dial"); code != 0 || !strings.HasPrefix(out, "accepted\n") {
		t.Fatalf("a call by the grant: %d\n%s", code, out)
	}
	var call labStatus
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		if call = status(); call.Call != nil && call.Call.Phase == "established" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the call the service placed did not connect: %+v\n%s", call, output())
		}
	}
	said("microphone active", 5*time.Second)
	// The quota was one call.
	if code, out := with("call", "dial", "600", "--key", "lab-svc-dial-2"); code != 3 || !strings.Contains(out, "(grant_required)") {
		t.Errorf("a second call, over the quota: %d\n%s", code, out)
	}
	// A window and a link are turned away; a second service finds the profile taken.
	if code, out := commandLine(t, bin, dir); code != 4 || !strings.Contains(out, "engine_owned_by_service") {
		t.Errorf("the window beside the service: %d\n%s", code, out)
	}
	if code, out := commandLine(t, bin, dir, "tel:600"); code != 4 || !strings.Contains(out, "uri_confirmation_unavailable") {
		t.Errorf("a link beside the service: %d\n%s", code, out)
	}
	if code, out := commandLine(t, bin, dir, "service", "--grant-file", grant, "--config", file); code != 4 || !strings.Contains(out, "owns the profile") {
		t.Errorf("a second service: %d\n%s", code, out)
	}
	// The call is ended by the grant.
	ended := false
	for range 5 {
		call = status()
		if call.Call == nil {
			t.Fatal("the call ended by itself")
		}
		code, out := with("call", "hangup", strconv.FormatUint(call.Call.CallID, 10), strconv.FormatUint(call.Call.Version, 10), "--key", "lab-svc-hangup")
		if ended = code == 0; ended || !strings.Contains(out, "(not_found)") {
			break
		}
	}
	if !ended {
		t.Fatal("the hang-up by the grant was not taken")
	}
	for deadline := time.Now().Add(15 * time.Second); status().Call != nil; time.Sleep(300 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the call did not end")
		}
	}
	said("microphone released", 5*time.Second)
	// A signal stops it: it unregisters and leaves with 0.
	if err := syscall.Kill(-service.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		// An AppImage's launcher is ended by the signal itself; the softphone it started is
		// the one that leaves in order, and its status is not the launcher's.
		if err != nil && !strings.HasSuffix(bin, ".AppImage") {
			t.Errorf("the service left with %v, want 0", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the service did not stop on a termination signal")
	}
	said("service stopping", 10*time.Second)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		code, out := commandLine(t, bin, dir, "status")
		if code == 4 && strings.Contains(out, "(engine_unavailable)") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status after the service stopped: %d\n%s", code, out)
		}
	}
	// The bootstrap started that session and starts no other.
	if code, out := commandLineWith(t, password, bin, dir, "service", "--grant-file", grant, "--config", file); code != 3 || !strings.Contains(out, "already used") {
		t.Errorf("the service started again on the same bootstrap: %d\n%s", code, out)
	}
	trail := auditTrail(t, dir)
	want := []string{"grant bootstrap issued 4 scopes, 10 min", "dnd_set granted enabled=true by grant " + issued.ID, "dnd_set succeeded -",
		"call_dial granted destination ***00 by grant " + issued.ID, "call_dial succeeded -",
		"call_hangup granted - by grant " + issued.ID, "call_hangup succeeded -"}
	if !slices.Equal(trail, want) {
		t.Errorf("the audit trail:\n  %s\nwant:\n  %s", strings.Join(trail, "\n  "), strings.Join(want, "\n  "))
	}
	if all := strings.Join(trail, " ") + output(); strings.Contains(all, issued.Capability) {
		t.Error("the capability is in the audit trail or in the service's output")
	}
	t.Logf("the audit trail: %s", strings.Join(trail, "; "))
}
