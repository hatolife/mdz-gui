package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/editor"
	"github.com/hatolife/mdz-gui/internal/settings"
	"github.com/hatolife/mdz-gui/internal/workspace"
)

// App は現在の作業文書とエディターを直列化して管理します。
type App struct {
	presentation   *presentation
	launchAudience func(string) error
	launchWindow   func(string) error
	slideUndo      [][]byte
	slideRedo      [][]byte
	slideRevision  string

	book        *bookProcess
	bookTrusted bool
	tocUndo     [][]byte
	tocRedo     [][]byte
	tocRevision string
	ctx         context.Context
	mu          sync.Mutex
	session     *workspace.Session
	native      *editor.Native
	inputNative atomic.Pointer[editor.Native]
	cfg         settings.Settings
	base        string
	initial     string
	nativeError string
	notify      func(string, any)
	pending     atomic.Bool
	lastAuto    time.Time

	unsavedMu     sync.Mutex
	unsavedReply  chan string
	closing       atomic.Bool
	closeApproved atomic.Bool
}

type Snapshot struct {
	Filename       string   `json:"filename"`
	Entry          string   `json:"entry"`
	Pages          []string `json:"pages"`
	Assets         []string `json:"assets"`
	Mode           string   `json:"mode"`
	SingleMarkdown bool     `json:"singleMarkdown"`
	Dirty          bool     `json:"dirty"`
	ID             string   `json:"id"`
	WorkDir        string   `json:"workDir"`
	Engine         string   `json:"engine"`
	NativeError    string   `json:"nativeError"`
}

type NativeState struct {
	Name    string `json:"name"`
	Text    string `json:"text"`
	Changed bool   `json:"changed"`
	CanUndo bool   `json:"canUndo"`
	CanRedo bool   `json:"canRedo"`
}

func (a *App) emit(event string, data any) {
	if event == "native-changed" {
		a.pending.Store(true)
	}
	if a.notify != nil {
		a.notify(event, data)
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data)
	}
}
func (a *App) Initial() string { return a.initial }
func (a *App) LastDocument() string {
	b, err := os.ReadFile(filepath.Join(a.base, "last-document"))
	if err != nil || len(b) == 0 {
		return ""
	}
	filename := string(b)
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	if !bundle.IsMarkdown(filename) && !strings.EqualFold(filepath.Ext(filename), ".mdz") {
		return ""
	}
	return filename
}
func (a *App) rememberLastDocument(filename string) {
	if filename == "" {
		return
	}
	if err := workspace.AtomicWrite(filepath.Join(a.base, "last-document"), []byte(filename)); err != nil {
		a.emit("app-error", "最後に開いた文書を記録できません: "+err.Error())
	}
}
func (a *App) Version() string { return version }

func (a *App) singleMarkdownLocked() bool {
	return a.session != nil && bundle.IsMarkdown(a.session.Filename)
}

func (a *App) State() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return Snapshot{Pages: []string{}, Assets: []string{}, Engine: "builtin"}
	}
	s := Snapshot{Filename: a.session.Filename, Entry: a.session.Doc.Entry, Pages: a.session.Doc.Pages(), Assets: []string{}, Mode: a.session.Doc.Mode, SingleMarkdown: a.singleMarkdownLocked(), Dirty: a.session.Dirty || a.pending.Load(), ID: a.session.ID, WorkDir: a.session.Content(), Engine: "builtin", NativeError: a.nativeError}
	if a.native != nil {
		s.Engine = "neovim"
	}
	for name := range a.session.Doc.Files {
		if !bundle.IsMarkdown(name) && name != "manifest.json" {
			s.Assets = append(s.Assets, name)
		}
	}
	sort.Strings(s.Assets)
	return s
}

