package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/settings"
	"github.com/hatolife/mdz-gui/internal/workspace"
)

func TestDocumentPageOrderAndNames(t *testing.T) {
	d := bundle.New()
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	var err error
	a.session, err = workspace.New(a.base, d, "", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.shutdown)
	for _, name := range []string{"zeta", "日本語", "chapters/alpha.md"} {
		if err = a.AddPage(name); err != nil {
			t.Fatal(err)
		}
	}
	original := []string{"index.md", "zeta.md", "日本語.md", "chapters/alpha.md"}
	if !slices.Equal(a.State().Pages, original) {
		t.Fatal(a.State().Pages)
	}
	for _, name := range []string{"zeta.md", "ZETA", "../outside", "invalid.txt", "", "chapters/"} {
		if err = a.AddPage(name); err == nil {
			t.Fatalf("不正なページ追加を受理しました: %q", name)
		}
	}
	// 本文の相対リンクと未知のmanifestフィールドを並べ替え後も保持します。
	text := "[次へ](chapters/alpha.md)\n"
	if err = a.Update("index.md", text); err != nil {
		t.Fatal(err)
	}
	manifest := a.session.Doc.Manifest
	manifest["custom"] = json.RawMessage(`{"keep":true}`)
	data, _ := json.Marshal(manifest)
	if err = os.WriteFile(filepath.Join(a.session.Content(), "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.MovePage(original, "chapters/alpha.md", "index.md", false); err != nil {
		t.Fatal(err)
	}
	wanted := []string{"chapters/alpha.md", "index.md", "zeta.md", "日本語.md"}
	if !slices.Equal(a.State().Pages, wanted) {
		t.Fatal(a.State().Pages)
	}
	if err = a.MovePage(original, "zeta.md", "index.md", false); err == nil {
		t.Fatal("古い順序による変更を受理しました")
	}
	if err = a.MovePage(wanted, "chapters/alpha.md", "日本語.md", true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.State().Pages, original) {
		t.Fatal(a.State().Pages)
	}
	filename := filepath.Join(t.TempDir(), "ordered.mdz")
	if err = a.saveLocked(filename); err != nil {
		t.Fatal(err)
	}
	reopened, err := bundle.Read(filename)
	if err != nil {
		t.Fatal(err)
	}
	var custom map[string]bool
	if err = json.Unmarshal(reopened.Manifest["custom"], &custom); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reopened.Pages(), original) || string(reopened.Files["index.md"]) != text || !custom["keep"] {
		t.Fatal("保存後の順序・リンク・追加設定が変化しました")
	}
	// 外部追加のページは失わず末尾へ補い、消えた参照と重複は除外します。
	reopened.Files["aaa.md"] = []byte("# 追加")
	reopened.Manifest["x-mdz-gui-pageOrder"] = json.RawMessage(`["日本語.md","missing.md","日本語.md","index.md","zeta.md","chapters/alpha.md"]`)
	if !slices.Equal(reopened.Pages(), []string{"日本語.md", "index.md", "zeta.md", "chapters/alpha.md", "aaa.md"}) {
		t.Fatal(reopened.Pages())
	}
	// 未保存の並べ替えも復旧データから戻せます。
	if err = a.MovePage(original, "zeta.md", "index.md", false); err != nil {
		t.Fatal(err)
	}
	a.session.PID = 0
	if err = a.session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	recovered, err := workspace.Recover(a.base, a.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if recovered.Doc.Pages()[0] != "zeta.md" {
		t.Fatal("復旧時にページ順が失われました")
	}
}
