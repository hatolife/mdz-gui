package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/settings"
	"github.com/hatolife/mdz-gui/internal/workspace"
)

func sampleConversionDocument(t *testing.T) *bundle.Document {
	t.Helper()
	files := map[string][]byte{
		"manifest.json":   []byte(`{"mode":"project","entryPoint":"docs/first.md","x-mdz-gui-pageOrder":["docs/first.md","second.md"]}`),
		"docs/first.md":   []byte("# First\n\n![画像](../images/test.png)\n\n---\n\n# First 2\n"),
		"second.md":       []byte("# Second\n"),
		"images/test.png": {1, 2, 3},
	}
	doc, err := bundle.FromFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestDocumentSlideConversionRoundTrip(t *testing.T) {
	original := sampleConversionDocument(t)
	slides, err := normalDocumentToSlides(original)
	if err != nil {
		t.Fatal(err)
	}
	if slides.Mode != "slides" {
		t.Fatalf("mode = %q", slides.Mode)
	}
	deck, err := slides.Deck()
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides) != 3 {
		t.Fatalf("slides = %d", len(deck.Slides))
	}
	if deck.Slides[0].File != "docs/first.md" || !strings.HasPrefix(deck.Slides[1].File, "docs/") {
		t.Fatalf("relative directory was not preserved: %+v", deck.Slides)
	}
	meta, body, ok := normalSlideSourceFromText(string(slides.Files[deck.Slides[1].File]))
	if !ok || meta.File != "docs/first.md" || meta.Page != 1 || meta.Part != 2 || meta.Parts != 2 || meta.Mode != "project" || !meta.Entry {
		t.Fatalf("source metadata = %+v, ok=%v", meta, ok)
	}
	if body != "# First 2\n" {
		t.Fatalf("body = %q", body)
	}

	restored, err := slideDocumentToNormal(slides)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Mode != "project" || restored.Entry != "docs/first.md" {
		t.Fatalf("restored mode/entry = %q / %q", restored.Mode, restored.Entry)
	}
	pages := restored.Pages()
	if len(pages) != 2 || pages[0] != "docs/first.md" || pages[1] != "second.md" {
		t.Fatalf("restored pages = %v", pages)
	}
	first := string(restored.Files["docs/first.md"])
	if strings.Contains(first, normalSlideSourcePrefix) || first != "# First\n\n![画像](../images/test.png)\n\n---\n\n# First 2\n" {
		t.Fatalf("restored first = %q", first)
	}
	if got := restored.Files["images/test.png"]; len(got) != 3 {
		t.Fatal("asset was not preserved")
	}
	if _, ok := restored.Files["slides.json"]; ok {
		t.Fatal("slides.json remained in normal document")
	}
}

func TestSlideToDocumentKeepsCurrentOrderAndNewPages(t *testing.T) {
	original := sampleConversionDocument(t)
	slides, err := normalDocumentToSlides(original)
	if err != nil {
		t.Fatal(err)
	}
	deck, err := slides.Deck()
	if err != nil {
		t.Fatal(err)
	}

	// 元ファイルの1枚目を削除し、現在位置へ新しいスライドを追加します。
	deck.Slides = append(deck.Slides[:0], deck.Slides[1:]...)
	added := bundle.Slide{ID: "slide-added", File: "slides/slide-added.md", Title: "Added", Layout: "standard", FontSize: 32}
	deck.Slides = append(deck.Slides, bundle.Slide{})
	copy(deck.Slides[2:], deck.Slides[1:])
	deck.Slides[1] = added
	slides.Files[added.File] = []byte("# Added\n")
	data, err := json.MarshalIndent(deck, "", "\t")
	if err != nil {
		t.Fatal(err)
	}
	slides.Files["slides.json"] = data

	restored, err := slideDocumentToNormal(slides)
	if err != nil {
		t.Fatal(err)
	}
	pages := restored.Pages()
	want := []string{"docs/first.md", "slides/slide-added.md", "second.md"}
	if len(pages) != len(want) {
		t.Fatalf("pages = %v", pages)
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Fatalf("pages = %v, want %v", pages, want)
		}
	}
	if first := string(restored.Files["docs/first.md"]); first != "# First 2\n" {
		t.Fatalf("deleted slide was restored: %q", first)
	}
	if addedText := string(restored.Files["slides/slide-added.md"]); addedText != "# Added\n" {
		t.Fatalf("new page = %q", addedText)
	}
}

func TestDuplicateConvertedSlideBecomesNewNormalPage(t *testing.T) {
	doc, err := normalDocumentToSlides(sampleConversionDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	a.cfg.Editor = "builtin"
	a.session, err = workspace.New(a.base, doc, "", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.shutdown)

	info, err := a.Slides()
	if err != nil {
		t.Fatal(err)
	}
	original := info.Deck.Slides[0]
	info, err = a.ChangeSlide(info.Revision, original.ID, "duplicate", "")
	if err != nil {
		t.Fatal(err)
	}
	duplicate := info.Deck.Slides[1]
	text, err := a.Text(duplicate.File)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := normalSlideSourceFromText(text); ok {
		t.Fatal("duplicate retained source metadata")
	}

	restored, err := slideDocumentToNormal(a.session.Doc)
	if err != nil {
		t.Fatal(err)
	}
	pages := restored.Pages()
	if len(pages) != 3 {
		t.Fatalf("duplicate was merged into source page: %v", pages)
	}
	if pages[0] != "docs/first.md" || pages[1] != duplicate.File {
		t.Fatalf("duplicate position was not preserved: %v", pages)
	}
}

func TestNormalSlideSourceParserRejectsInvalidHeader(t *testing.T) {
	text := "<!-- mdz-gui:normal-source {\"version\":1,\"file\":\"../bad.md\",\"page\":1,\"part\":1,\"parts\":1,\"mode\":\"document\"} -->\n\n# Body\n"
	if _, body, ok := normalSlideSourceFromText(text); ok || body != text {
		t.Fatal("invalid metadata was accepted")
	}
}

func TestNativeSlidesBecomeSingleMarkdown(t *testing.T) {
	slides, err := bundle.NewSlides()
	if err != nil {
		t.Fatal(err)
	}
	deck, err := slides.Deck()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := slideDocumentToNormal(slides)
	if err != nil {
		t.Fatal(err)
	}
	pages := restored.Pages()
	if len(pages) != 1 || pages[0] != deck.Slides[0].File {
		t.Fatalf("native slide pages = %v", pages)
	}
	text := string(restored.Files[pages[0]])
	if got := strings.Count(text, "\n---\n"); got != len(deck.Slides)-1 {
		t.Fatalf("separator count = %d, want %d", got, len(deck.Slides)-1)
	}
}

func TestDocumentConversionSplitsEverySeparatorLine(t *testing.T) {
	files := map[string][]byte{
		"manifest.json": []byte(`{"mode":"document","entryPoint":"index.md"}`),
		"index.md":      []byte("# A\n```\n---\n```\n---\n# B\n"),
	}
	doc, err := bundle.FromFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	slides, err := normalDocumentToSlides(doc)
	if err != nil {
		t.Fatal(err)
	}
	deck, err := slides.Deck()
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides) != 2 {
		t.Fatalf("slides = %d, want 2", len(deck.Slides))
	}
	_, first, ok := normalSlideSourceFromText(string(slides.Files[deck.Slides[0].File]))
	if !ok || !strings.Contains(first, "```\n---\n```") {
		t.Fatalf("fenced separator was not preserved: %q", first)
	}
}
