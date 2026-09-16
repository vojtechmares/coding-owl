//go:build darwin

// Command owl-desktop is the desktop app: a Wails window over the shared
// client (ADR-0009). It is a build target of its own, apart from the owl
// binary, which ADR-0001 keeps to the daemon, the CLI and any future Runner.
//
// The Go side of the app is internal/desktop; this file only wires it to
// Wails: the embedded frontend, the window, and the event runtime.
package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/vojtechmares/coding-owl/internal/desktop"
	"github.com/vojtechmares/coding-owl/internal/version"
	"github.com/vojtechmares/coding-owl/internal/xdg"
)

//go:embed all:frontend/dist
var assets embed.FS

// emitter sends the app's events through the Wails runtime once the window
// is up. Before that there is nobody to tell, and nothing is following a log
// yet either.
type emitter struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (e *emitter) ready(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ctx = ctx
}

func (e *emitter) Emit(name string, data any) {
	e.mu.RLock()
	ctx := e.ctx
	e.mu.RUnlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, name, data)
	}
}

func main() {
	paths, err := xdg.Resolve()
	if err != nil {
		fmt.Fprintln(os.Stderr, "owl-desktop:", err)
		os.Exit(1)
	}
	em := &emitter{}
	app := desktop.New(paths.SocketPath, em)

	err = wails.Run(&options.App{
		Title:     "Coding Owl",
		Width:     1200,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// The ground the app paints on, for the instant before the frontend
		// paints it itself. Keep it the same as --color-ground in
		// frontend/src/theme/tokens.css, or the window flashes on open.
		BackgroundColour: &options.RGBA{R: 250, G: 250, B: 250, A: 1},
		OnStartup:        em.ready,
		OnShutdown:       func(context.Context) { app.Shutdown() },
		Bind:             []any{app},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			// Light, and opaque: the theme is a flat ground with hairline
			// rules, so there is nothing for translucency to do but muddy it.
			Appearance:           mac.NSAppearanceNameAqua,
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Coding Owl " + version.Version,
				Message: "Runs coding agents on your machine while it is otherwise idle.",
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "owl-desktop:", err)
		os.Exit(1)
	}
}
