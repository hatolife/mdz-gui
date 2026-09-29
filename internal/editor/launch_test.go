package editor

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestResolveNeovimOrder(t *testing.T) {
	for _, tc := range []struct {
		name, custom, platform                    string
		windows, wsl, probeOK, wantWSL, wantError bool
	}{
		{"native first", "", "windows", true, true, true, false, false},
		{"default WSL", "", "windows", false, true, true, true, false},
		{"no WSL", "", "windows", false, false, false, false, true},
		{"unconfigured WSL", "", "windows", false, true, false, false, true},
		{"explicit missing", "C:/missing/nvim.exe", "windows", false, true, true, false, true},
		{"Linux missing", "", "linux", false, true, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probed := false
			launch, err := resolve(tc.custom, tc.platform, func(command string) (string, error) {
				if command == "nvim" && tc.windows {
					return "C:/Neovim/nvim.exe", nil
				}
				if command == "wsl.exe" && tc.wsl {
					return "C:/Windows/System32/wsl.exe", nil
				}
				return "", errors.New("not found")
			}, func(command string, args ...string) (string, error) {
				probed = true
				if !reflect.DeepEqual(args[:3], []string{"--exec", "sh", "-c"}) || strings.Contains(args[3], "-d ") || strings.Contains(args[3], "-u ") {
					t.Fatalf("default distro/user: %q", args)
				}
				if !tc.probeOK {
					return "", errors.New("no distro")
				}
				return "login message\nMDZ_NVIM=/home/user/.local/bin/nvim\nMDZ_PATH=/home/user/.local/bin:/usr/bin\n", nil
			})
			if (err != nil) != tc.wantError || launch.WSL != tc.wantWSL {
				t.Fatalf("launch=%+v err=%v", launch, err)
			}
			if probed != (tc.platform == "windows" && tc.custom == "" && !tc.windows && tc.wsl) {
				t.Fatal("unexpected probe")
			}
			if tc.wantWSL {
				got := launch.Args([]string{"--embed", "-u", "/mnt/c/a b/init.lua"})
				want := []string{"--exec", "env", "PATH=/home/user/.local/bin:/usr/bin", "/home/user/.local/bin/nvim", "--embed", "-u", "/mnt/c/a b/init.lua"}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("args=%q", got)
				}
			}
		})
	}
}

func TestWSLFilePath(t *testing.T) {
	e := Native{Root: "/mnt/c/日本語 文書", wsl: true}
	if got := e.filePath("slides/first.md"); got != "/mnt/c/日本語 文書/slides/first.md" {
		t.Fatal(got)
	}
}
