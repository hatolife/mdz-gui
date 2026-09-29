package bundle

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// SlideDeck は本文から独立した順序・表示設定を保持します。
type SlideDeck struct {
	Version      int     `json:"version"`
	Title        string  `json:"title"`
	Theme        string  `json:"theme"`
	Aspect       string  `json:"aspect"`
	MarginColor    string  `json:"marginColor,omitempty"`
	ContentMarginX int     `json:"contentMarginX,omitempty"`
	ContentMarginY int     `json:"contentMarginY,omitempty"`
	FontFamily     string  `json:"fontFamily,omitempty"`
	BodyFontSize int     `json:"bodyFontSize,omitempty"`
	H1FontSize   int     `json:"h1FontSize,omitempty"`
	H2FontSize   int     `json:"h2FontSize,omitempty"`
	H3FontSize   int     `json:"h3FontSize,omitempty"`
	H4FontSize   int     `json:"h4FontSize,omitempty"`
	H5FontSize   int     `json:"h5FontSize,omitempty"`
	Slides       []Slide `json:"slides"`
}

type Slide struct {
	ID         string `json:"id"`
	File       string `json:"file"`
	Title      string `json:"title"`
	Layout     string `json:"layout"`
	Background string `json:"background,omitempty"`
	FontSize   int    `json:"fontSize"`
	Notes      string `json:"notes,omitempty"`
}

// NewSlideDeck は新規作成・変換で共通利用する資料全体の既定値を返します。
func NewSlideDeck(title string) SlideDeck {
	return SlideDeck{
		Version:      1,
		Title:        title,
		Theme:        "light",
		Aspect:         "16:9",
		ContentMarginX: 60,
		ContentMarginY: 48,
		FontFamily:     "system",
		BodyFontSize: 20,
		H1FontSize:   42,
		H2FontSize:   28,
		H3FontSize:   26,
		H4FontSize:   24,
		H5FontSize:   22,
	}
}

var slideID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var slideColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Deck は参照先と設定を検証してから返します。
func (d *Document) Deck() (SlideDeck, error) {
	var deck SlideDeck
	if d.Mode != "slides" {
		return deck, fmt.Errorf("スライド文書ではありません")
	}
	data := d.Files["slides.json"]
	if !utf8.Valid(data) || json.Unmarshal(data, &deck) != nil {
		return deck, fmt.Errorf("slides.jsonを読み込めません")
	}
	return deck, deck.Validate(d.Files)
}

