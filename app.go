package main

import (
	"context"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"dsync/internal/config"
	"dsync/internal/discovery"
	"dsync/internal/node"
	"dsync/internal/proto"
)

// App exposes the node to the frontend. Its exported methods are callable
// from JavaScript, and node events are forwarded as Wails events.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc
	node   *node.Node

	mu        sync.Mutex
	statusErr string
}

func NewApp(cfg *config.Config) *App {
	a := &App{}
	a.node = node.New(cfg, a.emit)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go func() {
		if err := a.node.Run(runCtx); err != nil {
			a.mu.Lock()
			a.statusErr = err.Error()
			a.mu.Unlock()
			wruntime.EventsEmit(ctx, "error", err.Error())
		}
	}()
}

func (a *App) shutdown(context.Context) {
	a.cancel()
}

func (a *App) emit(event string, data any) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, event, data)
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
	paths, err := wruntime.OpenMultipleFilesDialog(a.ctx, wruntime.OpenDialogOptions{Title: "Send files"})
	if err != nil || len(paths) == 0 {
		return err
	}
	return a.node.SendFiles(a.ctx, peerID, paths)
}

// SendPaths sends files dropped onto the window.
func (a *App) SendPaths(peerID string, paths []string) error {
	return a.node.SendFiles(a.ctx, peerID, paths)
}

func (a *App) CancelTransfer(id int64) { a.node.CancelTransfer(id) }

func (a *App) ReceiveDir() string { return a.node.ReceiveDir() }

// ChooseReceiveDir lets the user pick where received files are saved.
func (a *App) ChooseReceiveDir() (string, error) {
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:            "Save received files in",
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
