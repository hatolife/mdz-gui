package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/workspace"
)

const normalSlideSourcePrefix = "<!-- mdz-gui:normal-source "
const normalSlideSourceSuffix = " -->"

type normalSlideSource struct {
	Version int    `json:"version"`
	File    string `json:"file"`
	Page    int    `json:"page"`
	Part    int    `json:"part"`
	Parts   int    `json:"parts"`
	Mode    string `json:"mode"`
	Entry   bool   `json:"entry,omitempty"`
}

type convertedSlideContent struct {
	slide bundle.Slide
	meta  normalSlideSource
	body  string
	known bool
}

type restoredNormalPage struct {
	source  string
	parts   []string
	body    string
	tracked bool
	file    string
}

// ConvertDocumentMode は通常MDZとスライドMDZを、復元情報を保ちながら相互変換します。
func (a *App) ConvertDocumentMode(target string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return fmt.Errorf("文書を開いてください")
	}
	if err := a.syncNativeLocked(); err != nil {
		return err
	}

	var (
		doc *bundle.Document
		err error
	)
	switch target {
	case "slides":
		if a.session.Doc.Mode == "slides" {
			return nil
		}
		if _, ok := a.session.Doc.Files["book.toml"]; ok {
			return fmt.Errorf("mdBookは通常MDZへ戻してから変換してください")
		}
		doc, err = normalDocumentToSlides(a.session.Doc)
	case "document":
		if a.session.Doc.Mode != "slides" {
			return nil
		}
		doc, err = slideDocumentToNormal(a.session.Doc)
	default:
		return fmt.Errorf("変換先のモードが不正です")
	}
	if err != nil {
		return err
	}

	filename := a.session.Filename
	savedHash := a.session.SavedHash
	session, err := workspace.New(a.base, doc, filename, true)
	if err != nil {
		return err
	}
	// 保存済みファイルの外部変更検出基準は、変換前のセッションから引き継ぎます。
	session.SavedHash = savedHash
	if err := session.Checkpoint(); err != nil {
		_ = session.Close()
		return err
	}
	a.adoptLocked(session)
	return nil
}

func normalDocumentToSlides(doc *bundle.Document) (*bundle.Document, error) {
	if doc.Mode == "slides" {
		return nil, fmt.Errorf("すでにスライド文書です")
	}
	if _, ok := doc.Files["book.toml"]; ok {
		return nil, fmt.Errorf("mdBookはスライドへ直接変換できません")
	}

	pages := doc.Pages()
	if len(pages) == 0 {
		return nil, fmt.Errorf("変換するMarkdownページがありません")
	}
	mode := doc.Mode
	if mode != "project" {
		mode = "document"
	}

	files := copyConversionAssets(doc.Files)
	used := make(map[string]bool, len(doc.Files)+500)
	for name := range doc.Files {
		used[strings.ToLower(name)] = true
	}

	deck := bundle.NewSlideDeck("変換したスライド")
	for pageIndex, name := range pages {
		text, ok := doc.Files[name]
		if !ok {
			return nil, fmt.Errorf("Markdownページが見つかりません: %s", name)
		}
		parts := splitNormalMarkdownSlides(string(text))
		if len(deck.Slides)+len(parts) > 500 {
			return nil, fmt.Errorf("変換後のスライドは500枚までです")
		}
		for partIndex, body := range parts {
			meta := normalSlideSource{
				Version: 1,
				File:    name,
				Page:    pageIndex + 1,
				Part:    partIndex + 1,
				Parts:   len(parts),
				Mode:    mode,
				Entry:   name == doc.Entry,
			}
			slideFile := name
			if partIndex > 0 {
				slideFile = convertedSlidePath(name, partIndex+1, used)
			}
			slideText, err := addNormalSlideSource(body, meta)
			if err != nil {
				return nil, err
			}
			id := fmt.Sprintf("slide-%03d", len(deck.Slides)+1)
			title := slideTitle(body)
			deck.Slides = append(deck.Slides, bundle.Slide{
				ID:       id,
				File:     slideFile,
				Title:    title,
				Layout:   "standard",
				FontSize: 32,
			})
			files[slideFile] = []byte(slideText)
		}
	}
	if len(deck.Slides) > 0 && deck.Slides[0].Title != "取り込んだスライド" {
		deck.Title = deck.Slides[0].Title
	}

	data, err := json.MarshalIndent(deck, "", "\t")
	if err != nil {
		return nil, err
	}
	files["slides.json"] = data

	manifest := conversionManifest(doc)
	delete(manifest, "x-mdz-gui-pageOrder")
	manifest["mode"], _ = json.Marshal("slides")
	manifest["entryPoint"], _ = json.Marshal(deck.Slides[0].File)
	meta, err := json.MarshalIndent(manifest, "", "\t")
	if err != nil {
		return nil, err
	}
	files["manifest.json"] = meta
	return conversionDocument(files)
}

