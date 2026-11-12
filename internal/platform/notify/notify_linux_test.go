package notify

import (
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// service stands in for the desktop's notification service on a private session bus.
type service struct {
	conn *dbus.Conn
	mu   sync.Mutex
	next uint32
	// shown are the notifications asked for, closed the ones taken away.
	shown  []shownNotice
	closed []uint32
}

type shownNotice struct {
	id            uint32
	replaces      uint32
	summary, body string
	actions       []string
	urgency       byte
}

func (s *service) GetCapabilities() ([]string, *dbus.Error) {
	return []string{"actions", "body"}, nil
}

func (s *service) Notify(app string, replaces uint32, icon, summary, body string, actions []string, hints map[string]dbus.Variant, expire int32) (uint32, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := replaces
	if id == 0 {
		s.next++
		id = s.next
	}
	urgency, _ := hints["urgency"].Value().(byte)
	s.shown = append(s.shown, shownNotice{id: id, replaces: replaces, summary: summary, body: body, actions: actions, urgency: urgency})
	return id, nil
}

func (s *service) CloseNotification(id uint32) *dbus.Error {
	s.mu.Lock()
	s.closed = append(s.closed, id)
	s.mu.Unlock()
	return nil
}

func (s *service) press(id uint32, key string) {
	_ = s.conn.Emit(path, dest+".ActionInvoked", id, key)
}

func (s *service) wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		s.mu.Lock()
		ok := cond()
		s.mu.Unlock()
		if ok {
			return
		}
	}
	t.Fatalf("no %s", what)
}

// The notifier against a notification service on a private session bus (make go-notify): a call
// that rings is shown with its caller as plain text and two buttons; the buttons come back as
// actions of that call; a notification is taken away when asked; a button of someone else's
// notification, or one that is not ours, is nothing.
func TestCallNotificationsOnASessionBus(t *testing.T) {
	if os.Getenv("GO_MICROSIP_NOTIFY_TEST") != "bus" {
		t.Skip("set GO_MICROSIP_NOTIFY_TEST=bus on a private session bus (make go-notify)")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fake := &service{conn: conn}
	if err := conn.Export(fake, path, dest); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(dest, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("the test could not be the notification service of its bus: %v %v", reply, err)
	}
	d := New()
	action := func(what string) ports.CallNoticeAction {
		t.Helper()
		select {
		case a := <-d.Actions():
			return a
		case <-time.After(10 * time.Second):
			t.Fatalf("no action for %s", what)
			return ports.CallNoticeAction{}
		}
	}

	d.Incoming(7, "<b>Ana</b> & co")
	fake.wait(t, "notification of call 7", func() bool { return len(fake.shown) == 1 })
	first := fake.shown[0]
	if first.summary != "Incoming call" || first.body != "&lt;b&gt;Ana&lt;/b&gt; &amp; co" || first.urgency != 2 ||
		!slices.Equal(first.actions, []string{"answer", "Answer", "reject", "Reject"}) {
		t.Errorf("the notification of call 7: %+v", first)
	}
	d.Incoming(8, "1003")
	fake.wait(t, "notification of call 8", func() bool { return len(fake.shown) == 2 })
	second := fake.shown[1]

	fake.press(second.id, "reject")
	if a := action("the Reject of call 8"); a != (ports.CallNoticeAction{Call: 8}) {
		t.Errorf("Reject of call 8 came back as %+v", a)
	}
	fake.press(first.id, "answer")
	if a := action("the Answer of call 7"); a != (ports.CallNoticeAction{Call: 7, Answer: true}) {
		t.Errorf("Answer of call 7 came back as %+v", a)
	}
	// Not ours: another notification's button, a button we never offered, and a button of ours
	// "pressed" by another process of the session, which is not the notification service.
	fake.press(999, "answer")
	fake.press(first.id, "default")
	other, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.Emit(path, dest+".ActionInvoked", first.id, "answer"); err != nil {
		t.Fatal(err)
	}
	select {
	case a := <-d.Actions():
		t.Errorf("an action nobody asked for: %+v", a)
	case <-time.After(300 * time.Millisecond):
	}

	// The same call rings on: its notification is replaced, not added to.
	d.Incoming(7, "Ana")
	fake.wait(t, "the replaced notification of call 7", func() bool { return len(fake.shown) == 3 })
	if again := fake.shown[2]; again.replaces != first.id || again.id != first.id {
		t.Errorf("call 7 shown again as %+v, want a replacement of %d", again, first.id)
	}
	d.Close(7)
	fake.wait(t, "the notification of call 7 taken away", func() bool { return slices.Contains(fake.closed, first.id) })
	d.Close(7) // already gone: nothing more is asked
	d.Close(99)
	time.Sleep(200 * time.Millisecond)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.closed) != 1 {
		t.Errorf("notifications taken away: %v, want only that of call 7", fake.closed)
	}
}

// Without a bus, or without a notification service on it, nothing is shown, nothing blocks and
// nothing fails: the window is the way to answer.
func TestNoNotificationServiceIsQuiet(t *testing.T) {
	if os.Getenv("GO_MICROSIP_NOTIFY_TEST") != "none" {
		t.Skip("set GO_MICROSIP_NOTIFY_TEST=none in a session without a notification service (make go-notify)")
	}
	d := New()
	start := time.Now()
	for call := uint64(1); call <= 3*queue; call++ { // more than the queue holds
		d.Incoming(call, "1002")
		d.Close(call)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("asking for notifications nobody shows took %v", took)
	}
	select {
	case a := <-d.Actions():
		t.Errorf("an action without a service: %+v", a)
	case <-time.After(300 * time.Millisecond):
	}
}
