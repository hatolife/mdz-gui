package editor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/hatolife/mdz-gui/internal/process"
)

// Launch はWindowsまたは既定のWSLで使用するNeovimの起動情報です。
type Launch struct {
	Command    string
	WSL        bool
	binary     string
	searchPath string
}

// Resolve は明示指定を優先し、WindowsのPATHにない場合だけWSLを探します。
func Resolve(custom string) (Launch, error) {
	return resolve(custom, runtime.GOOS, exec.LookPath, runProbe)
}

func runProbe(command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	output, err := process.CommandContext(ctx, command, args...).Output()
	return string(output), err
}

func resolve(custom, platform string, lookPath func(string) (string, error), probe func(string, ...string) (string, error)) (Launch, error) {
	command := strings.TrimSpace(custom)
	if command == "" {
		command = "nvim"
	}
	found, err := lookPath(command)
	if err == nil {
		return Launch{Command: found}, nil
	}
	if custom != "" || platform != "windows" {
		return Launch{}, err
	}
	wsl, wslErr := lookPath("wsl.exe")
	if wslErr != nil {
		return Launch{}, fmt.Errorf("WindowsのPATHにもWSLにもNeovimが見つかりません: %w", err)
	}
	// -dと-uを省略し、既定ユーザーのログイン対話シェルのPATHを調べます。
	output, wslErr := probe(wsl, "--exec", "sh", "-c", `exec "${SHELL:-/bin/sh}" -lic 'printf "\nMDZ_NVIM="; command -v nvim; printf "MDZ_PATH="; printenv PATH'`)
	if wslErr != nil {
		return Launch{}, fmt.Errorf("既定のWSLでNeovimを確認できません: %w", wslErr)
	}
	var binary, searchPath string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "MDZ_NVIM=") {
			binary = strings.TrimPrefix(line, "MDZ_NVIM=")
		}
		if strings.HasPrefix(line, "MDZ_PATH=") {
			searchPath = strings.TrimPrefix(line, "MDZ_PATH=")
		}
	}
	if !strings.HasPrefix(binary, "/") || strings.ContainsAny(binary, "\r\x00") || searchPath == "" {
		return Launch{}, fmt.Errorf("既定のWSLのPATHにNeovimが見つかりません")
	}
	return Launch{Command: wsl, WSL: true, binary: binary, searchPath: searchPath}, nil
}

// InitFile はNeovimが使用するinit.luaの候補と存在状態を返します。
// configuredが空欄ならNeovim自身のstdpath("config")から標準位置を求めます。
func (l Launch) InitFile(configured string) (string, bool, error) {
	display, runtimePath, err := l.initFilePath(configured)
	if err != nil {
		return display, false, err
	}
	found, err := l.initFileExists(runtimePath)
	return display, found, err
}

func (l Launch) initFilePath(configured string) (string, string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		found, err := l.defaultInitPath()
		return found, found, err
	}
	if !l.WSL || strings.HasPrefix(configured, "/") {
		return configured, configured, nil
	}
	converted, err := l.Path(configured)
	return configured, converted, err
}

func (l Launch) defaultInitPath() (string, error) {
	args := l.Args([]string{
		"--headless", "--clean",
		"-c", `lua io.write(vim.fn.stdpath("config") .. "/init.lua")`,
		"-c", "qa!",
	})
	output, err := runProbe(l.Command, args...)
	if err != nil {
		return "", fmt.Errorf("init.luaの標準位置を確認できません: %w", err)
	}
	filename := strings.TrimSpace(output)
	if filename == "" || strings.ContainsAny(filename, "\r\n\x00") {
		return "", fmt.Errorf("init.luaの標準位置が不正です")
	}
	return filename, nil
}

func (l Launch) initFileExists(filename string) (bool, error) {
	if !l.WSL {
		info, err := os.Stat(filename)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return info.Mode().IsRegular(), nil
	}
	output, err := runProbe(l.Command, "--exec", "sh", "-c", `if [ -f "$1" ]; then printf 1; else printf 0; fi`, "sh", filename)
	if err != nil {
		return false, fmt.Errorf("WSL上のinit.luaを確認できません: %w", err)
	}
	return strings.TrimSpace(output) == "1", nil
}

func (l Launch) Path(name string) (string, error) {
	if !l.WSL {
		return name, nil
	}
	output, err := runProbe(l.Command, "--exec", "wslpath", "-a", "-u", name)
	if err != nil {
		return "", fmt.Errorf("WSL用パスに変換できません: %s: %w", name, err)
	}
	converted := strings.TrimRight(output, "\r\n")
	if !strings.HasPrefix(converted, "/") {
		return "", fmt.Errorf("WSL用パスが不正です: %s", name)
	}
	return converted, nil
}

func (l Launch) Args(args []string) []string {
	if !l.WSL {
		return args
	}
	// シェルの起動メッセージがRPCへ混入しないように直接起動します。
	return append([]string{"--exec", "env", "PATH=" + l.searchPath, l.binary}, args...)
}

func (l Launch) Description() string {
	if l.WSL {
		return "WSL（既定）: " + l.binary
	}
	return l.Command
}