// MarkDirty は入力時点で閉じる確認を有効にします。
func (a *App) MarkDirty() { a.pending.Store(true) }
func (a *App) discardAllowed() bool {
	a.mu.Lock()
	if a.session == nil {
		a.mu.Unlock()
		return true
	}
	err := a.syncNativeLocked()
	dirty := a.session.Dirty || a.pending.Load()
	a.mu.Unlock()
	if err != nil {
		a.emit("app-error", err.Error())
		return false
	}
	if !dirty {
		return true
	}
	a.unsavedMu.Lock()
	if a.unsavedReply != nil {
		a.unsavedMu.Unlock()
		return false
	}
	reply := make(chan string, 1)
	a.unsavedReply = reply
	a.unsavedMu.Unlock()
	defer func() {
		a.unsavedMu.Lock()
		a.unsavedReply = nil
		a.unsavedMu.Unlock()
	}()
	a.emit("confirm-unsaved", nil)
	switch <-reply {
	case "discard":
		return true
	case "save":
		saved, err := a.Save(false)
		if err != nil {
			a.emit("app-error", err.Error())
		}
		return err == nil && saved
	default:
		return false
	}
}

// ResolveUnsaved は確認画面の選択を待機中の処理へ渡します。
func (a *App) ResolveUnsaved(choice string) {
	a.unsavedMu.Lock()
	defer a.unsavedMu.Unlock()
	if a.unsavedReply != nil {
		select {
		case a.unsavedReply <- choice:
		default:
		}
	}
}

// beforeClose は画面の応答を止めずに保存・破棄を確認します。
func (a *App) beforeClose() bool {
	if a.closeApproved.Load() {
		return false
	}
	if a.closing.CompareAndSwap(false, true) {
		go func() {
			defer a.closing.Store(false)
			if a.discardAllowed() {
				a.closeApproved.Store(true)
				runtime.Quit(a.ctx)
			}
		}()
	}
	return true
}

func (a *App) shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopPresentationLocked()
	if a.native != nil {
		a.inputNative.Store(nil)
		a.native.Close()
	}
	a.stopBookLocked()
	if a.session == nil {
		return
	}
	if err := a.session.Close(); err != nil {
		a.emit("app-error", err.Error())
	}
}
func (a *App) adoptLocked(s *workspace.Session) {
	a.stopPresentationLocked()
	a.slideUndo, a.slideRedo, a.slideRevision = nil, nil, ""
	if a.native != nil {
		a.inputNative.Store(nil)
		a.native.Close()
		a.native = nil
	}
	a.stopBookLocked()
	a.bookTrusted = false
	a.tocRevision = ""
	a.tocUndo = nil
	a.tocRedo = nil
	old := a.session
	a.session = s
	a.bookTrusted = a.rememberedBookLocked()
	a.pending.Store(false)
	a.nativeError = ""
	a.lastAuto = time.Now()
	if old != nil {
		if err := old.Close(); err != nil {
			a.emit("app-error", err.Error())
		}
	}
}
func (a *App) New() (bool, error) { return a.NewDocument("document") }
func (a *App) NewDocument(kind string) (bool, error) {
	if !a.discardAllowed() {
		return false, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	d, err := newDocument(kind)
	if err != nil {
		return false, err
	}
	s, err := workspace.New(a.base, d, "", true)
	if err != nil {
		return false, err
	}
	a.adoptLocked(s)
	return true, nil
}
func (a *App) Open(filename string) (bool, error) {
	if !a.discardAllowed() {
		return false, nil
	}
	if filename == "" {
		var err error
		filename, err = runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "文書を開く", Filters: []runtime.FileFilter{{DisplayName: "MDZip / Markdown", Pattern: "*.mdz;*.md;*.markdown"}}})
		if err != nil {
			return false, err
		}
		if filename == "" {
			return false, nil
		}
	}
	filename, err := filepath.Abs(filename)
	if err != nil {
		return false, err
	}
	var d *bundle.Document
	isMD := bundle.IsMarkdown(filename)
	if isMD {
		var b []byte
		b, err = readLimited(filename)
		if err == nil {
			name := filepath.Base(filename)
			if !workspace.PortablePath(name) {
				name = "document.md"
			}
			d = bundle.New()
			d.Files = map[string][]byte{}
			d.Entry = name
			err = d.Put(name, b)
		}
	} else if strings.EqualFold(filepath.Ext(filename), ".mdz") {
		d, err = bundle.Read(filename)
	} else {
		return false, fmt.Errorf("MDZまたはMarkdownを指定してください")
	}
	if err != nil {
		return false, err
	}
	a.mu.Lock()
	s, err := workspace.New(a.base, d, filename, false)
	if err != nil {
		a.mu.Unlock()
		return false, err
	}
	a.adoptLocked(s)
	filename = a.session.Filename
	a.mu.Unlock()
	a.rememberLastDocument(filename)
	return true, nil
}
// OpenInNewWindow はMDZまたはMarkdownを別のmdz-guiプロセスで開きます。
func (a *App) OpenInNewWindow(filename string) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return fmt.Errorf("開くMDZまたはMarkdownファイルを指定してください")
	}
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(absolute))
	if !bundle.IsMarkdown(absolute) && ext != ".mdz" {
		return fmt.Errorf("MDZまたはMarkdownファイルを指定してください")
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("通常のMDZまたはMarkdownファイルを指定してください")
	}
	if a.launchWindow != nil {
		return a.launchWindow(absolute)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("mdz-guiの実行ファイルを取得できません: %w", err)
	}
	command := exec.Command(executable, absolute)
	if err := command.Start(); err != nil {
		return fmt.Errorf("新しいmdz-guiウィンドウを開けません: %w", err)
	}
	go func() {
		// 子プロセスの終了を回収しつつ、現在のウィンドウは待機しません。
		_ = command.Wait()
	}()
	return nil
}

