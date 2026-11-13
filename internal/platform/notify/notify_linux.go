package notify

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

const (
	dest = "org.freedesktop.Notifications"
	path = "/org/freedesktop/Notifications"
	// callTimeout bounds one call to the notification service.
	callTimeout = 5 * time.Second
	// expireMS is how long the notification stays; it is also closed when the call stops ringing.
	expireMS = 60_000
)

// serve talks to the notification service for the life of the process. It owns the map from
// calls to notifications; nothing else touches it.
func (d *Desktop) serve() {
	var (
		conn    *dbus.Conn
		buttons bool
		signals chan *dbus.Signal
		shown   = map[uint64]uint32{} // call -> notification
		owner   string                // the notification service's own name on the bus
	)
	// serving asks the bus who the notification service is now. A button counts only when it
	// comes from that one: any process of the session can send a signal that looks like it. A
	// service that was replaced does not know the notifications of the one before.
	serving := func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var now string
		named := func() bool {
			return conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, dest).Store(&now) == nil
		}
		// A service that left has no owner, and the bus starts it again only for a call made to
		// the service itself: one is made, then the bus is asked again.
		if !named() && (conn.Object(dest, path).CallWithContext(ctx, dest+".GetCapabilities", 0).Err != nil || !named()) {
			return false
		}
		if now != owner {
			owner, shown = now, map[uint64]uint32{}
		}
		return true
	}
	connect := func() bool {
		if conn != nil {
			return true
		}
		c, err := dbus.SessionBus()
		if err != nil {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var capabilities []string
		if err := c.Object(dest, path).CallWithContext(ctx, dest+".GetCapabilities", 0).Store(&capabilities); err != nil {
			return false // no notification service: asked again at the next call
		}
		for _, capability := range capabilities {
			buttons = buttons || capability == "actions"
		}
		if err := c.AddMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(dest)); err != nil {
			return false
		}
		signals = make(chan *dbus.Signal, 2*queue)
		c.Signal(signals)
		conn = c
		return true
	}
	callOf := func(id uint32) (uint64, bool) {
		for call, shownID := range shown {
			if shownID == id {
				return call, true
			}
		}
		return 0, false
	}
	for {
		select {
		case r := <-d.requests:
			if !connect() || !serving() {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
			service := conn.Object(dest, path)
			switch id, known := shown[r.call]; {
			case r.show, r.missed:
				// A missed call takes the place of its ringing notification, with no button, and
				// stays for as long as the desktop keeps such notifications.
				summary, urgency, category, expire := "Incoming call", byte(2), "call.incoming", int32(expireMS)
				var actions []string
				if r.missed {
					summary, urgency, category, expire = "Missed call", 1, "call.unanswered", -1
				} else if buttons {
					actions = []string{"answer", "Answer", "reject", "Reject"}
				}
				hints := map[string]dbus.Variant{
					"urgency":       dbus.MakeVariant(urgency),
					"category":      dbus.MakeVariant(category),
					"desktop-entry": dbus.MakeVariant("go-microsip"),
				}
				var fresh uint32
				err := service.CallWithContext(ctx, dest+".Notify", 0, "go-microsip", id, "go-microsip",
					summary, r.caller, actions, hints, expire).Store(&fresh)
				switch {
				case r.missed:
					delete(shown, r.call) // nothing of this call is left to decide or to take away
					if err != nil && known {
						_ = service.CallWithContext(ctx, dest+".CloseNotification", 0, id).Err
					}
				case err == nil:
					shown[r.call] = fresh
				}
			case known:
				_ = service.CallWithContext(ctx, dest+".CloseNotification", 0, id).Err
				delete(shown, r.call)
			}
			cancel()
		case s := <-signals:
			if s == nil || len(s.Body) < 2 || s.Sender != owner {
				continue // not from the notification service that showed ours
			}
			id, _ := s.Body[0].(uint32)
			call, known := callOf(id)
			if !known {
				continue // someone else's notification
			}
			switch s.Name {
			case dest + ".ActionInvoked":
				key, _ := s.Body[1].(string)
				if key != "answer" && key != "reject" {
					continue
				}
				select {
				case d.actions <- ports.CallNoticeAction{Call: call, Answer: key == "answer"}:
				default:
				}
			case dest + ".NotificationClosed":
				delete(shown, call)
			}
		}
	}
}
