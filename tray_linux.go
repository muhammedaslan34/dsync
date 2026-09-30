package main

import "github.com/godbus/dbus/v5"

// trayAvailable reports whether the desktop shows tray icons. On Linux that
// needs a StatusNotifier host: KDE and most desktops have one, GNOME only
// with the AppIndicator extension.
func trayAvailable() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var has bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has)
	return err == nil && has
}