func (a *App) Text(name string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.session.Doc.Files[name]
	if !ok || !bundle.IsMarkdown(name) {
		return "", fmt.Errorf("本文がありません")
	}
	return string(b), nil
}
func (a *App) Update(name, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.native != nil {
		return fmt.Errorf("Neovimの編集中に別エディターから更新できません")
	}
	if !bundle.IsMarkdown(name) {
		return fmt.Errorf("Markdownファイルを指定してください")
	}
	if err := a.session.Put(name, []byte(text)); err != nil {
		return err
	}
	a.pending.Store(false)
	return nil
}
func (a *App) AddPage(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return fmt.Errorf("文書を開いてください")
	}
	if a.singleMarkdownLocked() {
		return fmt.Errorf("単一Markdownではページを追加できません。MDZとして保存してから追加してください")
	}
	if a.session.Doc.Mode == "slides" {
		return fmt.Errorf("スライドを追加してください")
	}
	// 拡張子を省略したページ名には.mdを補います。
	name = strings.TrimSpace(name)
	if name != "" && path.Ext(name) == "" && !strings.HasSuffix(name, "/") {
		name += ".md"
	}
	if !bundle.IsMarkdown(name) {
		return fmt.Errorf("拡張子を.mdにしてください")
	}
	if err := a.syncNativeLocked(); err != nil {
		return err
	}
	if err := a.session.Capture(); err != nil {
		return err
	}
	for p := range a.session.Doc.Files {
		if strings.EqualFold(p, name) {
			return fmt.Errorf("同名のファイルがあります")
		}
	}
	order := a.session.Doc.Pages()
	if err := a.session.Put(name, []byte("# 新しいページ\n")); err != nil {
		return err
	}
	return a.session.SetPageOrder(append(order, name))
}

