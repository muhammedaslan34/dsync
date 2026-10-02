package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"dsync/internal/clip"
	"dsync/internal/config"
	"dsync/internal/control"
	"dsync/internal/discovery"
	"dsync/internal/node"
	"dsync/internal/proto"
	"dsync/internal/version"
)

// App exposes the node to the frontend. Its exported methods are callable
// from JavaScript, and node events are forwarded as Wails events.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc
	node   *node.Node

	mu        sync.Mutex
	statusErr string

	tray          trayState
	upd           updateState
	notifications bool // desktop notifications work here
	notifySeq     atomic.Int64
}

func NewApp(cfg *config.Config) (*App, error) {
	a := &App{}
	n, err := node.New(cfg, a.emit)
	if err != nil {
		return nil, err
	}
	a.node = n
	return a, nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.node.SetClipboard(clip.System())
	if wruntime.InitializeNotifications(ctx) == nil {
		a.notifications = true
		wruntime.OnNotificationResponse(ctx, a.onNotificationClick)
	}
	a.startTray()
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go a.autoCheckUpdates(runCtx)
	go func() {
		if err := a.node.Run(runCtx); err != nil {
			a.mu.Lock()
			a.statusErr = err.Error()
			a.mu.Unlock()
			wruntime.EventsEmit(ctx, "error", err.Error())
		}
	}()
}

func (a *App) shutdown(ctx context.Context) {
	a.cancel()
	if a.notifications {
		wruntime.CleanupNotifications(ctx)
	}
	a.tray.mu.Lock()
	running := a.tray.running
	a.tray.mu.Unlock()
	if running {
		systray.Quit()
	}
}

func (a *App) emit(event string, data any) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, event, data)
		a.onEvent(event, data)
	}
}

// Status returns the error that stopped the background service, if any.
func (a *App) Status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.statusErr
}

func (a *App) Self() proto.Device         { return a.node.Self() }
func (a *App) SetName(name string) error  { return a.node.SetName(name) }
func (a *App) Peers() []node.Peer         { return a.node.Peers() }
func (a *App) History() []node.Message    { return a.node.History() }
func (a *App) ForgetPeer(id string) error { return a.node.ForgetPeer(id) }

func (a *App) Scan() {
	go a.node.Scan(a.ctx)
}

func (a *App) AddPeer(addr string) (node.Peer, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	return a.node.AddPeer(ctx, addr)
}

func (a *App) SendText(peerID, text string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return a.node.SendText(ctx, peerID, text)
}

// PickFiles opens a file dialog and sends the chosen files to a peer.
func (a *App) PickFiles(peerID string) error {
	paths, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{Title: a.tr("dialog.files")})
	if err != nil || len(paths) == 0 {
		return err
	}
	return a.node.SendFiles(a.ctx, peerID, paths)
}

// PickFolder opens a folder dialog and sends the chosen folder to a peer.
func (a *App) PickFolder(peerID string) error {
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: a.tr("dialog.folder")})
	if err != nil || dir == "" {
		return err
	}
	return a.node.SendFiles(a.ctx, peerID, []string{dir})
}

// SendPaths sends files and folders dropped onto the window.
func (a *App) SendPaths(peerID string, paths []string) error {
	return a.node.SendFiles(a.ctx, peerID, paths)
}

func (a *App) CancelTransfer(id int64) { a.node.CancelTransfer(id) }

// RetryTransfer sends a failed outgoing file again, resuming if possible.
func (a *App) RetryTransfer(id int64) error { return a.node.RetryTransfer(a.ctx, id) }

func (a *App) ReceiveDir() string { return a.node.ReceiveDir() }

// ChooseReceiveDir lets the user pick where received files are saved.
func (a *App) ChooseReceiveDir() (string, error) {
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:            a.tr("dialog.receive"),
		DefaultDirectory: a.node.ReceiveDir(),
	})
	if err != nil || dir == "" {
		return a.node.ReceiveDir(), err
	}
	return dir, a.node.SetReceiveDir(dir)
}

// OpenPath opens a file or folder with the system's default app.
func (a *App) OpenPath(path string) error { return openPath(path) }

// RevealPath shows a file in the system file manager.
func (a *App) RevealPath(path string) error { return revealPath(path) }

func (a *App) LocalAddrs() []discovery.LocalAddr { return a.node.LocalAddrs() }

func (a *App) FindOnNetwork() []node.Peer {
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	return a.node.FindOnNetwork(ctx)
}

// Fingerprint is this device's key fingerprint, for display.
func (a *App) Fingerprint() string { return a.node.Fingerprint() }

// StartPair asks a device to pair and returns the code to compare. The
// outcome arrives as a "pair:result" event.
func (a *App) StartPair(peerID string) (string, error) {
	return a.node.StartPair(a.ctx, peerID)
}

func (a *App) CancelPair(peerID string) { a.node.CancelPair(peerID) }

func (a *App) AnswerPair(requestID string, accept bool) { a.node.AnswerPair(requestID, accept) }

func (a *App) PendingPairs() []node.PairRequest { return a.node.PendingPairs() }

func (a *App) ReceiveSettings() node.ReceiveSettings { return a.node.ReceiveSettings() }

func (a *App) SetAskBeforeReceiving(on bool) error { return a.node.SetAskBeforeReceiving(on) }

func (a *App) PendingIncoming() []node.IncomingRequest { return a.node.PendingIncoming() }

func (a *App) AnswerIncoming(requestID string, accept bool) {
	a.node.AnswerIncoming(requestID, accept)
}

// SetControlPermission lets a paired peer control this computer. It is a
// separate permission from text and file sharing.
func (a *App) SetControlPermission(peerID string, allow bool) error {
	return a.node.SetControlPermission(peerID, allow)
}