func slideDocumentToNormal(doc *bundle.Document) (*bundle.Document, error) {
	deck, err := doc.Deck()
	if err != nil {
		return nil, err
	}

	items := make([]convertedSlideContent, 0, len(deck.Slides))
	files := copyConversionAssets(doc.Files)
	used := make(map[string]bool, len(files)+len(deck.Slides))
	for name := range files {
		used[strings.ToLower(name)] = true
	}

	originalMode := ""
	entrySource := ""
	for _, slide := range deck.Slides {
		text := string(doc.Files[slide.File])
		meta, body, known := normalSlideSourceFromText(text)
		items = append(items, convertedSlideContent{slide: slide, meta: meta, body: body, known: known})
		if !known {
			continue
		}
		used[strings.ToLower(meta.File)] = true
		if originalMode == "" {
			originalMode = meta.Mode
		}
		if entrySource == "" && meta.Entry {
			entrySource = meta.File
		}
	}
	if originalMode == "" {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, item.body)
		}
		page := claimNormalPagePath(items[0].slide.File, used)
		files[page] = []byte(joinNormalParts(parts))
		manifest := conversionManifest(doc)
		manifest["mode"], _ = json.Marshal("document")
		manifest["entryPoint"], _ = json.Marshal(page)
		manifest["x-mdz-gui-pageOrder"], _ = json.Marshal([]string{page})
		meta, err := json.MarshalIndent(manifest, "", "\t")
		if err != nil {
			return nil, err
		}
		files["manifest.json"] = meta
		return conversionDocument(files)
	}
	if originalMode != "project" {
		originalMode = "document"
	}

	groups := map[string]*restoredNormalPage{}
	order := make([]*restoredNormalPage, 0, len(items))
	for _, item := range items {
		if item.known {
			key := strings.ToLower(item.meta.File)
			page := groups[key]
			if page == nil {
				page = &restoredNormalPage{source: item.meta.File, tracked: true}
				groups[key] = page
				order = append(order, page)
			}
			page.parts = append(page.parts, item.body)
			continue
		}
		order = append(order, &restoredNormalPage{body: item.body, source: item.slide.File})
	}

	pageOrder := make([]string, 0, len(order))
	entry := ""
	for _, page := range order {
		if page.tracked {
			page.file = page.source
			files[page.file] = []byte(joinNormalParts(page.parts))
		} else {
			page.file = claimNormalPagePath(page.source, used)
			files[page.file] = []byte(page.body)
		}
		pageOrder = append(pageOrder, page.file)
		if entry == "" && entrySource != "" && strings.EqualFold(page.file, entrySource) {
			entry = page.file
		}
	}
	if len(pageOrder) == 0 {
		return nil, fmt.Errorf("通常MDZへ戻せるページがありません")
	}
	if entry == "" {
		entry = pageOrder[0]
	}

	manifest := conversionManifest(doc)
	manifest["mode"], _ = json.Marshal(originalMode)
	manifest["entryPoint"], _ = json.Marshal(entry)
	manifest["x-mdz-gui-pageOrder"], _ = json.Marshal(pageOrder)
	meta, err := json.MarshalIndent(manifest, "", "\t")
	if err != nil {
		return nil, err
	}
	files["manifest.json"] = meta
	return conversionDocument(files)
}

// splitNormalMarkdownSlides はコードフェンス外の --- 行を通常MDZのページ区切りとして扱います。
func splitNormalMarkdownSlides(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var result []string
	start, fenceLength := 0, 0
	var fence byte
	appendPart := func(part []string) {
		for len(part) > 0 && strings.TrimSpace(part[0]) == "" {
			part = part[1:]
		}
		for len(part) > 0 && strings.TrimSpace(part[len(part)-1]) == "" {
			part = part[:len(part)-1]
		}
		if len(part) > 0 {
			result = append(result, strings.Join(part, "\n")+"\n")
		}
	}
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) > 3 || strings.HasPrefix(trimmed, "\t") {
			continue
		}
		if len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			n := 0
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
			if fenceLength == 0 && n >= 3 {
				fence, fenceLength = trimmed[0], n
				continue
			}
			if fenceLength > 0 && trimmed[0] == fence && n >= fenceLength && strings.TrimSpace(trimmed[n:]) == "" {
				fenceLength = 0
				continue
			}
		}
		if fenceLength == 0 && strings.TrimSpace(line) == "---" {
			appendPart(lines[start:i])
			start = i + 1
		}
	}
	appendPart(lines[start:])
	if len(result) == 0 {
		return []string{""}
	}
	return result
}