func (a *App) Save(saveAs bool) (bool, error) {
	a.mu.Lock()
	filename := a.session.Filename
	id := a.session.ID
	a.mu.Unlock()
	if saveAs || filename == "" {
		defaultName := "document.mdz"
		if filename != "" {
			stem := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
			if stem != "" {
				defaultName = stem + ".mdz"
			}
		}
		var err error
		filename, err = runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "MDZとして保存", DefaultFilename: defaultName, Filters: []runtime.FileFilter{{DisplayName: "MDZip", Pattern: "*.mdz"}}})
		if err != nil {
			return false, err
		}
		if filename == "" {
			return false, nil
		}
		if !strings.EqualFold(filepath.Ext(filename), ".mdz") {
			filename += ".mdz"
		}
	}
	a.mu.Lock()
	if a.session.ID != id {
		a.mu.Unlock()
		return false, fmt.Errorf("保存対象の文書が変更されました")
	}
	if err := a.saveLocked(filename); err != nil {
		a.mu.Unlock()
		return false, err
	}
	filename = a.session.Filename
	a.mu.Unlock()
	a.rememberLastDocument(filename)
	return true, nil
}
func (a *App) saveLocked(filename string) error {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(a.session.Root, abs)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
		return fmt.Errorf("作業領域の外へ保存してください")
	}
	if err := a.syncNativeLocked(); err != nil {
		return err
	}
	if bundle.IsMarkdown(abs) {
		if !a.singleMarkdownLocked() {
			return fmt.Errorf("通常のMDZはMarkdownファイルへ直接保存できません")
		}
		if err := a.session.SaveMarkdown(a.base, abs, a.cfg); err != nil {
			return err
		}
	} else {
		if err := a.session.Save(a.base, abs, version, a.cfg); err != nil {
			return err
		}
	}
	if a.bookTrusted {
		if err := a.rememberBookLocked(); err != nil {
			a.emit("app-error", "実行許可の記録に失敗しました: "+err.Error())
		}
	}
	a.pending.Store(false)
	a.lastAuto = time.Now()
	return nil
}

