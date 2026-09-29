package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archive(t *testing.T, files map[string]string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "sample.mdz")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, data := range files {
		out, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestEntryPoint(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		entry string
		fails bool
	}{
		{"index", map[string]string{"index.md": "# Main", "other.md": "Other"}, "index.md", false},
		{"single", map[string]string{"日本語.markdown": "本文"}, "日本語.markdown", false},
		{"explicit", map[string]string{"manifest.json": `{"entryPoint":"pages/main.md","mode":"project"}`, "pages/main.md": "# Main", "index.md": "Other"}, "pages/main.md", false},
		{"ambiguous", map[string]string{"a.md": "a", "b.md": "b"}, "", true},
		{"nested only", map[string]string{"pages/main.md": "a"}, "", true},
		{"missing", map[string]string{"manifest.json": `{"entryPoint":"missing.md"}`, "index.md": "a"}, "", true},
		{"future", map[string]string{"manifest.json": `{"spec":{"version":"2.0.0"}}`, "index.md": "a"}, "", true},
		{"future minor", map[string]string{"manifest.json": `{"spec":{"version":"1.99.0"}}`, "index.md": "a"}, "index.md", false},
		{"unknown mode", map[string]string{"manifest.json": `{"mode":"unknown"}`, "index.md": "a"}, "", true},
		{"traversal", map[string]string{"../evil.md": "a", "index.md": "a"}, "", true},
		{"backslash", map[string]string{"images\\a.png": "a", "index.md": "a"}, "", true},
		{"utf8", map[string]string{"index.md": string([]byte{0xff})}, "", true},
		{"null manifest", map[string]string{"manifest.json": "null", "index.md": "a"}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := Read(archive(t, c.files))
			if c.fails {
				if err == nil {
					t.Fatal("expected rejection")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.Entry != c.entry {
				t.Fatalf("entry=%q", d.Entry)
			}
		})
	}
}

func TestRoundTripPreservesAssetsAndMetadata(t *testing.T) {
	filename := archive(t, map[string]string{
		"index.md": "# 文書\r\n", "chapters/next.md": "![画像](../images/日本語.png)",
		"images/日本語.png": string([]byte{0, 1, 2, 255}), "attachment.bin": "opaque",
		"manifest.json": `{"title":"題名","extension":{"keep":true},"created":"2020-01-01T00:00:00Z","producer":{"custom":123}}`,
	})
	d, err := Read(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Put("index.md", []byte("# 変更\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(filename, "test"); err != nil {
		t.Fatal(err)
	}
	loaded, err := Read(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.Files["index.md"]) != "# 変更\n" {
		t.Fatal("text not persisted")
	}
	if !bytes.Equal(loaded.Files["images/日本語.png"], []byte{0, 1, 2, 255}) {
		t.Fatal("image changed")
	}
	if string(loaded.Files["attachment.bin"]) != "opaque" {
		t.Fatal("attachment lost")
	}
	if string(loaded.Manifest["extension"]) != `{\n\t\t"keep": true\n\t}` {
		var v map[string]bool
		if json.Unmarshal(loaded.Manifest["extension"], &v) != nil || !v["keep"] {
			t.Fatal("unknown metadata lost")
		}
	}
	if string(loaded.Manifest["created"]) != `"2020-01-01T00:00:00Z"` {
		t.Fatal("created changed")
	}
}

func TestLimitsAndFailedSave(t *testing.T) {
	d := New()
	if err := d.Put("large.md", bytes.Repeat([]byte{'a'}, MaxFile+1)); err == nil {
		t.Fatal("large content accepted")
	}
	if err := d.Put("../escape.md", []byte("a")); err == nil {
		t.Fatal("unsafe path accepted")
	}
	filename := filepath.Join(t.TempDir(), "missing", "document.mdz")
	if err := d.Write(filename, "test"); err == nil {
		t.Fatal("save should fail")
	}
	if string(d.Files["index.md"]) == "" {
		t.Fatal("failed save lost content")
	}
}

func TestDecompressionLimit(t *testing.T) {
	filename := archive(t, map[string]string{"index.md": strings.Repeat("a", MaxFile+1)})
	if _, err := Read(filename); err == nil {
		t.Fatal("oversized ZIP accepted")
	}
}
