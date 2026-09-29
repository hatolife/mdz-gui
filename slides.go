package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/hatolife/mdz-gui/internal/bundle"
)

type SlidesInfo struct {
	Deck     bundle.SlideDeck `json:"deck"`
	Revision string           `json:"revision"`
	CanUndo  bool             `json:"canUndo"`
	CanRedo  bool             `json:"canRedo"`
}

func (a *App) slidesLocked() (SlidesInfo, error) {
	if a.session == nil {
		return SlidesInfo{}, fmt.Errorf("スライド文書を開いてください")
	}
	deck, err := a.session.Doc.Deck()
	if err != nil {
		return SlidesInfo{}, err
	}
	revision := contentRevision(a.session.Doc.Files["slides.json"])
	// 外部編集された設定へ過去の操作履歴を適用しません。
	if a.slideRevision != "" && a.slideRevision != revision {
		a.slideUndo, a.slideRedo = nil, nil
	}
	a.slideRevision = revision
	return SlidesInfo{Deck: deck, Revision: revision, CanUndo: len(a.slideUndo) > 0, CanRedo: len(a.slideRedo) > 0}, nil
}

func (a *App) Slides() (SlidesInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.slidesLocked()
}

// PrepareSlides は発表前にNeovimの未保存バッファも反映します。
func (a *App) PrepareSlides() (SlidesInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.syncNativeLocked(); err != nil {
		return SlidesInfo{}, err
	}
	return a.slidesLocked()
}

func (a *App) commitSlidesLocked(deck bundle.SlideDeck) error {
	if err := deck.Validate(a.session.Doc.Files); err != nil {
		return err
	}
	data, err := json.MarshalIndent(deck, "", "\t")
	if err != nil {
		return err
	}
	old := append([]byte(nil), a.session.Doc.Files["slides.json"]...)
	if string(old) == string(data) {
		return nil
	}
	if err := a.session.Put("slides.json", data); err != nil {
		return err
	}
	a.session.Doc.Entry = deck.Slides[0].File
	a.slideUndo = append(a.slideUndo, old)
	a.slideRedo = nil
	limit := a.cfg.UndoLevels
	if limit < 1 {
		limit = 100
	}
	for len(a.slideUndo) > limit || (totalHistory(a.slideUndo) > 16<<20 && len(a.slideUndo) > 1) {
		a.slideUndo = a.slideUndo[1:]
	}
	a.slideRevision = contentRevision(data)
	return nil
}