// AutoSave は保存先未指定の文書ではダイアログを開かず、作業データだけを保持します。
func (a *App) AutoSave() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil || !a.cfg.AutoSave || time.Since(a.lastAuto) < time.Duration(a.cfg.AutoSaveSeconds)*time.Second {
		return false, nil
	}
	a.lastAuto = time.Now()
	if err := a.syncNativeLocked(); err != nil {
		return false, err
	}
	if !a.session.Dirty || a.session.Filename == "" {
		return false, nil
	}
	if err := a.saveLocked(a.session.Filename); err != nil {
		return false, err
	}
	return true, nil
}
func (a *App) Settings() settings.Settings { a.mu.Lock(); defer a.mu.Unlock(); return a.cfg }
func (a *App) Configure(cfg settings.Settings) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.syncNativeLocked(); err != nil {
		return err
	}
	restart := cfg.NvimPath != a.cfg.NvimPath || cfg.InitMode != a.cfg.InitMode || cfg.InitPath != a.cfg.InitPath
	if a.native != nil && !restart {
		if err := a.native.SetUndoLevels(cfg.UndoLevels); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(cfg, "", "\t")
	if err != nil {
		return err
	}
	if err := workspace.AtomicWrite(filepath.Join(a.base, "settings.json"), b); err != nil {
		return err
	}
	if cfg.MdbookPath != a.cfg.MdbookPath {
		a.stopBookLocked()
	}
	a.cfg = cfg
	if cfg.Editor == "builtin" {
		a.nativeError = ""
	}
	a.lastAuto = time.Now()
	if restart && a.native != nil {
		a.inputNative.Store(nil)
		a.native.Close()
		a.native = nil
	}
	return nil
}
func (a *App) ChooseExecutable() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "nvim.exeを指定", Filters: []runtime.FileFilter{{DisplayName: "実行ファイル", Pattern: "*.exe"}}})
}
func (a *App) ChooseInit() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "init.luaを指定", Filters: []runtime.FileFilter{{DisplayName: "Lua設定", Pattern: "*.lua"}}})
}
func (a *App) OpenDataFolder()                       { runtime.BrowserOpenURL(a.ctx, a.base) }
func (a *App) Recoveries() ([]workspace.Meta, error) { return workspace.Recoveries(a.base) }
func (a *App) Recover(id string) (bool, error) {
	if !a.discardAllowed() {
		return false, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, err := workspace.Recover(a.base, id)
	if err != nil {
		return false, err
	}
	a.adoptLocked(s)
	return true, nil
}

func (a *App) StartNative() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return false
	}
	if a.native != nil {
		return true
	}
	e, err := editor.Start(a.session.Content(), filepath.Join(a.session.Root, "undo"), a.cfg, a.emit)
	if err != nil {
		a.nativeError = "Neovimを起動できないため内蔵エディターを使用します。設定から実行ファイルを指定してください: " + err.Error()
		return false
	}
	a.native = e
	a.inputNative.Store(e)
	a.nativeError = ""
	return true
}
func (a *App) NativeOpen(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.native == nil {
		return fmt.Errorf("Neovimが起動していません")
	}
	if _, ok := a.session.Doc.Files[name]; !ok || !bundle.IsMarkdown(name) {
		return fmt.Errorf("文書内のMarkdownを指定してください")
	}
	return a.native.Open(name)
}
func (a *App) syncNativeLocked() error {
	if a.native == nil {
		return nil
	}
	if err := a.native.Flush(); err != nil {
		a.inputNative.Store(nil)
		a.native.Close()
		a.native = nil
		a.nativeError = "Neovimが停止したため、最後に書き込まれた作業ファイルを内蔵エディターで開きます。"
		_ = a.session.Capture()
		return fmt.Errorf("Neovimの作業内容を保存できません: %w", err)
	}
	if err := a.session.Capture(); err != nil {
		return err
	}
	a.pending.Store(false)
	return a.session.Checkpoint()
}
func (a *App) NativePoll() (NativeState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.native == nil {
		return NativeState{}, nil
	}
	if !a.native.Changed() {
		return NativeState{}, nil
	}
	if err := a.syncNativeLocked(); err != nil {
		return NativeState{}, err
	}
	c, err := a.native.Current()
	if err != nil {
		return NativeState{}, err
	}
	history, err := a.native.UndoState()
	if err != nil {
		return NativeState{}, err
	}
	return NativeState{Name: c.Name, Text: c.Text, Changed: true, CanUndo: history.CanUndo, CanRedo: history.CanRedo}, nil
}
func (a *App) NativeInput(keys string) error {
	e := a.inputNative.Load()
	if e == nil {
		return fmt.Errorf("Neovimが起動していません")
	}
	return e.Input(keys)
}
func (a *App) NativePaste(text string) error {
	e := a.inputNative.Load()
	if e == nil {
		return fmt.Errorf("Neovimが起動していません")
	}
	if len(text) > bundle.MaxFile {
		return fmt.Errorf("貼り付け内容が大きすぎます")
	}
	return e.Paste(text)
}
func (a *App) NativeUndo(redo bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.native == nil {
		return fmt.Errorf("Neovimが起動していません")
	}
	return a.native.Undo(redo)
}
func (a *App) NativeResize(cols, rows int) error {
	e := a.inputNative.Load()
	if e == nil {
		return nil
	}
	return e.Resize(cols, rows)
}
func (a *App) NativeScroll(ratio float64) error {
	e := a.inputNative.Load()
	if e == nil {
		return nil
	}
	return e.Scroll(ratio)
}
func (a *App) NativeMouse(button, action, modifier string, row, col int) error {
	e := a.inputNative.Load()
	if e == nil {
		return nil
	}
	return e.Client.InputMouse(button, action, modifier, 0, row, col)
}

// StoreImage は画像を実ファイルとして保持し、Undo時にも削除しません。
func (a *App) StoreImage(encoded string) (string, error) {
	if len(encoded) > bundle.MaxFile*4/3+4 {
		return "", fmt.Errorf("画像が大きすぎます")
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	info, kind, err := image.DecodeConfig(strings.NewReader(string(b)))
	if err != nil {
		return "", fmt.Errorf("画像を読み込めません: %w", err)
	}
	if info.Width <= 0 || info.Height <= 0 || int64(info.Width)*int64(info.Height) > 100_000_000 {
		return "", fmt.Errorf("画像の画素数が上限を超えています")
	}
	if kind == "jpeg" {
		kind = "jpg"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.storeImageLocked(b, "."+kind)
}
func (a *App) AddImage() (string, error) {
	filename, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "画像を追加", Filters: []runtime.FileFilter{{DisplayName: "画像", Pattern: "*.png;*.jpg;*.jpeg;*.gif;*.webp;*.svg"}}})
	if err != nil || filename == "" {
		return "", err
	}
	return a.ImportImage(filename)
}

