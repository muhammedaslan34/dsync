//go:build !linux

package main

// trayAvailable reports whether the desktop shows tray icons, which Windows
// and macOS always do.
func trayAvailable() bool { return true }
