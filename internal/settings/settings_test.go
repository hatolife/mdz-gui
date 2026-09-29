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
	if Default().StartupWithoutFile != "welcome" || Default().StartupWithFile != "view" {
		t.Fatalf("default startup = %q / %q", Default().StartupWithoutFile, Default().StartupWithFile)
	}
	for _, value := range []string{"", "unknown"} {
		s := Default()
		s.StartupWithoutFile = value
		if err := s.Validate(); err == nil {
			t.Fatalf("accepted startup without file %q", value)
		}
		s = Default()
		s.StartupWithFile = value
		if err := s.Validate(); err == nil {
			t.Fatalf("accepted startup with file %q", value)
		}
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
	if s.UndoLevels != 25 || s.AutoSaveSeconds != 60 || s.ImageDirectory != "assets/pasted" || s.StartupWithoutFile != "welcome" || s.StartupWithFile != "view" {
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