// ImportImage はドロップされた画像も同じ検証を通して取り込みます。
func (a *App) ImportImage(filename string) (string, error) {
	b, err := readLimited(filename)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
	default:
		return "", fmt.Errorf("未対応の画像形式です")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return "", fmt.Errorf("文書を開いてください")
	}
	return a.storeImageLocked(b, ext)
}
func (a *App) storeImageLocked(b []byte, ext string) (string, error) {
	if a.singleMarkdownLocked() {
		return "", fmt.Errorf("単一Markdownでは画像を同梱できません。MDZとして保存してから追加してください")
	}
	now := time.Now()
	template := strings.NewReplacer("{date}", now.Format("20060102"), "{time}", now.Format("150405")).Replace(a.cfg.ImageName)
	for i := 1; i <= 100000; i++ {
		name := strings.ReplaceAll(template, "{counter}", fmt.Sprintf("%03d", i))
		if !strings.Contains(template, "{counter}") && i > 1 {
			name += fmt.Sprintf("-%03d", i)
		}
		directory := a.cfg.ImageDirectory
		if src, err := detectBook(a.session.Doc); err == nil && src != "" && src != "." {
			directory = path.Join(src, directory)
		}
		name = path.Join(directory, name+ext)
		exists := false
		for p := range a.session.Doc.Files {
			if strings.EqualFold(p, name) {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		if err := a.session.Put(name, b); err != nil {
			return "", err
		}
		return name, nil
	}
	return "", fmt.Errorf("画像のファイル名を確保できません")
}
func readLimited(filename string) ([]byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, bundle.MaxFile+1))
	if err == nil && len(b) > bundle.MaxFile {
		return nil, fmt.Errorf("ファイルは32MiB以下にしてください")
	}
	return b, err
}

func imageContentType(name string) (string, bool) {
	types := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml"}
	kind, ok := types[strings.ToLower(filepath.Ext(name))]
	return kind, ok
}

func writePreviewImage(w http.ResponseWriter, kind string, b []byte) {
	w.Header().Set("Content-Type", kind)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

// serveLocalMarkdownImage は単一Markdownから参照されたローカル画像だけを読み込みます。
func (a *App) serveLocalMarkdownImage(w http.ResponseWriter, r *http.Request) {
	reference := r.URL.Query().Get("path")
	if reference == "" || strings.ContainsRune(reference, 0) || strings.Contains(reference, "\\") || filepath.IsAbs(filepath.FromSlash(reference)) {
		http.NotFound(w, r)
		return
	}
	kind, ok := imageContentType(reference)
	if !ok {
		http.NotFound(w, r)
		return
	}
	a.mu.Lock()
	if !a.singleMarkdownLocked() {
		a.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	source := a.session.Filename
	a.mu.Unlock()

	filename := filepath.Clean(filepath.Join(filepath.Dir(source), filepath.FromSlash(reference)))
	f, err := os.Open(filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	b, err := io.ReadAll(io.LimitReader(f, bundle.MaxFile+1))
	if err != nil || len(b) > bundle.MaxFile {
		http.NotFound(w, r)
		return
	}
	writePreviewImage(w, kind, b)
}

func (a *App) serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/local-image" {
		a.serveLocalMarkdownImage(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/bundle/")
	if !strings.HasPrefix(r.URL.Path, "/bundle/") || !bundle.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	kind, ok := imageContentType(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	a.mu.Lock()
	if a.session == nil {
		a.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	b, ok := a.session.Doc.Files[name]
	a.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	writePreviewImage(w, kind, b)
}
