package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLimitsAndDefaults(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
	if Default().InitMode != "custom" {
		t.Fatalf("default init mode = %q", Default().InitMode)
	}
	for _, n := range []int{0, -1, 10001} {
		s := Default()
		s.UndoLevels = n
		if err := s.Validate(); err == nil {
			t.Fatalf("accepted undo limit %d", n)
		}
	}
	for _, path := range []string{"../outside", "/absolute", "C:/absolute", "images\\windows"} {
		s := Default()
		s.ImageDirectory = path
		if err := s.Validate(); err == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
	s := Default()
	s.ImageName = "{unknown}"
	if err := s.Validate(); err == nil {
		t.Fatal("unknown token accepted")
	}
	file := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(file, []byte(`{"undoLevels":25,"imageDirectory":"assets/pasted"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if s.UndoLevels != 25 || s.AutoSaveSeconds != 60 || s.ImageDirectory != "assets/pasted" {
		t.Fatalf("wrong defaults: %+v", s)
	}

	if err := os.WriteFile(file, []byte(`{"initMode":"user","undoLevels":25,"imageDirectory":"assets/pasted"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err = Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if s.InitMode != "custom" || s.InitPath != "" {
		t.Fatalf("legacy init mode was not migrated: %+v", s)
	}
}
