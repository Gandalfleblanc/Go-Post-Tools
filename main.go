package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"go-post-tools/internal/binutil"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed binaries
var binariesFS embed.FS

func init() {
	binutil.Init(binariesFS)
}

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:            "Go Post Tools",
		Width:            1600,
		Height:           1000,
		WindowStartState: options.Maximised,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
		Mac: &mac.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Go Post Tools",
				Message: "By GANDALF",
			},
		},
		EnableDefaultContextMenu: true,
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
