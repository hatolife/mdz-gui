package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/settings"
)

func TestSingleMarkdownOpenOverwriteAndConvert(t *testing.T) {
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "メモ.md")
	if err := os.WriteFile(source, []byte("# 元の本文\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	defer a.shutdown()

	opened, err := a.Open(source)
	if err != nil || !opened {
		t.Fatalf("open: opened=%v err=%v", opened, err)
	}
	state := a.State()
	if !state.SingleMarkdown {
		t.Fatal("単一Markdownモードになっていません")
	}
	if state.Dirty {
		t.Fatal("開いただけのMarkdownが未保存扱いです")
	}
	if state.Filename != source || state.Entry != "メモ.md" {
		t.Fatalf("source identity lost: filename=%q entry=%q", state.Filename, state.Entry)
	}
	if got := a.LastDocument(); got != source {
		t.Fatalf("last document = %q, want %q", got, source)
	}
	if len(state.Pages) != 1 || state.Pages[0] != state.Entry {
		t.Fatalf("unexpected pages: %#v", state.Pages)
	}
	if err := a.AddPage("追加.md"); err == nil {
		t.Fatal("単一Markdownでページを追加できました")
	}
	a.mu.Lock()
	_, imageErr := a.storeImageLocked([]byte("image"), ".png")
	a.mu.Unlock()
	if imageErr == nil {
		t.Fatal("単一Markdownで画像を同梱できました")
	}

	if err := a.Update(state.Entry, "# 更新した本文\n"); err != nil {
		t.Fatal(err)
	}
	saved, err := a.Save(false)
	if err != nil || !saved {
		t.Fatalf("overwrite: saved=%v err=%v", saved, err)
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# 更新した本文\n" {
		t.Fatalf("unexpected markdown: %q", body)
	}
	if !a.State().SingleMarkdown {
		t.Fatal("Markdown上書き後に単一モードを失いました")
	}

	if err := a.Update(state.Entry, "# MDZへ変換\n"); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "メモ.mdz")
	a.mu.Lock()
	err = a.saveLocked(destination)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if a.State().SingleMarkdown {
		t.Fatal("MDZ保存後も単一Markdownモードのままです")
	}
	doc, err := bundle.Read(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(doc.Files["メモ.md"]) != "# MDZへ変換\n" {
		t.Fatalf("unexpected mdz body: %q", doc.Files["メモ.md"])
	}
}

func TestOpenInNewWindowUsesDocumentPath(t *testing.T) {
	directory := t.TempDir()
	markdown := filepath.Join(directory, "other.md")
	if err := os.WriteFile(markdown, []byte("# 別の文書\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mdz := filepath.Join(directory, "other.mdz")
	if err := os.WriteFile(mdz, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	var launched []string
	a := &App{launchWindow: func(filename string) error {
		launched = append(launched, filename)
		return nil
	}}
	for _, source := range []string{markdown, mdz} {
		if err := a.OpenInNewWindow(source); err != nil {
			t.Fatal(err)
		}
	}
	if len(launched) != 2 {
		t.Fatalf("unexpected launched files: %#v", launched)
	}
	for i, source := range []string{markdown, mdz} {
		absolute, err := filepath.Abs(source)
		if err != nil {
			t.Fatal(err)
		}
		if launched[i] != absolute {
			t.Fatalf("unexpected launch path: got=%q want=%q", launched[i], absolute)
		}
	}

	unsupported := filepath.Join(directory, "other.txt")
	if err := os.WriteFile(unsupported, []byte("text\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenInNewWindow(unsupported); err == nil {
		t.Fatal("MDZまたはMarkdown以外を別ウィンドウで開けました")
	}
}

func TestSingleMarkdownDetectsExternalChange(t *testing.T) {
	source := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(source, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &App{base: t.TempDir(), cfg: settings.Default()}
	defer a.shutdown()
	if opened, err := a.Open(source); err != nil || !opened {
		t.Fatalf("open: opened=%v err=%v", opened, err)
	}
	if err := a.Update(a.State().Entry, "edited\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("external\n"), 0600); err != nil {
		t.Fatal(err)
	}
	saved, err := a.Save(false)
	if err == nil || saved || !strings.Contains(err.Error(), "外部") {
		t.Fatalf("external change not detected: saved=%v err=%v", saved, err)
	}
	body, readErr := os.ReadFile(source)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "external\n" {
		t.Fatalf("external file was overwritten: %q", body)
	}
}


func TestSingleMarkdownRelativeImages(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(docs, "images"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(docs, "note.md")
	if err := os.WriteFile(source, []byte("![nested](images/pixel.png)\n![parent](../shared.webp)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	nested := []byte("nested-image")
	parent := []byte("parent-image")
	if err := os.WriteFile(filepath.Join(docs, "images", "pixel.png"), nested, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shared.webp"), parent, 0600); err != nil {
		t.Fatal(err)
	}

	a := &App{base: t.TempDir(), cfg: settings.Default()}
	defer a.shutdown()
	if opened, err := a.Open(source); err != nil || !opened {
		t.Fatalf("open: opened=%v err=%v", opened, err)
	}

	check := func(reference, contentType string, want []byte) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/local-image?path="+url.QueryEscape(reference), nil)
		response := httptest.NewRecorder()
		a.serveAsset(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status=%d", reference, response.Code)
		}
		if response.Header().Get("Content-Type") != contentType {
			t.Fatalf("%s: content-type=%q", reference, response.Header().Get("Content-Type"))
		}
		if string(response.Body.Bytes()) != string(want) {
			t.Fatalf("%s: body=%q", reference, response.Body.Bytes())
		}
	}
	check("images/pixel.png", "image/png", nested)
	check("../shared.webp", "image/webp", parent)

	for _, reference := range []string{"/absolute.png", "secret.txt", "https://example.com/image.png"} {
		request := httptest.NewRequest(http.MethodGet, "/local-image?path="+url.QueryEscape(reference), nil)
		response := httptest.NewRecorder()
		a.serveAsset(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: unexpected status=%d", reference, response.Code)
		}
	}
}
