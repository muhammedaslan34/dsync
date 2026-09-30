// The dsync desktop app. Build with `wails build`; the command-line tool
// lives in cmd/dsync.
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"log"
	"os"
	"slices"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"dsync/internal/config"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	app, err := NewApp(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// --hidden (used when starting at login) starts in the tray, but only if
	// there is a tray to get the window back from.
	startHidden := slices.Contains(os.Args[1:], "--hidden") && trayAvailable()
	app.tray.hidden = startHidden

	err = wails.Run(&options.App{
		Title:            "dsync",
		Width:            960,
		Height:           640,
		MinWidth:         640,
		MinHeight:        420,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: app.imageHandler()},
		BackgroundColour: &options.RGBA{R: 24, G: 24, B: 27, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		// Window icon, and the name GNOME matches to the app menu entry.
		Linux:       &linux.Options{Icon: appIcon, ProgramName: "dsync-gui"},
		StartHidden: startHidden,
		// Opening dsync again shows the running one instead of a second copy.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               instanceID(cfg.Dir()),
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.showWindow() },
		},
		Bind: []any{app},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// instanceID is unique per settings folder, so test copies with their own
// DSYNC_CONFIG_DIR can run side by side.
func instanceID(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return "dsync-" + hex.EncodeToString(sum[:6])
}
