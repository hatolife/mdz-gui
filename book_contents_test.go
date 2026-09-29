package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/settings"
)

func makeBook(t *testing.T) *App {
	t.Helper()
	cfg := settings.Default()
	cfg.Editor = "builtin"
	a := &App{base: t.TempDir(), cfg: cfg}
	if _, err := a.NewDocument("mdbook"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.shutdown)
	return a
}
func TestBookContentsEditing(t *testing.T) {
	a := makeBook(t)
	c, err := a.Contents()
	if err != nil {
		t.Fatal(err)
	}
	find := func(name string) int {
		for _, e := range c.Entries {
			if e.Name == name {
				return e.ID
			}
		}
		t.Fatalf("目次にありません: %s", name)
		return -1
	}
	change := func(op, name, value string) {
		t.Helper()
		id := -1
		if name != "" {
			id = find(name)
		}
		c, err = a.ChangeContents(c.Revision, id, op, value)
		if err != nil {
			t.Fatal(op, err)
		}
	}
	original := bytes.Clone(a.session.Doc.Files["src/SUMMARY.md"])
	change("rename", "src/guide/editing.md", "編集 [基本]")
	if !strings.Contains(string(a.session.Doc.Files["src/SUMMARY.md"]), `編集 \[基本\]`) {
		t.Fatal("表示名のエスケープが不正です")
	}
	change("down", "src/guide/editing.md", "")
	text := string(a.session.Doc.Files["src/SUMMARY.md"])
	if strings.Index(text, "contents.md") > strings.Index(text, "editing.md") || strings.Index(text, "images.md") < strings.Index(text, "editing.md") {
		t.Fatal("子ページを含めて移動できません", text)
	}
	change("undo", "", "")
	change("undo", "", "")
	if !bytes.Equal(original, a.session.Doc.Files["src/SUMMARY.md"]) {
		t.Fatal("Undoで元の目次に戻りません")
	}
	change("indent", "src/guide/contents.md", "")
	change("outdent", "src/guide/contents.md", "")
	change("add", "src/guide/settings.md", "追加ページ")
	if _, ok := a.session.Doc.Files["src/chapters/page-001.md"]; !ok {
		t.Fatal("本文がありません")
	}
	change("remove", "src/chapters/page-001.md", "")
	if len(c.Unlisted) != 1 {
		t.Fatal(c.Unlisted)
	}
	change("attach", "", "src/chapters/page-001.md")
	if len(c.Unlisted) != 0 {
		t.Fatal(c.Unlisted)
	}
	if _, err := a.ChangeContents("stale", 0, "rename", "競合"); err == nil {
		t.Fatal("古い目次を上書きしました")
	}
	// 未対応の記法も別の項目の編集では失われません。
	data := append(a.session.Doc.Files["src/SUMMARY.md"], []byte("\n<!-- 保持するコメント -->\n[参照形式][extra]\n[extra]: next.md\n")...)
	if err := a.session.Put("src/SUMMARY.md", data); err != nil {
		t.Fatal(err)
	}
	c, err = a.Contents()
	if err != nil {
		t.Fatal(err)
	}
	if c.CanUndo {
		t.Fatal("外部変更前のUndoが残っています")
	}
	change("rename", "src/introduction.md", "入口")
	if !bytes.Contains(a.session.Doc.Files["src/SUMMARY.md"], []byte("[extra]: next.md")) {
		t.Fatal("未対応の記法が失われました")
	}
}
func TestBookConfigurationRoundTrip(t *testing.T) {
	a := makeBook(t)
	cfg, err := a.GetBookConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(cfg.Text, "新規mdBook", "試験用の本", 1) + "\n# 保持する設定です。\n[output.html.fold]\nenable = true\n"
	if err := a.SaveBookConfiguration(cfg.Revision, text); err != nil {
		t.Fatal(err)
	}
	if a.BookStatus().Title != "試験用の本" {
		t.Fatal(a.BookStatus())
	}
	if err := a.SaveBookConfiguration(cfg.Revision, "[book]\n"); err == nil {
		t.Fatal("古い設定を上書きしました")
	}
	updated, _ := a.GetBookConfiguration()
	if err := a.SaveBookConfiguration(updated.Revision, "[invalid"); err == nil {
		t.Fatal("不正なTOMLを書き込みました")
	}
	if !bytes.Equal(a.session.Doc.Files["book.toml"], []byte(text)) {
		t.Fatal("設定が破損しました")
	}
	filename := filepath.Join(a.base, "roundtrip.mdz")
	if err := a.saveLocked(filename); err != nil {
		t.Fatal(err)
	}
	d, err := bundle.Read(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.Files["book.toml"], []byte(text)) {
		t.Fatal("設定が保存されませんでした")
	}
}
func TestWingetWindowsAppsPath(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("USERPROFILE", profile)
	exe := filepath.Join(profile, "AppData", "Local", "Microsoft", "WindowsApps", "winget.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	if wingetExecutable() != exe {
		t.Fatal(wingetExecutable())
	}
}
func TestBookEditsWithNativeAndServer(t *testing.T) {
	if os.Getenv("MDZ_NVIM_TEST") == "" || os.Getenv("MDZ_MDBOOK_TEST") == "" {
		t.Skip("実NeovimとmdBookのパスを指定してください")
	}
	a := makeBook(t)
	a.cfg.Editor = "neovim"
	a.cfg.NvimPath = os.Getenv("MDZ_NVIM_TEST")
	a.cfg.MdbookPath = os.Getenv("MDZ_MDBOOK_TEST")
	if !a.StartNative() {
		t.Fatal(a.nativeError)
	}
	if err := a.NativeOpen("src/introduction.md"); err != nil {
		t.Fatal(err)
	}
	if err := a.NativePaste("本文の変更\n"); err != nil {
		t.Fatal(err)
	}
	url, err := a.StartBook(true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Contents()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ChangeContents(c.Revision, -1, "add", "新しい章"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		resp, e := http.Get(url + "chapters/page-001.html")
		if e == nil {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 && bytes.Contains(data, []byte("新しい章")) {
				if !bytes.Contains(data, []byte("mdz-reader-layout")) {
					t.Fatal("目次の統合がありません")
				}
				found = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !found {
		t.Fatal("追加ページがmdBookに反映されません")
	}
	config, err := a.GetBookConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if err = a.SaveBookConfiguration(config.Revision, strings.Replace(config.Text, "新規mdBook", "変更した本", 1)); err != nil {
		t.Fatal(err)
	}
	if a.BookStatus().URL != "" {
		t.Fatal("古い設定のプロセスが残っています")
	}
	if err := a.NativeUndo(false); err != nil {
		t.Fatal(err)
	}
	if err := a.syncNativeLocked(); err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(a.session.Doc.Files["src/introduction.md"], []byte("本文の変更")) {
		t.Fatal("目次編集で本文のUndoが失われました")
	}
}
