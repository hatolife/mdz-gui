package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/hatolife/mdz-gui/internal/settings"
)

func writeDroppedMarkdown(t *testing.T, directory, name, text string) string {
	t.Helper()
	filename := filepath.Join(directory, name)
	if err := os.WriteFile(filename, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestImportMarkdownFilesIntoDocumentUsesNaturalOrder(t *testing.T) {
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	defer a.shutdown()
	if opened, err := a.NewDocument("document"); err != nil || !opened {
		t.Fatalf("new document: opened=%v err=%v", opened, err)
	}

	source := t.TempDir()
	page10 := writeDroppedMarkdown(t, source, "page10.md", "# 10\n")
	page2 := writeDroppedMarkdown(t, source, "page2.md", "# 2\n")
	ignored := writeDroppedMarkdown(t, source, "ignored.txt", "ignored\n")
	imported, err := a.ImportMarkdownFiles([]string{page10, ignored, page2}, "本文.md", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 2 || imported[0] != "page2.md" || imported[1] != "page10.md" {
		t.Fatalf("unexpected imported files: %#v", imported)
	}
	pages := a.State().Pages
	want := []string{"本文.md", "page2.md", "page10.md"}
	if len(pages) != len(want) {
		t.Fatalf("unexpected pages: %#v", pages)
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Fatalf("unexpected pages: %#v", pages)
		}
	}
}

func TestImportMarkdownFilesIntoSlidesAtDropPosition(t *testing.T) {
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	defer a.shutdown()
	if opened, err := a.NewDocument("slides"); err != nil || !opened {
		t.Fatalf("new slides: opened=%v err=%v", opened, err)
	}
	before, err := a.Slides()
	if err != nil {
		t.Fatal(err)
	}
	target := before.Deck.Slides[0].ID

	source := t.TempDir()
	page10 := writeDroppedMarkdown(t, source, "page10.md", "# 10番\n")
	page2 := writeDroppedMarkdown(t, source, "page2.md", "# 2番\n")
	imported, err := a.ImportMarkdownFiles([]string{page10, page2}, target, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 2 || imported[0] != "slides/page2.md" || imported[1] != "slides/page10.md" {
		t.Fatalf("unexpected imported files: %#v", imported)
	}
	after, err := a.Slides()
	if err != nil {
		t.Fatal(err)
	}
	if after.Deck.Slides[1].File != "slides/page2.md" || after.Deck.Slides[2].File != "slides/page10.md" {
		t.Fatalf("unexpected slide order: %#v", after.Deck.Slides[:3])
	}
	if after.Deck.Slides[1].Title != "2番" || after.Deck.Slides[2].Title != "10番" {
		t.Fatalf("unexpected slide titles: %q, %q", after.Deck.Slides[1].Title, after.Deck.Slides[2].Title)
	}
}

func TestImportMarkdownFilesIntoMdbookUpdatesSummary(t *testing.T) {
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	defer a.shutdown()
	if opened, err := a.NewDocument("mdbook"); err != nil || !opened {
		t.Fatalf("new mdbook: opened=%v err=%v", opened, err)
	}
	before, err := a.Contents()
	if err != nil {
		t.Fatal(err)
	}
	targetIndex := -1
	for i, entry := range before.Entries {
		if entry.Kind == "chapter" {
			targetIndex = i
			break
		}
	}
	if targetIndex < 0 {
		t.Fatal("挿入対象の章がありません")
	}
	target := before.Entries[targetIndex]

	source := t.TempDir()
	page10 := writeDroppedMarkdown(t, source, "chapter10.md", "# 第10章\n")
	page2 := writeDroppedMarkdown(t, source, "chapter2.md", "# 第2章\n")
	imported, err := a.ImportMarkdownFiles([]string{page10, page2}, strconv.Itoa(target.ID), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 2 || imported[0] != "src/chapter2.md" || imported[1] != "src/chapter10.md" {
		t.Fatalf("unexpected imported files: %#v", imported)
	}
	after, err := a.Contents()
	if err != nil {
		t.Fatal(err)
	}
	index2, index10 := -1, -1
	for i, entry := range after.Entries {
		switch entry.Name {
		case "src/chapter2.md":
			index2 = i
			if entry.Depth != target.Depth || entry.Title != "第2章" {
				t.Fatalf("unexpected chapter2 entry: %#v", entry)
			}
		case "src/chapter10.md":
			index10 = i
			if entry.Depth != target.Depth || entry.Title != "第10章" {
				t.Fatalf("unexpected chapter10 entry: %#v", entry)
			}
		}
	}
	if index2 < 0 || index10 != index2+1 {
		t.Fatalf("imported pages are not naturally ordered: chapter2=%d chapter10=%d", index2, index10)
	}
	if text, err := a.Text("src/chapter2.md"); err != nil || text != "# 第2章\n" {
		t.Fatalf("chapter2 content=%q err=%v", text, err)
	}
}
