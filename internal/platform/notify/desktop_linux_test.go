package notify

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// On a desktop's own session (the VM check, build/vm/check.sh): the desktop's notification
// service is asked to show a call and to take it away. The service tells the one that asked
// when a notification closes, which is what shows it had one.
func TestTheDesktopsNotificationServiceShowsACall(t *testing.T) {
	if os.Getenv("GO_MICROSIP_NOTIFY_TEST") != "desktop" {
		t.Skip("set GO_MICROSIP_NOTIFY_TEST=desktop in a desktop session (make go-vm)")
	}
	conn, err := dbus.SessionBus() // the notifier's own connection: closes are told to it
	if err != nil {
		t.Fatal(err)
	}
	var name, vendor, version, spec string
	if err := conn.Object(dest, path).Call(dest+".GetServerInformation", 0).Store(&name, &vendor, &version, &spec); err != nil {
		t.Fatalf("the desktop has no notification service: %v", err)
	}
	var capabilities []string
	if err := conn.Object(dest, path).Call(dest+".GetCapabilities", 0).Store(&capabilities); err != nil {
		t.Fatal(err)
	}
	t.Logf("the desktop's notification service: %s %s (%s); buttons offered: %v", name, version, vendor, slices.Contains(capabilities, "actions"))
	if err := conn.AddMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(dest), dbus.WithMatchMember("NotificationClosed")); err != nil {
		t.Fatal(err)
	}
	closed := make(chan *dbus.Signal, 16)
	conn.Signal(closed)
	d := New()
	d.Incoming(1, "Ana")
	time.Sleep(3 * time.Second) // on the screen
	d.Close(1)
	for deadline := time.After(10 * time.Second); ; {
		select {
		case s := <-closed:
			if s.Name != dest+".NotificationClosed" || len(s.Body) != 2 {
				continue
			}
			t.Logf("the service took notification %v away (reason %v) when the call was over", s.Body[0], s.Body[1])
			return
		case <-deadline:
			t.Fatal("the service did not say the call's notification was closed: it was not shown, or not taken away")
		}
	}
}
