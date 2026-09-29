package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/settings"
)

func TestEmptyStartupAndBookTemplate(t *testing.T) {
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	defer a.shutdown()
	if a.State().ID != "" || a.StartNative() {
		t.Fatal("起動時に文書を作成しました")
	}
	if _, err := os.Stat(filepath.Join(a.base, "workspaces")); !os.IsNotExist(err) {
		t.Fatal("開始画面で作業ディレクトリを作成しました")
	}
	if !a.discardAllowed() {
		t.Fatal("開始画面で破棄確認が必要になりました")
	}
	if ok, err := a.NewDocument("mdbook"); err != nil || !ok {
		t.Fatal(err)
	}
	if src, err := detectBook(a.session.Doc); err != nil || src != "src" {
		t.Fatal(src, err)
	}
	if a.BookStatus().Title != "新規mdBook" || !strings.Contains(string(a.session.Doc.Files["book.toml"]), `description = "新規mdBookテンプレート"`) || !strings.HasPrefix(string(a.session.Doc.Files["src/introduction.md"]), "# 新規mdBookのテンプレート") {
		t.Fatal("新規テンプレートが不正です")
	}
	if a.State().Engine != "builtin" || a.native != nil {
		t.Fatal("作成時にNeovimが起動しました")
	}
	if _, err := a.StartBook(false); err == nil {
		t.Fatal("未許可の外部コマンドが起動しました")
	}
	file := filepath.Join(a.base, "test.mdz")
	if err := a.saveLocked(file); err != nil {
		t.Fatal(err)
	}
	d, err := bundle.Read(file)
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != "project" || d.Entry != "src/introduction.md" {
		t.Fatal(d)
	}
	d.Files["book.toml"] = []byte("[book]\nsrc='../outside'\n")
	if _, err := detectBook(d); err == nil {
		t.Fatal("外部パスを許可しました")
	}
	d.Files["book.toml"] = []byte("[book]\nsrc='custom'\n")
	d.Files["custom/SUMMARY.md"] = []byte("# 目次")
	if src, err := detectBook(d); err != nil || src != "custom" {
		t.Fatal(src, err)
	}
}
func TestRealMdbook(t *testing.T) {
	exe := os.Getenv("MDZ_MDBOOK_TEST")
	if exe == "" {
		t.Skip("実mdBookのパスを指定すると実行します")
	}
	cfg := settings.Default()
	cfg.MdbookPath = exe
	a := &App{base: t.TempDir(), cfg: cfg}
	defer a.shutdown()
	if _, err := a.NewDocument("mdbook"); err != nil {
		t.Fatal(err)
	}
	url, err := a.StartBook(true)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	file := filepath.Join(a.base, "book.mdz")
	if err := a.saveLocked(file); err != nil {
		t.Fatal(err)
	}
	d, err := bundle.Read(file)
	if err != nil {
		t.Fatal(err)
	}
	for name := range d.Files {
		if filepath.Ext(name) == ".html" {
			t.Fatal("生成HTMLが保存されました", name)
		}
	}
	if !a.rememberedBookLocked() {
		t.Fatal("実行許可が保存されませんでした")
	}
	if _, err := a.storeImageLocked([]byte("fixture"), ".png"); err != nil {
		t.Fatal(err)
	}
	found := false
	for name := range a.session.Doc.Files {
		if strings.HasPrefix(name, "src/images/") {
			found = true
		}
	}
	if !found {
		t.Fatal("画像がmdBookのソース領域にありません")
	}
	a.StopBook()
	if a.BookStatus().URL != "" {
		t.Fatal("終了後もURLが残っています")
	}
}


// 通常の新規文書はmdBookと独立した本文ページを持ちます。
func TestNewDocumentTemplate(t *testing.T) {
	for _, kind := range []string{"document", "project"} {
		d, err := newDocument(kind)
		if err != nil {
			t.Fatal(err)
		}
		if d.Entry != "本文.md" || len(d.Files) != 1 {
			t.Fatalf("新規文書の構成が不正です: %s, %v", d.Entry, d.Pages())
		}
		if !strings.Contains(string(d.Files[d.Entry]), "編集モード") {
			t.Fatal("編集方法の案内がありません")
		}
		filename := filepath.Join(t.TempDir(), "new.mdz")
		if err := d.Write(filename, "test"); err != nil {
			t.Fatal(err)
		}
		reopened, err := bundle.Read(filename)
		if err != nil {
			t.Fatal(err)
		}
		if reopened.Entry != d.Entry || string(reopened.Files[d.Entry]) != string(d.Files[d.Entry]) {
			t.Fatal("保存後に初期ページが変わりました")
		}
	}
}
