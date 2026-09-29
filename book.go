package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/process"
	"github.com/hatolife/mdz-gui/internal/workspace"
)

// bookProcess は文書単位のプレビューと終了待ちを保持します。
type bookProcess struct {
	proxy *http.Server
	cmd   *exec.Cmd
	done  chan struct{}
	url   string
	log   *bookLog
}
type bookLog struct {
	sync.Mutex
	text string
}

func (b *bookLog) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	b.text += string(p)
	if len(b.text) > 16000 {
		b.text = b.text[len(b.text)-16000:]
	}
	return len(p), nil
}
func (b *bookLog) String() string { b.Lock(); defer b.Unlock(); return b.text }

type BookInfo struct {
	Title      string `json:"title"`
	Present    bool   `json:"present"`
	Detected   bool   `json:"detected"`
	Source     string `json:"source"`
	Executable string `json:"executable"`
	Winget     bool   `json:"winget"`
	URL        string `json:"url"`
	Error      string `json:"error"`
	Trusted    bool   `json:"trusted"`
}

//go:embed templates/mdbook
var bookTemplate embed.FS

// newDocument は外部コマンドなしで編集可能なソース一式を作ります。
func newDocument(kind string) (*bundle.Document, error) {
	d := bundle.New()
	switch kind {
	case "slides":
		return bundle.NewSlides()
	case "document", "project":
		d.Files = map[string][]byte{"本文.md": []byte("# 新しい文書\n\n編集モードでは、この文章を書き換えて文書を作成できます。\n\n右上の「表示 / 編集」で、読みやすい表示と編集を切り替えられます。\n\n画像やページを追加して、ひとつのMDZファイルに保存できます。\n")}
		d.Entry = "本文.md"
		if kind == "project" {
			d.Mode = "project"
		}
		return d, nil
	case "mdbook":
		files := map[string][]byte{"manifest.json": []byte(`{"spec":{"name":"mdzip-spec","version":"1.1.0"},"mode":"project","entryPoint":"src/introduction.md"}`)}
		err := fs.WalkDir(bookTemplate, "templates/mdbook", func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			data, err := bookTemplate.ReadFile(name)
			if err != nil {
				return err
			}
			files[strings.TrimPrefix(name, "templates/mdbook/")] = data
			return nil
		})
		if err != nil {
			return nil, err
		}
		return bundle.FromFiles(files)
	default:
		return nil, fmt.Errorf("新規文書の種類が不正です")
	}
}