// ChangeSlide は本文のファイル名を変えずに構成を変更します。
func (a *App) ChangeSlide(revision, id, operation, value string) (SlidesInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.syncNativeLocked(); err != nil {
		return SlidesInfo{}, err
	}
	info, err := a.slidesLocked()
	if err != nil {
		return info, err
	}
	if revision != info.Revision {
		return info, fmt.Errorf("スライドの構成が変更されました。最新の一覧を確認してください")
	}
	deck := info.Deck
	index := -1
	for i, s := range deck.Slides {
		if s.ID == id {
			index = i
			break
		}
	}
	if operation == "undo" || operation == "redo" {
		from, to := &a.slideUndo, &a.slideRedo
		if operation == "redo" {
			from, to = to, from
		}
		if len(*from) == 0 {
			return info, nil
		}
		data := (*from)[len(*from)-1]
		var restored bundle.SlideDeck
		if err := json.Unmarshal(data, &restored); err != nil {
			return info, err
		}
		if err := restored.Validate(a.session.Doc.Files); err != nil {
			return info, err
		}
		old := append([]byte(nil), a.session.Doc.Files["slides.json"]...)
		if err := a.session.Put("slides.json", data); err != nil {
			return info, err
		}
		*from = (*from)[:len(*from)-1]
		*to = append(*to, old)
		a.session.Doc.Entry = restored.Slides[0].File
		a.slideRevision = contentRevision(data)
		return a.slidesLocked()
	}
	if operation != "add" && operation != "import" && index < 0 {
		return info, fmt.Errorf("スライドが見つかりません")
	}
	switch operation {
	case "add", "duplicate", "import":
		texts := []string{"# 新しいスライド\n\nここに内容を書きます。\n"}
		if operation == "duplicate" {
			texts[0] = stripNormalSlideSource(string(a.session.Doc.Files[deck.Slides[index].File]))
		}
		if operation == "import" {
			if len(value) > bundle.MaxFile {
				return info, fmt.Errorf("本文が大きすぎます")
			}
			if strings.HasPrefix(strings.TrimSpace(value), "---") {
				return info, fmt.Errorf("先頭のYAML設定や区切りを取り除いてから取り込んでください")
			}
			texts = splitSlideMarkdown(value, "---")
		}
		if len(deck.Slides)+len(texts) > 500 {
			return info, fmt.Errorf("スライドは500枚までです")
		}
		insert := index + 1
		if index < 0 {
			insert = len(deck.Slides)
		}
		for _, text := range texts {
			s := bundle.Slide{Title: "新しいスライド", Layout: "standard", FontSize: 32}
			if operation == "duplicate" {
				s = info.Deck.Slides[index]
				s.Title += "（コピー）"
			}
			if operation == "import" {
				s.Title = slideTitle(text)
			}
			// 同じディレクトリへの複製で画像の相対参照を保持します。
			dir := "slides"
			if operation == "duplicate" {
				dir = path.Dir(s.File)
			}
			for n := 1; ; n++ {
				s.ID = fmt.Sprintf("slide-%03d", n)
				s.File = path.Join(dir, s.ID+".md")
				used := false
				for _, existing := range deck.Slides {
					if existing.ID == s.ID {
						used = true
					}
				}
				for name := range a.session.Doc.Files {
					if strings.EqualFold(name, s.File) {
						used = true
					}
				}
				if !used {
					break
				}
			}
			if err := a.session.Put(s.File, []byte(text)); err != nil {
				return info, err
			}
			deck.Slides = append(deck.Slides, bundle.Slide{})
			copy(deck.Slides[insert+1:], deck.Slides[insert:])
			deck.Slides[insert] = s
			insert++
		}
	case "split":
		parts := splitSlidePageMarkdown(string(a.session.Doc.Files[deck.Slides[index].File]))
		if len(parts) < 2 {
			return info, fmt.Errorf("ページ分割できる --- が見つかりません")
		}
		if len(deck.Slides)+len(parts)-1 > 500 {
			return info, fmt.Errorf("スライドは500枚までです")
		}
		usedIDs := map[string]bool{}
		usedPaths := map[string]bool{}
		for _, existing := range deck.Slides {
			usedIDs[existing.ID] = true
			usedPaths[strings.ToLower(existing.File)] = true
		}
		for name := range a.session.Doc.Files {
			usedPaths[strings.ToLower(name)] = true
		}
		created := make([]bundle.Slide, 0, len(parts))
		for partIndex, text := range parts {
			s := deck.Slides[index]
			for n := 1; ; n++ {
				candidateID := fmt.Sprintf("slide-%03d", n)
				candidateFile := path.Join(path.Dir(s.File), candidateID+".md")
				if !usedIDs[candidateID] && !usedPaths[strings.ToLower(candidateFile)] {
					s.ID, s.File = candidateID, candidateFile
					usedIDs[candidateID] = true
					usedPaths[strings.ToLower(candidateFile)] = true
					break
				}
			}
			if partIndex > 0 {
				s.Title = slideTitle(text)
				s.Notes = ""
			}
			if err := a.session.Put(s.File, []byte(text)); err != nil {
				return info, err
			}
			created = append(created, s)
		}
		replaced := make([]bundle.Slide, 0, len(deck.Slides)+len(created)-1)
		replaced = append(replaced, deck.Slides[:index]...)
		replaced = append(replaced, created...)
		replaced = append(replaced, deck.Slides[index+1:]...)
		deck.Slides = replaced
	case "remove":
		if len(deck.Slides) == 1 {
			return info, fmt.Errorf("最後の1枚は削除できません")
		}
		// 本文と画像は構成Undoや既存リンクのため保持します。
		deck.Slides = append(deck.Slides[:index], deck.Slides[index+1:]...)
	case "up", "down", "move", "move-before", "move-after":
		destination := index - 1
		if operation == "down" {
			destination = index + 1
		}
		if strings.HasPrefix(operation, "move") {
			destination = -1
			for i, s := range deck.Slides {
				if s.ID == value {
					destination = i
					break
				}
			}
		}
		if destination >= 0 && (operation == "move-before" || operation == "move-after") {
			if destination == index {
				return info, nil
			}
			if destination > index {
				destination--
			}
			if operation == "move-after" {
				destination++
			}
		}
		if destination < 0 || destination >= len(deck.Slides) {
			return info, nil
		}
		s := deck.Slides[index]
		deck.Slides = append(deck.Slides[:index], deck.Slides[index+1:]...)
		deck.Slides = append(deck.Slides, bundle.Slide{})
		copy(deck.Slides[destination+1:], deck.Slides[destination:])
		deck.Slides[destination] = s
	default:
		return info, fmt.Errorf("未対応のスライド操作です")
	}
	if err := a.commitSlidesLocked(deck); err != nil {
		return info, err
	}
	return a.slidesLocked()
}

