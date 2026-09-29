package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/settings"
	"github.com/hatolife/mdz-gui/internal/workspace"
)

func slideApp(t *testing.T) *App {
	t.Helper()
	d, err := newDocument("slides")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	a.cfg.Editor = "builtin"
	a.session, err = workspace.New(a.base, d, "", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.shutdown)
	return a
}

func TestSlidesRoundTripAndStructure(t *testing.T) {
	a := slideApp(t)
	info, err := a.Slides()
	if err != nil {
		t.Fatal(err)
	}
	initialCount := len(info.Deck.Slides)
	original := info.Deck.Slides[0]
	text := "# 画像付き\n\n![画像](../images/test.png)\n\n---\n\n水平線です。\n"
	if err = a.Update(original.File, text); err != nil {
		t.Fatal(err)
	}
	if err = a.session.Put("images/test.png", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	info.Deck.Slides[0].Notes = "発表用のメモ"
	info, err = a.ConfigureSlides(info.Revision, info.Deck)
	if err != nil {
		t.Fatal(err)
	}
	info, err = a.ChangeSlide(info.Revision, original.ID, "duplicate", "")
	if err != nil {
		t.Fatal(err)
	}
	copySlide := info.Deck.Slides[1]
	if got, _ := a.Text(copySlide.File); got != text || copySlide.Notes != "発表用のメモ" {
		t.Fatal("copy lost text or notes")
	}
	oldRevision := info.Revision
	info, err = a.ChangeSlide(info.Revision, copySlide.ID, "up", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.State().Pages[0] != copySlide.File || a.State().Entry != copySlide.File {
		t.Fatal("order not reflected")
	}
	if _, err = a.ChangeSlide(oldRevision, copySlide.ID, "remove", ""); err == nil {
		t.Fatal("stale edit accepted")
	}
	info, err = a.ChangeSlide(info.Revision, original.ID, "remove", "")
	if err != nil {
		t.Fatal(err)
	}
	info, err = a.ChangeSlide(info.Revision, "", "undo", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Deck.Slides) != initialCount+1 {
		t.Fatal("undo did not restore")
	}
	info, err = a.ChangeSlide(info.Revision, "", "redo", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Deck.Slides) != initialCount {
		t.Fatal("redo failed")
	}
	info.Deck.Title = "日本語の資料"
	info.Deck.Theme = "dark"
	info.Deck.Aspect = "4:3"
	info, err = a.ConfigureSlides(info.Revision, info.Deck)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "slides.mdz")
	if err = a.saveLocked(filename); err != nil {
		t.Fatal(err)
	}
	d, err := bundle.Read(filename)
	if err != nil {
		t.Fatal(err)
	}
	deck, err := d.Deck()
	if err != nil {
		t.Fatal(err)
	}
	if deck.Title != "日本語の資料" || deck.Theme != "dark" || deck.Aspect != "4:3" || deck.Slides[0].ID != copySlide.ID {
		t.Fatalf("round trip: %+v", deck)
	}
	if string(d.Files[copySlide.File]) != text || len(d.Files["images/test.png"]) != 3 {
		t.Fatal("round trip lost assets")
	}
	if _, ok := d.Files["vendor/reveal.esm.js"]; ok {
		t.Fatal("renderer should not be saved into document")
	}
	// 編集後に異常終了した状況でも、構成と本文を復元します。
	a.session.PID = 0
	if err = a.session.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	r, err := workspace.Recover(a.base, a.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.Doc.Mode != "slides" || r.Doc.Pages()[0] != copySlide.File {
		t.Fatal("recovery lost deck")
	}
}

func TestSlideImportAndSafeRendering(t *testing.T) {
	indented := "\tcode\n\n\t---\n\n    code\n"
	if got := splitSlideMarkdown(indented, "---"); len(got) != 1 || got[0] != indented {
		t.Fatalf("indented code changed: %q", got)
	}
	text := "# One\n\n---\n\n# Two\n\n````markdown\n---\n```\n---\n````\n\n---\n\n# Three\n\n    ---\n\nHeading\n---\n"
	parts := splitSlideMarkdown(text, "---")
	if len(parts) != 3 || !strings.Contains(parts[1], "````") || !strings.Contains(parts[2], "Heading\n---") {
		t.Fatalf("bad split: %q", parts)
	}
	a := slideApp(t)
	info, _ := a.Slides()
	initialCount := len(info.Deck.Slides)
	info, err := a.ChangeSlide(info.Revision, info.Deck.Slides[0].ID, "import", text)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Deck.Slides) != initialCount+3 || info.Deck.Slides[2].Title != "Two" {
		t.Fatal("import did not preserve order")
	}
	html, err := a.RenderSlide("# 左\n\n<!-- column -->\n\n# 右\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1))", "columns")
	if err != nil || !strings.Contains(html, "slide-columns") || strings.Contains(html, "<script>") || strings.Contains(html, "href=\"javascript:") {
		t.Fatalf("unsafe rendering: %s, %v", html, err)
	}
	info, _ = a.Slides()
	if _, err = a.ChangeSlide(info.Revision, "", "import", "---\ntheme: test\n---\n\n# body"); err == nil {
		t.Fatal("frontmatter accepted")
	}
}

func TestSlideSplitKeepsOriginalForUndo(t *testing.T) {
	a := slideApp(t)
	info, err := a.Slides()
	if err != nil {
		t.Fatal(err)
	}
	source := info.Deck.Slides[0]
	text := "# First\n---\n# Second\n\n```md\n---\n```\n"
	if err = a.Update(source.File, text); err != nil {
		t.Fatal(err)
	}
	info, err = a.ChangeSlide(info.Revision, source.ID, "split", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Deck.Slides) != 9 {
		t.Fatalf("split count: %d", len(info.Deck.Slides))
	}
	if info.Deck.Slides[0].File == source.File {
		t.Fatal("split overwrote original source path")
	}
	first, _ := a.Text(info.Deck.Slides[0].File)
	second, _ := a.Text(info.Deck.Slides[1].File)
	if first != "# First\n" || !strings.Contains(second, "# Second") || !strings.Contains(second, "```md\n---\n```") {
		t.Fatalf("split text: first=%q second=%q", first, second)
	}
	original, _ := a.Text(source.File)
	if original != text {
		t.Fatal("original source was changed")
	}
	info, err = a.ChangeSlide(info.Revision, "", "undo", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Deck.Slides[0].File != source.File {
		t.Fatal("undo did not restore original source")
	}
}

func TestSlideValidationAndExternalEdit(t *testing.T) {
	a := slideApp(t)
	info, _ := a.Slides()
	bad := info.Deck
	bad.Slides = append([]bundle.Slide(nil), info.Deck.Slides...)
	bad.Slides[0].File = "../outside.md"
	if _, err := a.ConfigureSlides(info.Revision, bad); err == nil {
		t.Fatal("path mutation accepted")
	}
	bad = info.Deck
	bad.Theme = "injected\" onload=\"alert(1)"
	if _, err := a.ConfigureSlides(info.Revision, bad); err == nil {
		t.Fatal("invalid theme accepted")
	}
	bad = info.Deck
	bad.ContentMarginX = 181
	if _, err := a.ConfigureSlides(info.Revision, bad); err == nil {
		t.Fatal("invalid horizontal margin accepted")
	}
	bad = info.Deck
	bad.ContentMarginY = 7
	if _, err := a.ConfigureSlides(info.Revision, bad); err == nil {
		t.Fatal("invalid vertical margin accepted")
	}
	bad = info.Deck
	bad.FontFamily = "system"
	bad.BodyFontSize = 32
	bad.H1FontSize, bad.H2FontSize, bad.H3FontSize, bad.H4FontSize, bad.H5FontSize = 40, 42, 36, 33, 29
	if _, err := a.ConfigureSlides(info.Revision, bad); err == nil {
		t.Fatal("invalid heading order accepted")
	}
	bad = info.Deck
	bad.Version = 99
	if _, err := a.ConfigureSlides(info.Revision, bad); err == nil {
		t.Fatal("unknown version accepted")
	}
	info, err := a.ChangeSlide(info.Revision, info.Deck.Slides[0].ID, "add", "")
	if err != nil {
		t.Fatal(err)
	}
	if !info.CanUndo {
		t.Fatal("missing undo")
	}
	external := info.Deck
	external.Title = "external"
	data, _ := json.Marshal(external)
	if err = os.WriteFile(filepath.Join(a.session.Content(), "slides.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.session.Capture(); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ChangeSlide(info.Revision, "", "undo", ""); err == nil {
		t.Fatal("external edit overwritten")
	}
	latest, _ := a.Slides()
	if latest.CanUndo || latest.Deck.Title != "external" {
		t.Fatal("external edit was lost")
	}
	files := map[string][]byte{"manifest.json": []byte(`{"mode":"slides","entryPoint":"index.md"}`), "index.md": []byte("# x")}
	if _, err = bundle.FromFiles(files); err == nil {
		t.Fatal("missing deck accepted")
	}
}
