package main

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestReplaceBookTitle(t *testing.T) {
	for _, source := range []string{
		"# 保持\n[book]\ntitle = '旧タイトル' # 注釈\nsrc = 'src'\n[output.html]\ndefault-theme = 'light'\n",
		"[book]\ntitle = \"\"\"複数行\nタイトル\"\"\"\nsrc = 'src'\n",
		"book.title = '旧タイトル'\nbook.src = 'src'\n",
		"book = {title = '旧タイトル', src = 'src'}\n",
		"book = {src = 'src'}\n",
		"[book]\nsrc = 'src'\n",
		"[book]",
		"[output.html]\ndefault-theme = 'light'\n",
		"['book']\n\"title\" = '旧タイトル'\n",
	} {
		t.Run(source, func(t *testing.T) {
			const title = "新しい本 \"引用\" \\ 日本語"
			updated, err := replaceBookTitle([]byte(source), title)
			if err != nil {
				t.Fatal(err)
			}
			var config struct{ Book struct{ Title string } }
			if err := toml.Unmarshal(updated, &config); err != nil || config.Book.Title != title {
				t.Fatalf("%s: %v", updated, err)
			}
			for _, preserved := range []string{"# 保持", "# 注釈", "src = 'src'", "default-theme = 'light'"} {
				if strings.Contains(source, preserved) && !strings.Contains(string(updated), preserved) {
					t.Fatalf("設定が失われました: %s", preserved)
				}
			}
		})
	}
	if _, err := replaceBookTitle([]byte("[book]\n"), "  "); err == nil {
		t.Fatal("空のタイトルを受け付けました")
	}
}

func TestRenameBook(t *testing.T) {
	a := makeBook(t)
	config, err := a.GetBookConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RenameBook(config.Revision, "自分の本"); err != nil {
		t.Fatal(err)
	}
	if a.BookStatus().Title != "自分の本" || !a.State().Dirty {
		t.Fatal("タイトルまたは未保存状態が反映されません")
	}
	if err := a.RenameBook(config.Revision, "古い画面からの変更"); err == nil {
		t.Fatal("古い設定を上書きしました")
	}
}
