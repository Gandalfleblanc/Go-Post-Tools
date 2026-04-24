package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"golang.org/x/crypto/bcrypt"

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
	// CLI bootstrap helper : génère un hash bcrypt pour un mot de passe.
	// Usage : ./Go-Post-Tools --hash-password "monmotdepasse"
	// Sert à seeder team.json avant le premier login (chicken-and-egg).
	if len(os.Args) >= 3 && os.Args[1] == "--hash-password" {
		pw := os.Args[2]
		if pw == "" {
			fmt.Fprintln(os.Stderr, "mot de passe vide")
			os.Exit(2)
		}
		h, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
		if err != nil {
			fmt.Fprintln(os.Stderr, "erreur:", err)
			os.Exit(1)
		}
		fmt.Println(string(h))
		os.Exit(0)
	}

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
