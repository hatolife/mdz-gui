package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/hatolife/mdz-gui/internal/settings"
)

//go:embed frontend/dist
var assets embed.FS

func init() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lshortfile)
}

func main() {
	prepareCLIOutput(os.Args[1:])
	args := ParseArgs()
	if args.Version {
		fmt.Println(version)
		return
	}
	if err := run(args); err != nil {
		log.Fatal(err)
	}
}

func run(args Args) error {
	if args.Audience != "" {
		return runAudience(args.Audience)
	}
	root, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	base := filepath.Join(cache, "mdz-gui")
	cfg, err := settings.Load(filepath.Join(base, "settings.json"))
	if err != nil {
		return fmt.Errorf("設定を読み込めません: %w", err)
	}
	a := &App{base: base, cfg: cfg, initial: args.File}
	return wails.Run(&options.App{
		Frameless: true,
		Title:     "mdz-gui " + version, Width: 1280, Height: 860, MinWidth: 800, MinHeight: 520,
		AssetServer:   &assetserver.Options{Assets: root, Handler: http.HandlerFunc(a.serveAsset)},
		Windows:       &windows.Options{WebviewUserDataPath: filepath.Join(base, "WebView2"), Theme: windows.SystemDefault},
		DragAndDrop:   &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		OnStartup:     func(ctx context.Context) { a.ctx = ctx },
		OnBeforeClose: func(ctx context.Context) bool { return a.beforeClose() },
		OnShutdown:    func(ctx context.Context) { a.shutdown() },
		Bind:          []interface{}{a},
	})
}
