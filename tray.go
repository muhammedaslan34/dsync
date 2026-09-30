package main

import (
	"context"
	"embed"
	"fmt"
	"runtime"
	"sync"

	"fyne.io/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"dsync/internal/node"
)

// The tray icon keeps dsync reachable while its window is closed, which is
// what lets clipboard sync and receiving keep working in the background.

//go:embed build/tray/tray.png build/tray/tray-unread.png build/tray/tray.ico build/tray/tray-unread.ico
var trayIcons embed.FS

type trayState struct {
	mu       sync.Mutex
	running  bool // the icon is in the tray
	hidden   bool // the window is hidden
	unread   bool // something arrived while hidden
	quitting bool
	open     *systray.MenuItem
	status   *systray.MenuItem
	clip     *systray.MenuItem
	quitItem *systray.MenuItem
}

func trayIcon(unread bool) []byte {
	name := "build/tray/tray"
	if unread {
		name += "-unread"
	}
	if runtime.GOOS == "windows" {
		name += ".ico"
	} else {
		name += ".png"
	}
	b, _ := trayIcons.ReadFile(name)
	return b
}

// startTray shows the tray icon if this desktop has a tray.
func (a *App) startTray() {
	if !trayAvailable() {
		return
	}
	go func() {
		// The tray runs its own message loop, which on Windows must stay on
		// the thread that created the icon.
		runtime.LockOSThread()
		systray.Run(a.trayReady, func() {})
	}()
}

func (a *App) trayReady() {
	systray.SetIcon(trayIcon(false))
	systray.SetTitle("dsync")
	systray.SetTooltip("dsync")
	systray.SetOnTapped(a.showWindow)

	open := systray.AddMenuItem(a.tr("tray.open"), a.tr("tray.openTip"))
	systray.AddSeparator()
	status := systray.AddMenuItem("", "")
	status.Disable()
	cs := a.node.ClipboardStatus()
	clip := systray.AddMenuItemCheckbox(a.tr("tray.clip"), a.tr("tray.clipTip"), cs.Enabled)
	if !cs.Available {
		clip.Disable()
	}
	systray.AddSeparator()
	quit := systray.AddMenuItem(a.tr("tray.quit"), a.tr("tray.quitTip"))

	a.tray.mu.Lock()
	a.tray.running, a.tray.open, a.tray.status, a.tray.clip, a.tray.quitItem = true, open, status, clip, quit
	a.tray.mu.Unlock()
	a.updateTray()

	go func() {
		for {
			select {
			case <-open.ClickedCh:
				a.showWindow()
			case <-clip.ClickedCh:
				on := !a.node.ClipboardStatus().Enabled
				if a.node.SetClipboardSync(on) == nil {
					a.updateTray()
					a.emit("clipboard:status", a.node.ClipboardStatus())
				}
			case <-quit.ClickedCh:
				a.quit()
				return
			}
		}
	}()
}

// relabelTray puts the menu in the current language, after it changed in
// the settings.
func (a *App) relabelTray() {
	a.tray.mu.Lock()
	if a.tray.running {
		a.tray.open.SetTitle(a.tr("tray.open"))
		a.tray.open.SetTooltip(a.tr("tray.openTip"))
		a.tray.clip.SetTitle(a.tr("tray.clip"))
		a.tray.clip.SetTooltip(a.tr("tray.clipTip"))
		a.tray.quitItem.SetTitle(a.tr("tray.quit"))
		a.tray.quitItem.SetTooltip(a.tr("tray.quitTip"))
	}
	a.tray.mu.Unlock()
	a.updateTray()
}

// updateTray refreshes the icon and menu from the current state.
func (a *App) updateTray() {
	a.tray.mu.Lock()
	defer a.tray.mu.Unlock()
	if !a.tray.running {
		return
	}
	a.tray.status.SetTitle(devicesOnline(a.language(), a.node.OnlinePaired()))
	if a.node.ClipboardStatus().Enabled {
		a.tray.clip.Check()
	} else {
		a.tray.clip.Uncheck()
	}
	systray.SetIcon(trayIcon(a.tray.unread))
	// On Linux the title set in trayReady can be dropped if the item
	// wasn't fully registered yet, so set it again here.
	systray.SetTitle("dsync")
	tip := "dsync"
	if a.tray.unread {
		tip = a.tr("tray.unread")
	}
	systray.SetTooltip(tip)
}

func (a *App) showWindow() {
	a.tray.mu.Lock()
	a.tray.hidden, a.tray.unread = false, false
	a.tray.mu.Unlock()
	wruntime.WindowShow(a.ctx)
	wruntime.WindowUnminimise(a.ctx)
	a.updateTray()
}

func (a *App) windowHidden() bool {
	a.tray.mu.Lock()
	defer a.tray.mu.Unlock()
	return a.tray.hidden
}

func (a *App) quit() {
	a.tray.mu.Lock()
	a.tray.quitting = true
	a.tray.mu.Unlock()
	wruntime.Quit(a.ctx)
}

// beforeClose hides the window instead of quitting while the tray icon is
// there to bring it back.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	a.tray.mu.Lock()
	keep := a.tray.running && !a.tray.quitting && !a.node.QuitOnClose()
	if keep {
		a.tray.hidden = true
	}
	a.tray.mu.Unlock()
	if !keep {
		return false
	}
	wruntime.WindowHide(ctx)
	if a.node.FirstTrayHint() {
		a.notify(a.tr("notify.running"), a.tr("notify.runningTx"), "")
	}
	return true
}

// onEvent reacts to node events for the tray and notifications.
func (a *App) onEvent(event string, data any) {
	switch event {
	case node.EventPeers:
		a.updateTray()
	case node.EventPairRequest, node.EventControlPIN:
		a.showWindow() // it needs someone here
	case node.EventMessage:
		m := data.(node.Message)
		if m.Incoming && m.File == nil && a.windowHidden() {
			a.markUnread()
			a.notify(m.PeerName, m.Text, m.PeerID)
		}
	case node.EventMessageUpdate:
		m := data.(node.Message)
		if m.Incoming && m.File != nil && m.File.Status == node.StatusDone && a.windowHidden() {
			a.markUnread()
			key := "notify.file"
			if m.File.Folder {
				key = "notify.folder"
			}
			a.notify(m.PeerName, a.tr(key, m.File.Name), m.PeerID)
		}
	}
}

func (a *App) markUnread() {
	a.tray.mu.Lock()
	a.tray.unread = true
	a.tray.mu.Unlock()
	a.updateTray()
}

// notify shows a desktop notification; clicking it opens peerID's
// conversation (if set).
func (a *App) notify(title, body, peerID string) {
	if !a.notifications {
		return
	}
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	wruntime.SendNotification(a.ctx, wruntime.NotificationOptions{
		ID:    fmt.Sprintf("dsync-%d", a.notifySeq.Add(1)),
		Title: title,
		Body:  body,
		Data:  map[string]any{"peerId": peerID},
	})
}

func (a *App) onNotificationClick(r wruntime.NotificationResult) {
	a.showWindow()
	if id, _ := r.Response.UserInfo["peerId"].(string); id != "" {
		wruntime.EventsEmit(a.ctx, "open-peer", id)
	}
}