func addNormalSlideSource(body string, meta normalSlideSource) (string, error) {
	data, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	header := normalSlideSourcePrefix + string(data) + normalSlideSourceSuffix
	if body == "" {
		return header + "\n", nil
	}
	return header + "\n\n" + body, nil
}

func normalSlideSourceFromText(text string) (normalSlideSource, string, bool) {
	var meta normalSlideSource
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	limit := len(lines)
	if limit > 20 {
		limit = 20
	}
	for i := 0; i < limit; i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, normalSlideSourcePrefix) || !strings.HasSuffix(line, normalSlideSourceSuffix) {
			continue
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(line, normalSlideSourcePrefix), normalSlideSourceSuffix)
		if json.Unmarshal([]byte(raw), &meta) != nil ||
			meta.Version != 1 ||
			!bundle.ValidPath(meta.File) ||
			!bundle.IsMarkdown(meta.File) ||
			meta.Page < 1 ||
			meta.Part < 1 ||
			meta.Parts < meta.Part ||
			(meta.Mode != "document" && meta.Mode != "project") {
			return normalSlideSource{}, text, false
		}
		end := i + 1
		if end < len(lines) && strings.TrimSpace(lines[end]) == "" {
			end++
		}
		bodyLines := append([]string{}, lines[:i]...)
		bodyLines = append(bodyLines, lines[end:]...)
		return meta, strings.Join(bodyLines, "\n"), true
	}
	return normalSlideSource{}, text, false
}

func stripNormalSlideSource(text string) string {
	_, body, ok := normalSlideSourceFromText(text)
	if !ok {
		return text
	}
	return body
}

func convertedSlidePath(source string, part int, used map[string]bool) string {
	dir := path.Dir(source)
	if dir == "." {
		dir = ""
	}
	ext := path.Ext(source)
	stem := strings.TrimSuffix(path.Base(source), ext)
	base := path.Join(dir, fmt.Sprintf("%s.mdz-slide-%03d.md", stem, part))
	if !used[strings.ToLower(base)] {
		used[strings.ToLower(base)] = true
		return base
	}
	for n := 2; ; n++ {
		candidate := path.Join(dir, fmt.Sprintf("%s.mdz-slide-%03d-%d.md", stem, part, n))
		if !used[strings.ToLower(candidate)] {
			used[strings.ToLower(candidate)] = true
			return candidate
		}
	}
}

func claimNormalPagePath(preferred string, used map[string]bool) string {
	key := strings.ToLower(preferred)
	if bundle.ValidPath(preferred) && bundle.IsMarkdown(preferred) && !used[key] {
		used[key] = true
		return preferred
	}
	dir := path.Dir(preferred)
	if dir == "." {
		dir = ""
	}
	ext := path.Ext(preferred)
	stem := strings.TrimSuffix(path.Base(preferred), ext)
	if stem == "" {
		stem = "page"
	}
	for n := 1; ; n++ {
		candidate := path.Join(dir, fmt.Sprintf("%s-page-%03d.md", stem, n))
		key = strings.ToLower(candidate)
		if bundle.ValidPath(candidate) && !used[key] {
			used[key] = true
			return candidate
		}
	}
}

func joinNormalParts(parts []string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		clean = append(clean, strings.TrimRight(part, "\n"))
	}
	text := strings.Join(clean, "\n\n---\n\n")
	if text == "" {
		return ""
	}
	return text + "\n"
}

func copyConversionAssets(files map[string][]byte) map[string][]byte {
	result := map[string][]byte{}
	for name, data := range files {
		if name == "manifest.json" || name == "slides.json" || bundle.IsMarkdown(name) {
			continue
		}
		result[name] = append([]byte(nil), data...)
	}
	return result
}

func conversionManifest(doc *bundle.Document) map[string]json.RawMessage {
	manifest := map[string]json.RawMessage{}
	if data := doc.Files["manifest.json"]; len(data) > 0 {
		_ = json.Unmarshal(data, &manifest)
	}
	if manifest == nil {
		manifest = map[string]json.RawMessage{}
	}
	if len(manifest) == 0 {
		for key, value := range doc.Manifest {
			manifest[key] = append(json.RawMessage(nil), value...)
		}
	}
	return manifest
}

func conversionDocument(files map[string][]byte) (*bundle.Document, error) {
	if len(files) > bundle.MaxEntries {
		return nil, fmt.Errorf("変換後のファイル数が上限を超えています")
	}
	total := 0
	for name, data := range files {
		if len(data) > bundle.MaxFile {
			return nil, fmt.Errorf("変換後のファイルが大きすぎます: %s", name)
		}
		total += len(data)
		if total > bundle.MaxTotal {
			return nil, fmt.Errorf("変換後の文書サイズが上限を超えています")
		}
	}
	return bundle.FromFiles(files)
}
