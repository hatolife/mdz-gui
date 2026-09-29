package editor

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hatolife/mdz-gui/internal/settings"
)

func TestNativeEditingUndoAndWrite(t *testing.T) {
	binary := os.Getenv("MDZ_NVIM_TEST")
	if binary == "" {
		t.Skip("MDZ_NVIM_TESTでNeovimを指定してください")
	}
	root := t.TempDir()
	file := filepath.Join(root, "index.md")
	if err := os.WriteFile(file, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.md"), []byte("other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := settings.Default()
	cfg.NvimPath = binary
	cfg.InitMode = "clean"
	cfg.UndoLevels = 7
	var frames, writes atomic.Int64
	e, err := Start(root, t.TempDir(), cfg, func(name string, _ any) {
		if name == "native-redraw" {
			frames.Add(1)
		}
		if name == "native-save" {
			writes.Add(1)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.Open("index.md"); err != nil {
		t.Fatal(err)
	}
	if state, err := e.UndoState(); err != nil || state.CanUndo || state.CanRedo {
		t.Fatalf("initial undo state=%+v err=%v", state, err)
	}
	if err := e.Input("gg0i日本語<Esc>"); err != nil {
		t.Fatal(err)
	}
	waitText(t, e, "日本語original\n")
	if state, err := e.UndoState(); err != nil || !state.CanUndo || state.CanRedo {
		t.Fatalf("edited undo state=%+v err=%v", state, err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "日本語original\n" {
		t.Fatalf("checkpoint=%q err=%v", b, err)
	}
	if writes.Load() != 0 {
		t.Fatal("checkpoint recursively requested MDZ save")
	}
	if err := e.Open("other.md"); err != nil {
		t.Fatal(err)
	}
	if err := e.Open("index.md"); err != nil {
		t.Fatal(err)
	}
	if err := e.Undo(false); err != nil {
		t.Fatal(err)
	}
	waitText(t, e, "original\n")
	if state, err := e.UndoState(); err != nil || state.CanUndo || !state.CanRedo {
		t.Fatalf("undone state=%+v err=%v", state, err)
	}
	if err := e.Undo(true); err != nil {
		t.Fatal(err)
	}
	waitText(t, e, "日本語original\n")
	if err := e.Paste("![画像](images/test.png)"); err != nil {
		t.Fatal(err)
	}
	c, err := e.Current()
	if err != nil || !strings.Contains(c.Text, "![画像]") {
		t.Fatalf("paste=%+v %v", c, err)
	}
	if err := e.Undo(false); err != nil {
		t.Fatal(err)
	}
	waitText(t, e, "日本語original\n")
	if err := e.Undo(true); err != nil {
		t.Fatal(err)
	}
	if err := e.SetUndoLevels(3); err != nil {
		t.Fatal(err)
	}
	var levels int
	if err := e.Client.ExecLua("return vim.bo.undolevels", &levels); err != nil || levels != 3 {
		t.Fatalf("levels=%d err=%v", levels, err)
	}
	if err := e.Client.Command("write"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for writes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if writes.Load() == 0 || frames.Load() == 0 {
		t.Fatal("missing UI/save notification")
	}
}

func waitText(t *testing.T, e *Native, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := e.Current()
		if err != nil {
			t.Fatal(err)
		}
		if c.Text == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("text=%q want=%q", c.Text, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
