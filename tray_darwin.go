package main

// trayAvailable is false on macOS for now: the tray library and Wails both
// need the main thread there, so dsync quits when its window is closed,
// like other apps without a menu bar icon.
func trayAvailable() bool { return false }