func (d SlideDeck) Validate(files map[string][]byte) error {
	if d.Version != 1 {
		return fmt.Errorf("未対応のスライド形式です")
	}
	if strings.TrimSpace(d.Title) == "" || len(d.Title) > 2000 {
		return fmt.Errorf("資料タイトルは1〜2000バイトで指定してください")
	}
	if d.Theme != "light" && d.Theme != "dark" {
		return fmt.Errorf("テーマはlightまたはdarkを指定してください")
	}
	if d.Aspect != "16:9" && d.Aspect != "4:3" {
		return fmt.Errorf("縦横比は16:9または4:3を指定してください")
	}
	if d.MarginColor != "" && !slideColor.MatchString(d.MarginColor) {
		return fmt.Errorf("余白の色は#RRGGBB形式で指定してください")
	}
	if d.ContentMarginX != 0 && (d.ContentMarginX < 8 || d.ContentMarginX > 180) {
		return fmt.Errorf("左右余白は8〜180pxで指定してください")
	}
	if d.ContentMarginY != 0 && (d.ContentMarginY < 8 || d.ContentMarginY > 180) {
		return fmt.Errorf("上下余白は8〜180pxで指定してください")
	}
	typography := d.FontFamily != "" || d.BodyFontSize != 0 || d.H1FontSize != 0 || d.H2FontSize != 0 || d.H3FontSize != 0 || d.H4FontSize != 0 || d.H5FontSize != 0
	if typography {
		switch d.FontFamily {
		case "system", "gothic", "mincho", "monospace":
		default:
			return fmt.Errorf("フォントはsystem、gothic、mincho、monospaceのいずれかを指定してください")
		}
		if d.BodyFontSize < 12 || d.BodyFontSize > 64 {
			return fmt.Errorf("本文の文字サイズは12〜64で指定してください")
		}
		for _, size := range []int{d.H1FontSize, d.H2FontSize, d.H3FontSize, d.H4FontSize, d.H5FontSize} {
			if size < 14 || size > 96 {
				return fmt.Errorf("見出しの文字サイズは14〜96で指定してください")
			}
		}
		if !(d.H1FontSize > d.H2FontSize && d.H2FontSize > d.H3FontSize && d.H3FontSize > d.H4FontSize && d.H4FontSize > d.H5FontSize) {
			return fmt.Errorf("見出しの文字サイズはH1 > H2 > H3 > H4 > H5にしてください")
		}
	}
	if len(d.Slides) == 0 || len(d.Slides) > 500 {
		return fmt.Errorf("スライド数は1〜500枚にしてください")
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	for _, s := range d.Slides {
		key := strings.ToLower(s.File)
		if !slideID.MatchString(s.ID) || ids[s.ID] || paths[key] {
			return fmt.Errorf("スライドIDまたは参照先が不正・重複しています")
		}
		ids[s.ID], paths[key] = true, true
		if !ValidPath(s.File) || !IsMarkdown(s.File) {
			return fmt.Errorf("スライドの本文パスが不正です: %s", s.File)
		}
		if _, ok := files[s.File]; !ok {
			return fmt.Errorf("スライドの本文がありません: %s", s.File)
		}
		if strings.TrimSpace(s.Title) == "" || len(s.Title) > 2000 {
			return fmt.Errorf("スライドタイトルは1〜2000バイトで指定してください")
		}
		switch s.Layout {
		case "standard", "cover", "image", "columns":
		default:
			return fmt.Errorf("未対応のレイアウトです")
		}
		if s.FontSize < 18 || s.FontSize > 64 {
			return fmt.Errorf("文字サイズは18〜64で指定してください")
		}
		if s.Background != "" && !slideColor.MatchString(s.Background) {
			return fmt.Errorf("背景色は#RRGGBB形式で指定してください")
		}
		if len(s.Notes) > 1<<20 {
			return fmt.Errorf("ノートは1MiB以下にしてください")
		}
	}
	return nil
}

func NewSlides() (*Document, error) {
	// 書式とページ移動を試せるサンプルを、編集可能なMarkdownとして用意します。
	samples := []struct {
		title, layout, text string
		size                int
	}{
		{"新規スライド", "cover", "# 新規スライド\n\nMarkdownで作るプレゼンテーション\n\n書き換えて使える8ページのサンプル\n", 32},
		{"内容を書く", "standard", "# 内容を書く\n\n- 1ページにつき、伝えたいことを1つに。\n- **大事な言葉**は太字で強調できます。\n- 左の一覧からページを追加・並べ替えできます。\n- この文章を、自分の発表内容に書き換えましょう。\n", 32},
		{"2つの案を比較する", "columns", "# 案A：小さく試す\n\n- すぐに着手できる\n- 結果を見て調整する\n- 変更しやすさを重視\n\n<!-- column -->\n\n# 案B：まとめて作る\n\n- 全体像を先に決める\n- 役割を分けて進める\n- 一貫性を重視\n", 30},
		{"表で整理する", "standard", "# 進め方を整理する\n\n| 段階 | やること | 得られるもの |\n|---|---|---|\n| 調査 | 必要な情報を集める | 判断材料 |\n| 作成 | 小さく作って動かす | 比較できる実物 |\n| 検証 | 実際に使ってみる | 次の改善点 |\n\n短い表なら、全体を一目で見渡せます。\n", 30},
		{"コードを見せる", "standard", "# 短いコードで伝える\n\n```go\nfunc greet(name string) string {\n\treturn \"こんにちは、\" + name\n}\n```\n\nコードは要点を絞り、説明は本文で補います。\n", 32},
		{"画像で伝える", "image", "# 試して、確かめて、改善する\n\n![調査・作成・検証の流れ](../images/slide-sample.svg)\n", 30},
		{"発表中の操作", "standard", "# 画面にはスライドだけ\n\n- 次へ：→・↓・Enter・ホイール下\n- 前へ：←・↑・Shift+Enter・ホイール上\n- 左30%をクリック：前へ／右30%：次へ\n- O：一覧表示／Esc：発表を終了\n", 30},
		{"まとめ", "cover", "# まずは発表してみましょう\n\n文章と画像を差し替えれば、あなたの資料になります。\n\nEscで編集画面に戻れます。\n", 32},
	}
	deck := NewSlideDeck("新規スライド")
	files := map[string][]byte{
		"manifest.json":           []byte(`{"mode":"slides","entryPoint":"slides/slide-001.md"}`),
		"images/slide-sample.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="960" height="300" viewBox="0 0 960 300"><rect width="960" height="300" rx="24" fill="#edf3fb"/><g fill="#326ed6"><rect x="40" y="75" width="240" height="150" rx="20"/><rect x="360" y="75" width="240" height="150" rx="20"/><rect x="680" y="75" width="240" height="150" rx="20"/></g><g stroke="#326ed6" stroke-width="8" fill="none"><path d="M295 150h45m-18-18 18 18-18 18M615 150h45m-18-18 18 18-18 18"/></g><g font-family="Segoe UI,Yu Gothic,sans-serif" text-anchor="middle" fill="white" font-size="36"><text x="160" y="162">調査</text><text x="480" y="162">作成</text><text x="800" y="162">検証</text></g></svg>`),
	}
	for i, sample := range samples {
		id := fmt.Sprintf("slide-%03d", i+1)
		file := "slides/" + id + ".md"
		deck.Slides = append(deck.Slides, Slide{ID: id, File: file, Title: sample.title, Layout: sample.layout, FontSize: sample.size})
		files[file] = []byte(sample.text)
	}
	data, err := json.MarshalIndent(deck, "", "\t")
	if err != nil {
		return nil, err
	}
	files["slides.json"] = data
	return FromFiles(files)
}
