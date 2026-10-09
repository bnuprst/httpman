//go:build !nogui

package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/bnuprst/httpman/internal/app"
	"github.com/bnuprst/httpman/internal/workspace"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// wailsUI adapts the Wails runtime to app.UI.
type wailsUI struct{ ctx context.Context }

func (u *wailsUI) Emit(event string, data any) { wruntime.EventsEmit(u.ctx, event, data) }

func filters(fs []app.FileFilter) []wruntime.FileFilter {
	out := make([]wruntime.FileFilter, len(fs))
	for i, f := range fs {
		out[i] = wruntime.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern}
	}
	return out
}

func (u *wailsUI) OpenFiles(title string, fs []app.FileFilter) ([]string, error) {
	return wruntime.OpenMultipleFilesDialog(u.ctx, wruntime.OpenDialogOptions{Title: title, Filters: filters(fs)})
}

func (u *wailsUI) SaveFile(title, name string, fs []app.FileFilter) (string, error) {
	return wruntime.SaveFileDialog(u.ctx, wruntime.SaveDialogOptions{Title: title, DefaultFilename: name, Filters: filters(fs)})
}

func runGUI(args []string) error {
	dir := workspace.DefaultDir()
	for i := 0; i < len(args); i++ {
		if args[i] == "--workspace" && i+1 < len(args) {
			dir = args[i+1]
			i++
		} else {
			return fmt.Errorf("unknown argument %q (see httpman help)", args[i])
		}
	}
	ws, err := workspace.Open(dir)
	if err != nil {
		return fmt.Errorf("opening workspace %s: %w", dir, err)
	}
	a := app.New(ws, version)
	// Start maximised so the header is never off screen on small displays.
	// The restored size fits 1366x768 at 100% scaling; the minimums also fit
	// it at 125% (~1090x580 usable).
	return wails.Run(&options.App{
		Title:            "httpman",
		WindowStartState: options.Maximised,
		Width:            1280,
		Height:           720,
		MinWidth:         800,
		MinHeight:        480,
		BackgroundColour: &options.RGBA{R: 30, G: 30, B: 30, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup: func(ctx context.Context) {
			a.SetUI(&wailsUI{ctx: ctx})
			wruntime.OnFileDrop(ctx, func(_, _ int, paths []string) {
				res := a.ImportPaths(paths)
				wruntime.EventsEmit(ctx, "import", res)
			})
		},
		OnShutdown: func(context.Context) { a.Shutdown() },
		Bind:       []interface{}{a},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Windows: &windows.Options{
			WebviewUserDataPath: dataDir(dir),
			Theme:               windows.SystemDefault,
		},
		Mac: &mac.Options{
			About: &mac.AboutInfo{Title: "httpman", Message: "A local HTTP client compatible with Postman collections.\nVersion " + version, Icon: icon},
		},
		Linux: &linux.Options{Icon: icon, ProgramName: "httpman"},
	})
}

func dataDir(ws string) string {
	d := ws + string(os.PathSeparator) + ".webview"
	_ = os.MkdirAll(d, 0o755)
	return d
}
