package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"dsync/internal/control"
	"dsync/internal/update"
	"dsync/internal/version"
)

// UpdateInfo is what the settings show about updates.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	URL       string `json:"url,omitempty"` // release page
	Notes     string `json:"notes,omitempty"`
	// CanInstall means the app can install it itself; otherwise the user
	// downloads it from the release page.
	CanInstall bool   `json:"canInstall"`
	Error      string `json:"error,omitempty"`
}

type updateState struct {
	mu     sync.Mutex
	latest *update.Release
	busy   bool
}

// CheckUpdate asks GitHub for the newest release.
func (a *App) CheckUpdate() UpdateInfo {
	info := UpdateInfo{Current: version.Version}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	r, err := update.Latest(ctx)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	a.upd.mu.Lock()
	a.upd.latest = &r
	a.upd.mu.Unlock()
	info.Latest, info.URL, info.Notes = r.Version, r.URL, r.Notes
	info.Available = update.Newer(r.Version, version.Version)
	info.CanInstall = update.AssetName(update.Kind(), r.Version) != ""
	return info
}

// autoCheckUpdates looks for updates shortly after startup and then twice
// a day, and tells the UI when one is available.
func (a *App) autoCheckUpdates(ctx context.Context) {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if info := a.CheckUpdate(); info.Available {
			wruntime.EventsEmit(a.ctx, "update:available", info)
		}
		timer.Reset(12 * time.Hour)
	}
}

// UpdateProgress is sent as "update:progress" while updating.
type UpdateProgress struct {
	Step  string `json:"step"` // downloading, installing, restarting
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// InstallUpdate downloads the newest release, checks it, installs it and
// restarts dsync.
func (a *App) InstallUpdate() error {
	a.upd.mu.Lock()
	r := a.upd.latest
	if a.upd.busy {
		a.upd.mu.Unlock()
		return nil
	}
	a.upd.busy = true
	a.upd.mu.Unlock()
	defer func() {
		a.upd.mu.Lock()
		a.upd.busy = false
		a.upd.mu.Unlock()
	}()
	if r == nil {
		info := a.CheckUpdate()
		if info.Error != "" {
			return errString(info.Error)
		}
		a.upd.mu.Lock()
		r = a.upd.latest
		a.upd.mu.Unlock()
	}

	kind := update.Kind()
	name := update.AssetName(kind, r.Version)
	if name == "" {
		wruntime.BrowserOpenURL(a.ctx, r.URL)
		return nil
	}
	progress := func(step string, done, total int64) {
		wruntime.EventsEmit(a.ctx, "update:progress", UpdateProgress{Step: step, Done: done, Total: total})
	}
	dir := filepath.Join(os.TempDir(), "dsync-update")
	file, err := update.Download(a.ctx, *r, name, dir, func(d, t int64) { progress("downloading", d, t) })
	if err != nil {
		return err
	}
	progress("installing", 0, 0)
	if err := update.Install(a.ctx, kind, file); err != nil {
		return err
	}
	progress("restarting", 0, 0)
	if err := update.Restart(); err != nil {
		return err
	}
	// Quit so the new version can start (and, on Windows, so the setup can
	// replace the files).
	go func() {
		time.Sleep(500 * time.Millisecond)
		a.quit()
	}()
	return nil
}

// InstallProgram installs Moonlight or Sunshine for remote control.
func (a *App) InstallProgram(program string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	defer cancel()
	return control.Install(ctx, program)
}

type errString string

func (e errString) Error() string { return string(e) }