// AnswerControlPIN confirms or declines automatic Sunshine PIN submission.
func (a *App) AnswerControlPIN(requestID string, accept bool) {
	a.node.AnswerControlPIN(requestID, accept)
}

func (a *App) Unpair(peerID string) error { return a.node.Unpair(a.ctx, peerID) }

// SendPasted sends data pasted into the window (e.g. a copied image), given
// as base64 because that is how bytes travel from JavaScript.
func (a *App) SendPasted(peerID, name, dataBase64 string) error {
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return err
	}
	return a.node.SendData(a.ctx, peerID, name, data)
}

// imageHandler serves pictures for the conversation at /image/{message id}.
// Only image files recorded in the history can be served.
func (a *App) imageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idStr, ok := strings.CutPrefix(r.URL.Path, "/image/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if !ok || err != nil {
			http.NotFound(w, r)
			return
		}
		path, ok := a.node.ImagePath(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		http.ServeFile(w, r, path)
	})
}

func (a *App) ClipboardStatus() node.ClipboardStatus { return a.node.ClipboardStatus() }

func (a *App) SetClipboardSync(on bool) error { return a.node.SetClipboardSync(on) }

// BackgroundSettings are the settings for running without the window.
type BackgroundSettings struct {
	TrayAvailable      bool `json:"trayAvailable"`
	KeepInTray         bool `json:"keepInTray"`
	AutostartSupported bool `json:"autostartSupported"`
	Autostart          bool `json:"autostart"`
	AppMenuSupported   bool `json:"appMenuSupported"`
	AppMenu            bool `json:"appMenu"`
}

func (a *App) Background() BackgroundSettings {
	a.tray.mu.Lock()
	running := a.tray.running
	a.tray.mu.Unlock()
	return BackgroundSettings{
		TrayAvailable:      running,
		KeepInTray:         !a.node.QuitOnClose(),
		AutostartSupported: autostartSupported(),
		Autostart:          autostartEnabled(),
		AppMenuSupported:   appMenuSupported(),
		AppMenu:            appMenuEnabled(),
	}
}

func (a *App) SetKeepInTray(keep bool) error { return a.node.SetQuitOnClose(!keep) }
func (a *App) SetAutostart(on bool) error    { return setAutostart(on) }
func (a *App) SetAppMenu(on bool) error      { return setAppMenu(on) }

// StartControl opens Moonlight controlling a paired device, setting up
// Sunshine and pairing first if needed; progress arrives as "control"
// events.
func (a *App) StartControl(peerID string, opts node.ControlOptions) error {
	return a.node.StartControl(a.ctx, peerID, opts)
}

// ControlInfo asks a device which screens it offers for remote control.
func (a *App) ControlInfo(peerID string) node.ControlInfo {
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()
	return a.node.ControlInfo(ctx, peerID)
}

func (a *App) CancelControl(peerID string) { a.node.CancelControl(peerID) }

// HostInfo reports this computer's Moonlight and Sunshine setup.
func (a *App) HostInfo() node.HostInfo { return a.node.HostInfo() }

// SetSunshineLogin saves the Sunshine web login (checked first); an empty
// user removes it.
func (a *App) SetSunshineLogin(user, password string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return a.node.SetSunshineLogin(ctx, user, password)
}

// OpenURL opens a web page, e.g. Sunshine's PIN page.
func (a *App) OpenURL(url string) { wruntime.BrowserOpenURL(a.ctx, url) }

// Version is the dsync version, shown in the settings.
func (a *App) Version() string { return version.Version }

// FirewallStatus says whether Windows Firewall lets other computers reach
// dsync.
func (a *App) FirewallStatus() FirewallStatus { return firewallStatus() }

// FixFirewall allows dsync through Windows Firewall (with the admin prompt).
func (a *App) FixFirewall() error { return fixFirewall() }

// MakeNetworkPrivate sets the connected Public networks to Private (with the
// admin prompt), so the firewall rules apply.
func (a *App) MakeNetworkPrivate() error { return makeNetworkPrivate() }

// OpenSunshineSetup starts Sunshine here if needed and opens its web page,
// where its username and password are set.
func (a *App) OpenSunshineSetup() error {
	if err := a.node.StartLocalSunshine(); err != nil {
		return err
	}
	wruntime.BrowserOpenURL(a.ctx, a.node.HostInfo().SunshineURL)
	return nil
}

// AllowSunshineFirewall opens this computer's firewall (ufw) for Sunshine,
// so other computers can control it; asks for the password in a window.
func (a *App) AllowSunshineFirewall() error {
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	return control.AllowSunshineFirewall(ctx)
}

// ClearHistory removes the messages with a device, or all messages if
// peerID is empty. Received files stay on disk.
func (a *App) ClearHistory(peerID string) error { return a.node.ClearHistory(peerID) }

// Language is the language chosen in the settings ("en", "ar", "tr",
// "fr"), or "system" to follow the system.
func (a *App) Language() string {
	if l := a.node.Language(); validLanguage(l) {
		return l
	}
	return "system"
}

// SetLanguage saves the language ("system" or a language code). The tray
// menu changes right away; the window switches by itself.
func (a *App) SetLanguage(lang string) error {
	if lang == "system" {
		lang = ""
	} else if !validLanguage(lang) {
		return fmt.Errorf("unknown language %q", lang)
	}
	if err := a.node.SetLanguage(lang); err != nil {
		return err
	}
	a.relabelTray()
	return nil
}

// StartPhonePairing makes a QR code for pairing a phone.
func (a *App) StartPhonePairing() node.PhonePairing { return a.node.StartPhonePairing() }
