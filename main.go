package main

import (
	"embed"
	"fmt"
	"log"
	"math"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	linuxOptions "github.com/wailsapp/wails/v2/pkg/options/linux"

	"planisphere/internal/app"
	"planisphere/internal/version"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Work around a Wails v2 GTK3 bug on Wayland/Hyprland: Wails's C helper
	// onWayland() checks XDG_SESSION_TYPE=="wayland" during NewWindow before the
	// GtkWindow is realized, caching decoratorWidth = 1 - Width (-1599) and
	// clamping GDK_HINT_MAX_SIZE so the WebView cannot expand on resize/fullscreen.
	// GTK3 itself uses WAYLAND_DISPLAY for native Wayland, so unsetting
	// XDG_SESSION_TYPE keeps native Wayland while bypassing the broken geometry clamp.
	_ = os.Unsetenv("XDG_SESSION_TYPE")

	application := app.New()

	runError := wails.Run(&options.App{
		Title:     fmt.Sprintf("%s — %s", version.ProductName, version.Tagline),
		Width:     1600,
		Height:    960,
		MaxWidth:  math.MaxInt16,
		MaxHeight: math.MaxInt16,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 7, G: 9, B: 13, A: 1},
		OnStartup:        app.StartupHook(application),
		OnShutdown:       app.ShutdownHook(application),
		Bind: []interface{}{
			application,
		},
		Linux: &linuxOptions.Options{
			WebviewGpuPolicy: linuxOptions.WebviewGpuPolicyNever,
			ProgramName:      version.BinaryName,
		},
	})
	if runError != nil {
		log.Fatal(runError)
	}
}


