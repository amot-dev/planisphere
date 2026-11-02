package main

import (
	"embed"
	"fmt"
	"log"
	"math"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	linuxOptions "github.com/wailsapp/wails/v2/pkg/options/linux"
	macOptions "github.com/wailsapp/wails/v2/pkg/options/mac"
	windowsOptions "github.com/wailsapp/wails/v2/pkg/options/windows"

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

	// Ensure WebKitGTK hardware-accelerated compositing mode is enabled on Linux
	_ = os.Unsetenv("WEBKIT_DISABLE_COMPOSITING_MODE")
	if os.Getenv("WEBKIT_FORCE_COMPOSITING_MODE") == "" {
		_ = os.Setenv("WEBKIT_FORCE_COMPOSITING_MODE", "1")
	}

	// On Linux Wayland with NVIDIA drivers, WebKitGTK's DMABUF renderer fails
	// creating GBM buffers ("Failed to create GBM buffer ... Invalid argument"),
	// causing stutter and render pipeline stalls. Disabling DMABUF renderer falls back
	// to EGL/shared memory compositing which renders smoothly without buffer allocation failures.
	if _, err := os.Stat("/proc/driver/nvidia"); err == nil {
		if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
			_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
		}
	}

	// Implement OS-specific WebView hardware acceleration flags for Windows/WebView2.
	// Microsoft WebView2 respects WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS when launching
	// its renderer and compositor processes.
	wv2Flags := []string{
		"--force-gpu-rasterization",
		"--ignore-gpu-blocklist",
		"--enable-gpu-rasterization",
		"--enable-zero-copy",
	}
	existingWv2Args := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
	if existingWv2Args == "" {
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", strings.Join(wv2Flags, " "))
	} else {
		for _, flag := range wv2Flags {
			if !strings.Contains(existingWv2Args, flag) {
				existingWv2Args += " " + flag
			}
		}
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", strings.TrimSpace(existingWv2Args))
	}

	application := app.New()

	runError := wails.Run(&options.App{
		Title:     fmt.Sprintf("%s — %s", version.ProductName, version.Tagline),
		Width:     1600,
		Height:    960,
		MaxWidth:  math.MaxInt16,
		MaxHeight: math.MaxInt16,
		// Explicitly disable frameless mode and window translucency effects
		// Frameless: false ensures standard window manager borders without requiring
		// custom client-side window shadow/decoration compositing.
		Frameless: false,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: options.NewRGB(7, 9, 13),
		OnStartup:        app.StartupHook(application),
		OnShutdown:       app.ShutdownHook(application),
		Bind: []interface{}{
			application,
		},
		Linux: &linuxOptions.Options{
			// Note: In Wails v2.17 C bindings (window.c), gpuPolicy 0 maps to
			// WEBKIT_HARDWARE_ACCELERATION_POLICY_ALWAYS, whereas gpuPolicy 1 maps to
			// the deprecated WEBKIT_HARDWARE_ACCELERATION_POLICY_ON_DEMAND. Setting OnDemand
			// here passes 0, invoking WEBKIT_HARDWARE_ACCELERATION_POLICY_ALWAYS directly
			// without triggering the WebKitGTK deprecation warning on modern WebKit 2.40+.
			WebviewGpuPolicy:    linuxOptions.WebviewGpuPolicyOnDemand,
			WindowIsTranslucent: false,
			ProgramName:         version.BinaryName,
		},
		Windows: &windowsOptions.Options{
			// Keep WebView2 GPU acceleration active and disable Mica/Acrylic translucent backdrops
			WebviewGpuIsDisabled:              false,
			WindowIsTranslucent:               false,
			WebviewIsTransparent:              false,
			BackdropType:                      windowsOptions.None,
			DisableFramelessWindowDecorations: false,
		},
		Mac: &macOptions.Options{
			// Ensure macOS does not enable translucent window blending
			WindowIsTranslucent:  false,
			WebviewIsTransparent: false,
		},
	})
	if runError != nil {
		log.Fatal(runError)
	}
}
