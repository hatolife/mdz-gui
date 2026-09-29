package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/workspace"
)

type importedMarkdown struct {
	source string
	base   string
	data   []byte
}

// ImportMarkdownFiles は外部Markdownを現在の文書へコピーし、ドロップ位置へページとして挿入します。
func (a *App) ImportMarkdownFiles(filenames []string, target string, after bool) ([]string, error) {
	items, err := loadImportedMarkdown(filenames)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []string{}, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return nil, fmt.Errorf("文書を開いてください")
	}
	if a.singleMarkdownLocked() {
		return nil, fmt.Errorf("単一Markdownにはページを追加できません")
	}
	if err := a.syncNativeLocked(); err != nil {
		return nil, err
	}
	if err := a.session.Capture(); err != nil {
		return nil, err
	}

	if a.session.Doc.Mode == "slides" {
		return a.importSlidesLocked(items, target, after)
	}
	src, err := detectBook(a.session.Doc)
	if err != nil {
		return nil, err
	}
	if src != "" {
		return a.importBookMarkdownLocked(items, target, after)
	}
	return a.importDocumentMarkdownLocked(items, target, after)
}

func loadImportedMarkdown(filenames []string) ([]importedMarkdown, error) {
	items := make([]importedMarkdown, 0, len(filenames))
	for _, filename := range filenames {
		if !bundle.IsMarkdown(filename) {
			continue
		}
		absolute, err := filepath.Abs(filename)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("通常のMarkdownファイルを指定してください: %s", filepath.Base(absolute))
		}
		data, err := readLimited(absolute)
		if err != nil {
			return nil, err
		}
		items = append(items, importedMarkdown{source: absolute, base: filepath.Base(absolute), data: data})
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].base, items[j].base
		if naturalFilenameLess(left, right) {
			return true
		}
		if naturalFilenameLess(right, left) {
			return false
		}
		return strings.ToLower(items[i].source) < strings.ToLower(items[j].source)
	})
	return items, nil
}

func naturalFilenameLess(left, right string) bool {
	a, b := strings.ToLower(left), strings.ToLower(right)
	for i, j := 0, 0; i < len(a) || j < len(b); {
		if i >= len(a) {
			return true
		}
		if j >= len(b) {
			return false
		}
		ad, bd := a[i] >= '0' && a[i] <= '9', b[j] >= '0' && b[j] <= '9'
		if ad && bd {
			ai, bj := i, j
			for ai < len(a) && a[ai] >= '0' && a[ai] <= '9' {
				ai++
			}
			for bj < len(b) && b[bj] >= '0' && b[bj] <= '9' {
				bj++
			}
			an, bn := strings.TrimLeft(a[i:ai], "0"), strings.TrimLeft(b[j:bj], "0")
			if an == "" {
				an = "0"
			}
			if bn == "" {
				bn = "0"
			}
			if len(an) != len(bn) {
				return len(an) < len(bn)
			}
			if an != bn {
				return an < bn
			}
			if ai-i != bj-j {
				return ai-i < bj-j
			}
			i, j = ai, bj
			continue
		}
		if ad != bd {
			return ad
		}
		ai, bj := i, j
		for ai < len(a) && !(a[ai] >= '0' && a[ai] <= '9') {
			ai++
		}
		for bj < len(b) && !(b[bj] >= '0' && b[bj] <= '9') {
			bj++
		}
		as, bs := a[i:ai], b[j:bj]
		if as != bs {
			return as < bs
		}
		i, j = ai, bj
	}
	return left < right
}

func uniqueImportedMarkdownPath(directory, base string, files map[string][]byte) (string, error) {
	base = filepath.Base(base)
	ext := strings.ToLower(filepath.Ext(base))
	if ext != ".md" && ext != ".markdown" {
		ext = ".md"
	}
	if !workspace.PortablePath(base) || !bundle.IsMarkdown(base) {
		base = "imported" + ext
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	for n := 1; n <= 4096; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d%s", stem, n, ext)
		}
		candidate := name
		if directory != "" && directory != "." {
			candidate = path.Join(directory, name)
		}
		if !workspace.PortablePath(candidate) || markdownPathExists(files, candidate) {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("取り込み先のMarkdownファイル名を確保できません")
}

func markdownPathExists(files map[string][]byte, candidate string) bool {
	for name := range files {
		if strings.EqualFold(name, candidate) {
			return true
		}
	}
	return false
}

func importedMarkdownTitle(data []byte, base string) string {
	title := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			if title != "" {
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(base), filepath.Ext(base))
	}
	for len(title) > 500 {
		_, size := utf8.DecodeLastRuneInString(title)
		if size <= 0 {
			break
		}
		title = title[:len(title)-size]
	}
	if strings.TrimSpace(title) == "" {
		return "取り込んだページ"
	}
	return title
}

func insertionIndex(order []string, target string, after bool) (int, error) {
	if target == "" {
		return len(order), nil
	}
	for i, name := range order {
		if name == target {
			if after {
				return i + 1, nil
			}
			return i, nil
		}
	}
	return 0, fmt.Errorf("挿入位置のページが変更されています")
}

