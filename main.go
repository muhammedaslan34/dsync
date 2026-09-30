// The dsync desktop app. Build with `wails build`; the command-line tool
// lives in cmd/dsync.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"dsync/internal/config"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	app, err := NewApp(cfg)
	if err != nil {
		log.Fatal(err)
	}

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
		Bind:             []any{app},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
