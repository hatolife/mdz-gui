package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hatolife/mdz-gui/internal/editor"
)

// Dependency は設定入力に対応する実行ファイルの存在確認結果です。
type Dependency struct {
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// CheckDependencies は設定を保存せずに確認し、必要な場合だけWSLを起動します。
func (a *App) CheckDependencies(nvimPath, initPath, mdbookPath string) []Dependency {
	result := []Dependency{}

	nvimCustom := strings.TrimSpace(nvimPath)
	launch, nvimErr := editor.Resolve(nvimCustom)
	nvim := Dependency{Name: "Neovim", Found: nvimErr == nil}
	if nvimErr == nil {
		nvim.Path = launch.Description()
		nvim.Message = "検出済み：" + nvim.Path
	} else {
		nvim.Message = "Neovimを確認できません：" + nvimErr.Error()
	}
	result = append(result, nvim)

	init := Dependency{Name: "init.lua"}
	if nvimErr != nil {
		init.Message = "Neovimを確認できないためinit.luaを検出できません。"
	} else {
		filename, found, err := launch.InitFile(initPath)
		init.Path = filename
		init.Found = found
		switch {
		case err != nil:
			init.Message = "init.luaを確認できません：" + err.Error()
		case found:
			init.Message = "検出済み：" + filename
		case filename != "":
			init.Message = "見つかりません：" + filename + "（設定なし（--clean）で起動します）"
		default:
			init.Message = "init.luaを検出できません（設定なし（--clean）で起動します）"
		}
	}
	result = append(result, init)

	custom := strings.TrimSpace(mdbookPath)
	command := custom
	if command == "" {
		command = "mdbook"
	}
	found, _ := exec.LookPath(command)
	found = findMdbook(custom)
	mdbook := Dependency{Name: "mdBook", Path: found}
	if found != "" {
		info, statErr := os.Stat(found)
		mdbook.Found = statErr == nil && !info.IsDir()
	}
	if mdbook.Found {
		if absolute, err := filepath.Abs(found); err == nil {
			mdbook.Path = absolute
		}
		mdbook.Message = "検出済み：" + mdbook.Path
	} else {
		mdbook.Path = ""
		if custom == "" {
			mdbook.Message = "見つかりません。インストールするか実行ファイルを指定してください。"
		} else {
			mdbook.Message = "指定した実行ファイルが見つからないか、実行できません：" + custom
		}
	}
	return append(result, mdbook)
}