// ConfigureSlides は順序を変更せず資料・ページの設定を更新します。
func (a *App) ConfigureSlides(revision string, deck bundle.SlideDeck) (SlidesInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.syncNativeLocked(); err != nil {
		return SlidesInfo{}, err
	}
	info, err := a.slidesLocked()
	if err != nil {
		return info, err
	}
	if revision != info.Revision {
		return info, fmt.Errorf("スライドの設定が変更されています。設定を開き直してください")
	}
	if len(deck.Slides) != len(info.Deck.Slides) {
		return info, fmt.Errorf("構成変更はページ操作を使用してください")
	}
	for i, s := range deck.Slides {
		if s.ID != info.Deck.Slides[i].ID || s.File != info.Deck.Slides[i].File {
			return info, fmt.Errorf("ページの参照先は変更できません")
		}
	}
	if err := a.commitSlidesLocked(deck); err != nil {
		return info, err
	}
	return a.slidesLocked()
}

// splitSlideMarkdown は空行で囲まれた区切りだけを認識し、コード内は維持します。
func splitSlideMarkdown(text, separator string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var result []string
	start, fenceLength := 0, 0
	var fence byte
	appendPart := func(part []string) {
		// 本文先頭のインデントを失わないよう、空行だけを取り除きます。
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
		if fenceLength > 0 {
			continue
		}
		if strings.TrimSpace(line) == separator && (i == 0 || strings.TrimSpace(lines[i-1]) == "") && (i == len(lines)-1 || strings.TrimSpace(lines[i+1]) == "") {
			appendPart(lines[start:i])
			start = i + 1
		}
	}
	appendPart(lines[start:])
	if len(result) == 0 {
		result = []string{""}
	}
	return result
}

// splitSlidePageMarkdown は手動のページ分割用に、コードブロック外の単独 --- 行を分割位置として扱います。
func splitSlidePageMarkdown(text string) []string {
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
		result = []string{""}
	}
	return result
}

func slideTitle(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return "取り込んだスライド"
}

// RenderSlide は安全なMarkdown描画をレイアウト間で共有します。
func (a *App) RenderSlide(text, layout string) (string, error) {
	if len(text) > bundle.MaxFile {
		return "", fmt.Errorf("本文が大きすぎます")
	}
	if layout != "columns" {
		return a.Render(text)
	}
	parts := splitSlideMarkdown(text, "<!-- column -->")
	left, err := a.Render(parts[0])
	if err != nil {
		return "", err
	}
	right := ""
	if len(parts) > 1 {
		right, err = a.Render(strings.Join(parts[1:], "\n"))
	}
	return `<div class="slide-columns"><div>` + left + `</div><div>` + right + `</div></div>`, err
}