func (a *App) importDocumentMarkdownLocked(items []importedMarkdown, target string, after bool) ([]string, error) {
	order := append([]string(nil), a.session.Doc.Pages()...)
	insert, err := insertionIndex(order, target, after)
	if err != nil {
		return nil, err
	}
	imported := make([]string, 0, len(items))
	for _, item := range items {
		name, err := uniqueImportedMarkdownPath("", item.base, a.session.Doc.Files)
		if err != nil {
			return nil, err
		}
		if err := a.session.Put(name, item.data); err != nil {
			return nil, err
		}
		imported = append(imported, name)
	}
	next := make([]string, 0, len(order)+len(imported))
	next = append(next, order[:insert]...)
	next = append(next, imported...)
	next = append(next, order[insert:]...)
	if err := a.session.SetPageOrder(next); err != nil {
		return nil, err
	}
	return imported, nil
}

func (a *App) importSlidesLocked(items []importedMarkdown, target string, after bool) ([]string, error) {
	info, err := a.slidesLocked()
	if err != nil {
		return nil, err
	}
	deck := info.Deck
	if len(deck.Slides)+len(items) > 500 {
		return nil, fmt.Errorf("スライドは500枚までです")
	}
	insert := len(deck.Slides)
	if target != "" {
		insert = -1
		for i, slide := range deck.Slides {
			if slide.ID == target {
				insert = i
				if after {
					insert++
				}
				break
			}
		}
		if insert < 0 {
			return nil, fmt.Errorf("挿入位置のスライドが変更されています")
		}
	}

	usedIDs := map[string]bool{}
	for _, slide := range deck.Slides {
		usedIDs[slide.ID] = true
	}
	imported := make([]string, 0, len(items))
	added := make([]bundle.Slide, 0, len(items))
	for _, item := range items {
		name, err := uniqueImportedMarkdownPath("slides", item.base, a.session.Doc.Files)
		if err != nil {
			return nil, err
		}
		if err := a.session.Put(name, item.data); err != nil {
			return nil, err
		}
		id := ""
		for n := 1; n <= 999999; n++ {
			candidate := fmt.Sprintf("slide-%03d", n)
			if !usedIDs[candidate] {
				id = candidate
				usedIDs[candidate] = true
				break
			}
		}
		if id == "" {
			return nil, fmt.Errorf("スライドIDを確保できません")
		}
		added = append(added, bundle.Slide{
			ID:       id,
			File:     name,
			Title:    importedMarkdownTitle(item.data, item.base),
			Layout:   "standard",
			FontSize: 32,
		})
		imported = append(imported, name)
	}
	next := make([]bundle.Slide, 0, len(deck.Slides)+len(added))
	next = append(next, deck.Slides[:insert]...)
	next = append(next, added...)
	next = append(next, deck.Slides[insert:]...)
	deck.Slides = next
	if err := a.commitSlidesLocked(deck); err != nil {
		return nil, err
	}
	return imported, nil
}

func (a *App) importBookMarkdownLocked(items []importedMarkdown, target string, after bool) ([]string, error) {
	contents, summaryName, err := a.contentsLocked()
	if err != nil {
		return nil, err
	}
	entries := append([]TocEntry(nil), contents.Entries...)
	position := len(entries)
	depth := 0
	if target != "" {
		targetID, err := strconv.Atoi(target)
		if err != nil {
			return nil, fmt.Errorf("挿入位置が不正です")
		}
		index := -1
		for i, entry := range entries {
			if entry.ID == targetID && (entry.Kind == "chapter" || entry.Kind == "standalone" || entry.Kind == "part") {
				index = i
				if entry.Kind == "chapter" {
					depth = entry.Depth
				}
				break
			}
		}
		if index < 0 {
			return nil, fmt.Errorf("挿入位置の目次が変更されています")
		}
		position = index
		if after {
			position = index + 1
			if entries[index].Kind == "chapter" {
				position = subtreeEnd(entries, index)
			}
		}
	} else {
		for position > 0 && entries[position-1].Kind == "raw" && strings.TrimSpace(entries[position-1].Raw) == "" {
			position--
		}
	}

	src := path.Dir(summaryName)
	imported := make([]string, 0, len(items))
	added := make([]TocEntry, 0, len(items)+1)
	for _, item := range items {
		name, err := uniqueImportedMarkdownPath(src, item.base, a.session.Doc.Files)
		if err != nil {
			return nil, err
		}
		if err := a.session.Put(name, item.data); err != nil {
			return nil, err
		}
		relative := name
		if src != "." {
			relative = strings.TrimPrefix(name, src+"/")
		}
		entry := TocEntry{
			Kind:   "chapter",
			Title:  importedMarkdownTitle(item.data, item.base),
			Target: (&url.URL{Path: relative}).String(),
			Name:   name,
			Depth:  depth,
		}
		entry.Raw = entryLine(entry)
		added = append(added, entry)
		imported = append(imported, name)
	}
	added = append(added, TocEntry{Kind: "raw", Raw: ""})
	entries = append(entries[:position], append(added, entries[position:]...)...)

	old := bytes.Clone(a.session.Doc.Files[summaryName])
	data := contentsBytes(entries)
	if err := a.writeBookFileLocked(summaryName, data); err != nil {
		return nil, err
	}
	a.tocRevision = contentRevision(data)
	a.tocUndo = append(a.tocUndo, old)
	a.tocRedo = nil
	limit := a.cfg.UndoLevels
	if limit < 1 {
		limit = 100
	}
	if len(a.tocUndo) > limit {
		a.tocUndo = a.tocUndo[len(a.tocUndo)-limit:]
	}
	for totalHistory(a.tocUndo) > 64<<20 && len(a.tocUndo) > 1 {
		a.tocUndo = a.tocUndo[1:]
	}
	return imported, nil
}
