package gui

import "github.com/godbus/dbus/v5"

// trayHost reports whether the desktop shows tray icons now: something owns the
// StatusNotifierItem watcher on the session bus (KDE, GNOME with the AppIndicator extension, most
// panels). Without one a hidden window could not be brought back.
func trayHost() bool {
	conn, err := dbus.SessionBus() // the connection the toolkit's tray uses: never closed here
	if err != nil {
		return false
	}
	var owned bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owned)
	return err == nil && owned
}
