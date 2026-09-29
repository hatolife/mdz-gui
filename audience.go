package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// AudienceWindow は発表者とは別のネイティブウィンドウを管理します。
type AudienceWindow struct {
	ctx      context.Context
	endpoint string
	client   *http.Client
}
type audienceFiles struct{ fs.FS }

func (a audienceFiles) Open(name string) (fs.File, error) {
	if name == "index.html" {
		name = "audience.html"
	}
	return a.FS.Open(name)
}
func runAudience(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || len(strings.Trim(u.Path, "/")) != 64 || u.RawQuery != "" || u.User != nil {
		return fmt.Errorf("投影用の接続先が不正です")
	}
	root, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	a := &AudienceWindow{endpoint: endpoint, client: &http.Client{Timeout: 3 * time.Second}}
	return wails.Run(&options.App{Title: "mdz-gui — 投影用（F: 全画面 / Esc / 右クリック: 発表終了）", Width: 960, Height: 600, MinWidth: 320, MinHeight: 240,
		AssetServer: &assetserver.Options{Assets: audienceFiles{root}, Handler: http.HandlerFunc(a.serveAsset)},
		Windows:     &windows.Options{WebviewUserDataPath: filepath.Join(cache, "mdz-gui", "WebView2-Audience")},
		OnStartup:   func(ctx context.Context) { a.ctx = ctx }, OnShutdown: func(context.Context) { _ = a.Control("closed", 0) }, Bind: []interface{}{a}})
}

// Poll は初回だけ本文も取得し、それ以外はページ番号などの状態だけを同期します。
func (a *AudienceWindow) Poll(withSlides bool) (PresentationState, error) {
	resource := "state"
	if withSlides {
		resource += "?deck=1"
	}
	r, err := a.client.Get(a.endpoint + resource)
	if err != nil {
		return PresentationState{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return PresentationState{}, fmt.Errorf("発表者側との接続が終了しました")
	}
	var state PresentationState
	err = json.NewDecoder(io.LimitReader(r.Body, 160<<20)).Decode(&state)
	return state, err
}
func (a *AudienceWindow) Control(command string, index int) error {
	data, _ := json.Marshal(map[string]any{"command": command, "index": index})
	r, err := a.client.Post(a.endpoint+"command", "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 204 {
		return fmt.Errorf("発表操作に失敗しました")
	}
	return nil
}
func (a *AudienceWindow) Close() { runtime.Quit(a.ctx) }
func (a *AudienceWindow) Fullscreen(active bool) {
	if active {
		runtime.WindowFullscreen(a.ctx)
	} else {
		runtime.WindowUnfullscreen(a.ctx)
	}
}
func (a *AudienceWindow) serveAsset(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/bundle/") {
		http.NotFound(w, r)
		return
	}
	response, err := a.client.Get(a.endpoint + strings.TrimPrefix(r.URL.EscapedPath(), "/"))
	if err != nil {
		http.Error(w, "画像を読み込めません", 503)
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.WriteHeader(response.StatusCode)
	io.Copy(w, io.LimitReader(response.Body, 32<<20))
}