// detectBook は設定されたソースディレクトリを文書内に限定します。
func detectBook(d *bundle.Document) (string, error) {
	if d.Mode == "slides" {
		return "", nil
	}
	data, ok := d.Files["book.toml"]
	if !ok {
		return "", nil
	}
	var cfg struct {
		Book struct {
			Src string `toml:"src"`
		} `toml:"book"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("book.toml: %w", err)
	}
	src := cfg.Book.Src
	if src == "" {
		src = "src"
	}
	if src != "." && !bundle.ValidPath(src) {
		return "", fmt.Errorf("mdBookのsrcは文書内の相対パスにしてください")
	}
	if _, ok := d.Files[path.Join(src, "SUMMARY.md")]; !ok {
		return "", fmt.Errorf("%sがありません", path.Join(src, "SUMMARY.md"))
	}
	return src, nil
}
func findMdbook(custom string) string {
	if custom != "" {
		if p, err := exec.LookPath(custom); err == nil {
			return p
		}
		return ""
	}
	if p, err := exec.LookPath("mdbook"); err == nil {
		return p
	}
	// インストール直後も既存プロセスのPATH更新を待たずに検出します。
	if goruntime.GOOS == "windows" {
		roots := []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet"), filepath.Join(os.Getenv("ProgramFiles"), "WinGet")}
		for _, root := range roots {
			patterns := []string{filepath.Join(root, "Links", "mdbook.exe"), filepath.Join(root, "Packages", "Rustlang.mdBook_*", "mdbook.exe"), filepath.Join(root, "Packages", "Rustlang.mdBook_*", "*", "mdbook.exe")}
			for _, pattern := range patterns {
				matches, _ := filepath.Glob(pattern)
				for _, p := range matches {
					if st, e := os.Stat(p); e == nil && !st.IsDir() {
						return p
					}
				}
			}
		}
	}
	return ""
}
func (a *App) BookStatus() BookInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	info := BookInfo{Executable: findMdbook(a.cfg.MdbookPath), Trusted: a.bookTrusted}
	info.Winget = goruntime.GOOS == "windows" && wingetExecutable() != ""
	if a.session == nil {
		return info
	}
	_, info.Present = a.session.Doc.Files["book.toml"]
	var config struct {
		Book struct {
			Title string `toml:"title"`
		} `toml:"book"`
	}
	_ = toml.Unmarshal(a.session.Doc.Files["book.toml"], &config)
	info.Title = config.Book.Title
	src, err := detectBook(a.session.Doc)
	info.Source = src
	info.Detected = src != ""
	if err != nil {
		info.Error = err.Error()
	}
	if a.book != nil {
		select {
		case <-a.book.done:
			info.Error = "mdBookが終了しました。" + a.book.log.String()
		default:
			info.URL = a.book.url
		}
	}
	return info
}
func (a *App) ChooseMdbook() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "mdbook.exeを指定", Filters: []runtime.FileFilter{{DisplayName: "実行ファイル", Pattern: "*.exe"}}})
}

// wingetExecutable はユーザーが指定したWindowsAppsの実行エイリアスを直接参照します。
func wingetExecutable() string {
	profile := os.Getenv("USERPROFILE")
	if profile == "" {
		profile, _ = os.UserHomeDir()
	}
	if profile == "" {
		return ""
	}
	filename := filepath.Join(profile, "AppData", "Local", "Microsoft", "WindowsApps", "winget.exe")
	if _, err := os.Stat(filename); err != nil {
		return ""
	}
	return filename
}
func (a *App) InstallMdbook() (string, error) {
	if goruntime.GOOS != "windows" {
		return "", fmt.Errorf("wingetはWindowsで使用できます")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	winget := wingetExecutable()
	if winget == "" {
		return "", fmt.Errorf("%%USERPROFILE%%\\AppData\\Local\\Microsoft\\WindowsApps\\winget.exeが見つかりません")
	}
	cmd := process.CommandContext(ctx, winget, "install", "--id", "Rustlang.mdBook", "--exact", "--source", "winget", "--accept-source-agreements", "--accept-package-agreements", "--disable-interactivity")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("wingetの導入に失敗しました: %w\n%s", err, output)
	}
	executable := findMdbook("")
	if executable == "" {
		return "", fmt.Errorf("導入は完了しましたがmdbook.exeを検出できません。実行ファイルを指定してください")
	}
	return executable, nil
}
func freePort() (int, error) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return 0, e
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
func (a *App) StartBook(allow bool) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return "", fmt.Errorf("文書を開いてください")
	}
	if err := a.syncNativeLocked(); err != nil {
		return "", err
	}
	if !allow && !a.bookTrusted {
		return "", fmt.Errorf("mdBookの実行を許可してください")
	}
	if _, err := detectBook(a.session.Doc); err != nil {
		return "", err
	}
	if src, _ := detectBook(a.session.Doc); src == "" {
		return "", fmt.Errorf("mdBookが含まれていません")
	}
	if a.book != nil {
		select {
		case <-a.book.done:
			a.stopBookLocked()
		default:
			return a.book.url, nil
		}
	}
	exe := findMdbook(a.cfg.MdbookPath)
	if exe == "" {
		return "", fmt.Errorf("mdBookが見つかりません")
	}
	port, err := freePort()
	if err != nil {
		return "", err
	}
	// 生成物はMDZに保存するcontentの外に置きます。
	cmd := process.Command(exe, "serve", a.session.Content(), "--hostname", "127.0.0.1", "--port", fmt.Sprint(port), "--dest-dir", filepath.Join(a.session.Root, "book-preview"))
	cmd.Dir = a.session.Root
	cmd.Env = append(os.Environ(), "MDBOOK_BUILD__CREATE_MISSING=false")
	b := &bookProcess{cmd: cmd, done: make(chan struct{}), url: fmt.Sprintf("http://127.0.0.1:%d/", port), log: &bookLog{}}
	cmd.Stdout = b.log
	cmd.Stderr = b.log
	if err := cmd.Start(); err != nil {
		return "", err
	}
	a.book = b
	a.bookTrusted = true
	if err := a.rememberBookLocked(); err != nil {
		a.emit("app-error", err.Error())
	}
	go func() { _ = cmd.Wait(); close(b.done) }()
	client := http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-b.done:
			a.book = nil
			return "", fmt.Errorf("mdBookを起動できません: %s", b.log.String())
		default:
		}
		resp, e := client.Get(b.url)
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				proxy, url, err := attachBookPreview(b.url)
				if err != nil {
					a.stopBookLocked()
					return "", err
				}
				b.proxy = proxy
				b.url = url
				return b.url, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	a.stopBookLocked()
	return "", fmt.Errorf("mdBookの起動がタイムアウトしました: %s", b.log.String())
}
func (a *App) stopBookLocked() {
	if a.book == nil {
		return
	}
	b := a.book
	a.book = nil
	if b.proxy != nil {
		_ = b.proxy.Close()
	}
	select {
	case <-b.done:
		return
	default:
	}
	if goruntime.GOOS == "windows" {
		_ = process.Command("taskkill", "/PID", fmt.Sprint(b.cmd.Process.Pid), "/T", "/F").Run()
	}
	_ = b.cmd.Process.Kill()
	<-b.done
}
func (a *App) StopBook() { a.mu.Lock(); defer a.mu.Unlock(); a.stopBookLocked() }

// EndEditing は入力を止めて作業ファイルを確定し、閲覧へ戻します。
func (a *App) EndEditing() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.syncNativeLocked(); err != nil {
		return err
	}
	if a.native != nil {
		a.inputNative.Store(nil)
		a.native.Close()
		a.native = nil
	}
	return nil
}

// SetDocumentMode は表示方式と独立した文書の意味付けを保存します。
func (a *App) SetDocumentMode(mode string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return fmt.Errorf("文書を開いてください")
	}
	if mode != "document" && mode != "project" {
		return fmt.Errorf("モードが不正です")
	}
	a.session.Doc.Mode = mode
	a.session.Doc.Manifest["mode"], _ = json.Marshal(mode)
	b, err := json.MarshalIndent(a.session.Doc.Manifest, "", "\t")
	if err != nil {
		return err
	}
	return a.session.Put("manifest.json", b)
}

// BookLog は実行結果の確認に使い、MDZには保存しません。
func (a *App) BookLog() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.book == nil {
		return ""
	}
	return strings.TrimSpace(a.book.log.String())
}

// bookFingerprint は文書の内容が変わった場合に実行許可を取り直すための識別子です。
func (a *App) bookFingerprint() string {
	h := sha256.New()
	names := make([]string, 0, len(a.session.Doc.Files))
	for name := range a.session.Doc.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := a.session.Doc.Files[name]
		fmt.Fprintf(h, "%d:%s:%d:", len(name), name, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func (a *App) rememberedBookLocked() bool {
	if a.session == nil {
		return false
	}
	var entries map[string]bool
	data, err := os.ReadFile(filepath.Join(a.base, "book-trust.json"))
	if err != nil {
		return false
	}
	if json.Unmarshal(data, &entries) != nil {
		return false
	}
	return entries[a.bookFingerprint()]
}
func (a *App) rememberBookLocked() error {
	entries := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(a.base, "book-trust.json"))
	if err == nil {
		if err = json.Unmarshal(data, &entries); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if entries == nil {
		entries = map[string]bool{}
	}
	entries[a.bookFingerprint()] = true
	data, err = json.Marshal(entries)
	if err != nil {
		return err
	}
	return workspace.AtomicWrite(filepath.Join(a.base, "book-trust.json"), data)
}
