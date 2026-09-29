package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/hatolife/mdz-gui/internal/bundle"
)

// TocEntry は目次の行を表し、未対応の記法は元の行をそのまま保持します。
type TocEntry struct {
	ID      int    `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Target  string `json:"target"`
	Name    string `json:"name"`
	Depth   int    `json:"depth"`
	Missing bool   `json:"missing"`
	Raw     string `json:"-"`
}
type BookContents struct {
	Revision string     `json:"revision"`
	Entries  []TocEntry `json:"entries"`
	Unlisted []string   `json:"unlisted"`
	CanUndo  bool       `json:"canUndo"`
	CanRedo  bool       `json:"canRedo"`
}
type BookConfiguration struct {
	Text     string `json:"text"`
	Revision string `json:"revision"`
}

var tocLink = regexp.MustCompile(`^\[(.*)\]\((.*)\)\s*$`)

func contentRevision(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func escapeTocTitle(title string) string {
	return strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(title)
}
func unescapeTocTitle(title string) string {
	return strings.NewReplacer(`\[`, `[`, `\]`, `]`, `\\`, `\`).Replace(title)
}
func parseContents(data []byte, src string, files map[string][]byte) []TocEntry {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	entries := make([]TocEntry, 0, len(lines))
	indents := []int{0}
	titleSeen := false
	for i, line := range lines {
		e := TocEntry{ID: i, Kind: "raw", Raw: line}
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "# "):
			if !titleSeen && strings.TrimSpace(strings.Join(lines[:i], "\n")) == "" {
				e.Kind = "heading"
			} else {
				e.Kind = "part"
			}
			titleSeen = true
			e.Title = strings.TrimSpace(trim[2:])
			indents = []int{0}
		case trim == "---" || trim == "***" || trim == "___":
			e.Kind = "separator"
			indents = []int{0}
		default:
			list := strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ")
			link := trim
			if list {
				link = strings.TrimSpace(trim[2:])
			}
			if m := tocLink.FindStringSubmatch(link); m != nil {
				e.Kind = "standalone"
				e.Title = unescapeTocTitle(m[1])
				e.Target = m[2]
				if list {
					e.Kind = "chapter"
					n := len(line) - len(strings.TrimLeft(line, " \t"))
					indent := len(strings.ReplaceAll(line[:n], "\t", "    "))
					for len(indents) > 1 && indent < indents[len(indents)-1] {
						indents = indents[:len(indents)-1]
					}
					if indent > indents[len(indents)-1] {
						indents = append(indents, indent)
					}
					e.Depth = len(indents) - 1
				} else {
					indents = []int{0}
				}
				if e.Target != "" {
					u, err := url.Parse(e.Target)
					if err == nil && u.Scheme == "" && u.Host == "" && !strings.HasPrefix(u.Path, "/") {
						name := path.Clean(path.Join(src, u.Path))
						if bundle.ValidPath(name) && bundle.IsMarkdown(name) {
							e.Name = name
							_, exists := files[name]
							e.Missing = !exists
						}
					}
					if e.Name == "" {
						e.Missing = true
					}
				}
			}
		}
		entries = append(entries, e)
	}
	return entries
}
func entryLine(e TocEntry) string {
	switch e.Kind {
	case "chapter":
		return strings.Repeat("    ", e.Depth) + "- [" + escapeTocTitle(e.Title) + "](" + e.Target + ")"
	case "standalone":
		return "[" + escapeTocTitle(e.Title) + "](" + e.Target + ")"
	case "part":
		return "# " + e.Title
	default:
		return e.Raw
	}
}
func contentsBytes(entries []TocEntry) []byte {
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(e.Raw)
	}
	return []byte(b.String())
}
func (a *App) contentsLocked() (BookContents, string, error) {
	c := BookContents{Entries: []TocEntry{}, Unlisted: []string{}}
	if a.session == nil {
		return c, "", fmt.Errorf("文書を開いてください")
	}
	src, err := detectBook(a.session.Doc)
	if err != nil {
		return c, "", err
	}
	if src == "" {
		return c, "", fmt.Errorf("mdBookではありません")
	}
	name := path.Join(src, "SUMMARY.md")
	data := a.session.Doc.Files[name]
	c.Revision = contentRevision(data)
	if a.tocRevision != "" && a.tocRevision != c.Revision {
		a.tocUndo = nil
		a.tocRedo = nil
	}
	a.tocRevision = c.Revision
	c.Entries = parseContents(data, src, a.session.Doc.Files)
	used := map[string]bool{name: true}
	for _, e := range c.Entries {
		used[e.Name] = true
	}
	for _, p := range a.session.Doc.Pages() {
		if !used[p] && (src == "." || strings.HasPrefix(p, src+"/")) {
			c.Unlisted = append(c.Unlisted, p)
		}
	}
	c.CanUndo = len(a.tocUndo) > 0
	c.CanRedo = len(a.tocRedo) > 0
	return c, name, nil
}
func (a *App) Contents() (BookContents, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, _, err := a.contentsLocked()
	return c, err
}

// subtreeEnd は章の子孫を含めて移動するための範囲を求めます。
func subtreeEnd(es []TocEntry, i int) int {
	j := i + 1
	for j < len(es) {
		if es[j].Kind == "raw" {
			j++
			continue
		}
		if es[i].Kind == "chapter" && es[j].Kind == "chapter" && es[j].Depth > es[i].Depth {
			j++
			continue
		}
		break
	}
	return j
}
func previousEntry(es []TocEntry, i int) int {
	for j := i - 1; j >= 0; j-- {
		if es[j].Kind != "raw" {
			return j
		}
	}
	return -1
}
func (a *App) writeBookFileLocked(name string, data []byte) error {
	if err := a.session.Put(name, data); err != nil {
		return err
	}
	if a.native != nil {
		return a.native.ReloadFile(name)
	}
	return nil
}

// ChangeContents は元の目次を照合してから、GUIの変更をSUMMARY.mdに反映します。
func (a *App) ChangeContents(revision string, index int, operation, value string) (BookContents, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	empty := BookContents{}
	if err := a.syncNativeLocked(); err != nil {
		return empty, err
	}
	c, name, err := a.contentsLocked()
	if err != nil {
		return empty, err
	}
	if c.Revision != revision {
		return empty, fmt.Errorf("目次が変更されました。表示を更新してから操作してください")
	}
	old := bytes.Clone(a.session.Doc.Files[name])
	es := c.Entries
	src := path.Dir(name)
	var data []byte
	if operation == "undo" || operation == "redo" {
		from, to := &a.tocUndo, &a.tocRedo
		if operation == "redo" {
			from, to = to, from
		}
		if len(*from) == 0 {
			return c, nil
		}
		data = bytes.Clone((*from)[len(*from)-1])
		if err := a.writeBookFileLocked(name, data); err != nil {
			return empty, err
		}
		a.tocRevision = contentRevision(data)
		*from = (*from)[:len(*from)-1]
		*to = append(*to, old)
		c, _, err = a.contentsLocked()
		return c, err
	}
	validTitle := func() bool {
		return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "\r\n\x00") && len(value) <= 500
	}
	if operation == "add" || operation == "attach" || operation == "part" {
		if !validTitle() {
			return empty, fmt.Errorf("名前を1行で指定してください")
		}
		e := TocEntry{Kind: "chapter", Title: strings.TrimSpace(value)}
		if operation == "part" {
			e.Kind = "part"
		} else {
			if operation == "attach" {
				found := false
				for _, p := range c.Unlisted {
					if p == value {
						found = true
					}
				}
				if !found {
					return empty, fmt.Errorf("目次にないページを指定してください")
				}
				e.Name = value
				e.Title = strings.TrimSuffix(path.Base(value), path.Ext(value))
			} else {
				for n := 1; n <= 4096; n++ {
					candidate := path.Join(src, fmt.Sprintf("chapters/page-%03d.md", n))
					exists := false
					for p := range a.session.Doc.Files {
						if strings.EqualFold(p, candidate) {
							exists = true
							break
						}
					}
					if !exists {
						e.Name = candidate
						break
					}
				}
				if e.Name == "" {
					return empty, fmt.Errorf("ページ名を確保できません")
				}
			}
			rel := strings.TrimPrefix(e.Name, src+"/")
			if src == "." {
				rel = e.Name
			}
			e.Target = (&url.URL{Path: rel}).String()
			if operation == "add" {
				if err := a.session.Put(e.Name, []byte("# "+e.Title+"\n\nここから書き始めます。\n")); err != nil {
					return empty, err
				}
			}
		}
		pos := len(es)
		for pos > 0 && (es[pos-1].Kind == "raw" || es[pos-1].Kind == "standalone" || es[pos-1].Kind == "separator") {
			pos--
		}
		if index >= 0 && index < len(es) && es[index].Kind == "chapter" {
			parent := index
			if e.Kind == "part" {
				for parent > 0 && es[parent].Depth > 0 {
					parent = previousEntry(es, parent)
					if parent < 0 {
						parent = index
						break
					}
				}
			}
			pos = subtreeEnd(es, parent)
			if e.Kind == "chapter" {
				e.Depth = es[index].Depth
			}
		}
		e.Raw = entryLine(e)
		es = append(es[:pos], append([]TocEntry{e, {Kind: "raw", Raw: ""}}, es[pos:]...)...)
	} else {
		if index < 0 || index >= len(es) {
			return empty, fmt.Errorf("目次の項目を選んでください")
		}
		e := es[index]
		end := subtreeEnd(es, index)
		switch operation {
		case "rename":
			if !validTitle() {
				return empty, fmt.Errorf("名前を1行で指定してください")
			}
			if e.Kind != "chapter" && e.Kind != "standalone" && e.Kind != "part" {
				return empty, fmt.Errorf("名前を変更できない項目です")
			}
			es[index].Title = strings.TrimSpace(value)
			es[index].Raw = entryLine(es[index])
		case "remove":
			if e.Kind == "heading" || e.Kind == "raw" {
				return empty, fmt.Errorf("削除できない項目です")
			}
			es = append(es[:index], es[end:]...)
		case "up":
			j := previousEntry(es, index)
			for j >= 0 && es[j].Kind == "chapter" && es[j].Depth > e.Depth {
				j = previousEntry(es, j)
			}
			if j < 0 || es[j].Kind != e.Kind || es[j].Depth != e.Depth {
				return empty, fmt.Errorf("同じ階層の前の項目がありません")
			}
			block := append([]TocEntry{}, es[index:end]...)
			es = append(es[:index], es[end:]...)
			es = append(es[:j], append(block, es[j:]...)...)
		case "down":
			if end >= len(es) || es[end].Kind != e.Kind || es[end].Depth != e.Depth {
				return empty, fmt.Errorf("同じ階層の次の項目がありません")
			}
			next := subtreeEnd(es, end)
			block := append([]TocEntry{}, es[index:end]...)
			es = append(es[:index], append(append([]TocEntry{}, es[end:next]...), append(block, es[next:]...)...)...)
		case "indent", "outdent":
			if e.Kind != "chapter" {
				return empty, fmt.Errorf("章の階層だけ変更できます")
			}
			delta := 1
			if operation == "indent" {
				j := previousEntry(es, index)
				for j >= 0 && es[j].Kind == "chapter" && es[j].Depth > e.Depth {
					j = previousEntry(es, j)
				}
				if j < 0 || es[j].Kind != "chapter" || es[j].Depth != e.Depth || e.Depth >= 8 {
					return empty, fmt.Errorf("親にできる前の章がありません")
				}
			} else {
				if e.Depth == 0 {
					return empty, fmt.Errorf("最上位の章です")
				}
				delta = -1
			}
			for j := index; j < end; j++ {
				if es[j].Kind == "chapter" {
					es[j].Depth += delta
					es[j].Raw = entryLine(es[j])
				}
			}
			if delta < 0 {
				j := index - 1
				for j >= 0 && (es[j].Kind != "chapter" || es[j].Depth >= e.Depth) {
					j--
				}
				if j >= 0 {
					next := end
					for next < len(es) && (es[next].Kind == "raw" || (es[next].Kind == "chapter" && es[next].Depth > es[j].Depth)) {
						next++
					}
					block := append([]TocEntry{}, es[index:end]...)
					es = append(es[:index], append(append([]TocEntry{}, es[end:next]...), append(block, es[next:]...)...)...)
				}
			}
		default:
			return empty, fmt.Errorf("未対応の目次操作です")
		}
	}
	data = contentsBytes(es)
	if err := a.writeBookFileLocked(name, data); err != nil {
		return empty, err
	}
	a.tocRevision = contentRevision(data)
	a.tocUndo = append(a.tocUndo, old)
	a.tocRedo = nil
	limit := a.cfg.UndoLevels
	if len(a.tocUndo) > limit {
		a.tocUndo = a.tocUndo[len(a.tocUndo)-limit:]
	}
	for totalHistory(a.tocUndo) > 64<<20 && len(a.tocUndo) > 1 {
		a.tocUndo = a.tocUndo[1:]
	}
	c, _, err = a.contentsLocked()
	return c, err
}
func totalHistory(history [][]byte) int {
	n := 0
	for _, b := range history {
		n += len(b)
	}
	return n
}
func (a *App) GetBookConfiguration() (BookConfiguration, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return BookConfiguration{}, fmt.Errorf("文書を開いてください")
	}
	if err := a.syncNativeLocked(); err != nil {
		return BookConfiguration{}, err
	}
	data, ok := a.session.Doc.Files["book.toml"]
	if !ok {
		return BookConfiguration{}, fmt.Errorf("本の設定がありません")
	}
	return BookConfiguration{string(data), contentRevision(data)}, nil
}

// SaveBookConfiguration は未知の設定とコメントを保持し、不正なTOMLは書き込みません。
func (a *App) SaveBookConfiguration(revision, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return fmt.Errorf("文書を開いてください")
	}
	if err := a.syncNativeLocked(); err != nil {
		return err
	}
	if contentRevision(a.session.Doc.Files["book.toml"]) != revision {
		return fmt.Errorf("設定が変更されました。開き直してください")
	}
	if len(text) > bundle.MaxFile || !utf8.ValidString(text) {
		return fmt.Errorf("設定ファイルのサイズまたは文字コードが不正です")
	}
	var config map[string]any
	if err := toml.Unmarshal([]byte(text), &config); err != nil {
		return fmt.Errorf("TOMLの記述を確認してください: %w", err)
	}
	files := map[string][]byte{}
	for k, v := range a.session.Doc.Files {
		files[k] = v
	}
	files["book.toml"] = []byte(text)
	d := &bundle.Document{Files: files}
	if _, err := detectBook(d); err != nil {
		return err
	}
	if bytes.Equal(a.session.Doc.Files["book.toml"], []byte(text)) {
		return nil
	}
	a.stopBookLocked()
	a.bookTrusted = false
	return a.writeBookFileLocked("book.toml", []byte(text))
}
