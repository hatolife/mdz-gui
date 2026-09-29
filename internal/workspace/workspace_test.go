package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/settings"
)

func TestWorkspaceSaveBackupsAndConflict(t *testing.T) {
	base := t.TempDir()
	cfg := settings.Default()
	cfg.BackupGenerations = 2
	s, err := New(base, bundle.New(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "document.mdz")
	for _, text := range []string{"one", "two", "three", "four"} {
		if err := s.Put("index.md", []byte(text)); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(base, file, "test", cfg); err != nil {
			t.Fatal(err)
		}
	}
	d, err := bundle.Read(file)
	if err != nil || string(d.Files["index.md"]) != "four" {
		t.Fatal("save failed", err)
	}
	var archives []string
	filepath.WalkDir(filepath.Join(base, "backups"), func(p string, e os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".mdz") {
			archives = append(archives, p)
		}
		return err
	})
	if len(archives) != 2 {
		t.Fatalf("generations=%d", len(archives))
	}
	for i, want := range []string{"two", "three"} {
		d, err := bundle.Read(archives[i])
		if err != nil || string(d.Files["index.md"]) != want {
			t.Fatalf("backup %d: %v", i, err)
		}
	}
	if err := os.WriteFile(file, []byte("externally changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(base, file, "test", cfg); err == nil {
		t.Fatal("overwrote external change")
	}
	if b, _ := os.ReadFile(file); string(b) != "externally changed" {
		t.Fatal("lost external data")
	}
}

func TestRecoveryAndImageRetention(t *testing.T) {
	base := t.TempDir()
	s, err := New(base, bundle.New(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("images/paste.png", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("index.md", []byte("![x](images/paste.png)")); err != nil {
		t.Fatal(err)
	}
	// 本文のUndoを再現し、画像を保持することを確認します。
	if err := s.Put("index.md", []byte("undone")); err != nil {
		t.Fatal(err)
	}
	s.PID = 0
	if err := s.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	list, err := Recoveries(base)
	if err != nil || len(list) != 1 {
		t.Fatal("recovery missing", err)
	}
	r, err := Recover(base, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Doc.Files["index.md"]) != "undone" || len(r.Doc.Files["images/paste.png"]) != 3 {
		t.Fatal("recovery lost content")
	}
	if !r.Dirty {
		t.Fatal("recovered workspace marked clean")
	}
	if _, err := Recover(base, "../escape"); err == nil {
		t.Fatal("unsafe recovery accepted")
	}
}

func TestPortablePathsAndSymlinks(t *testing.T) {
	for _, name := range []string{"../x.md", "CON.md", "images/AUX.png", "a./x.md", "x /x.md"} {
		if PortablePath(name) {
			t.Fatal("accepted", name)
		}
	}
	base := t.TempDir()
	d := bundle.New()
	d.Files["INDEX.md"] = []byte("duplicate")
	if _, err := New(base, d, "", false); err == nil {
		t.Fatal("case collision accepted")
	}
	s, err := New(base, bundle.New(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(s.Content(), "link.md")); err != nil {
		t.Skip("symlink unavailable")
	}
	if err := s.Capture(); err == nil {
		t.Fatal("symlink accepted")
	}
}
