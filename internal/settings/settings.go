// Package settings はユーザー設定と保存時の入力検証を扱います。
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hatolife/mdz-gui/internal/bundle"
)

type Settings struct {
	MdbookPath        string `json:"mdbookPath"`
	MdbookDeclined    bool   `json:"mdbookDeclined"`
	Theme             string `json:"theme"`
	Accent            string `json:"accent"`
	Editor            string `json:"editor"`
	NvimPath          string `json:"nvimPath"`
	InitMode          string `json:"initMode"`
	InitPath          string `json:"initPath"`
	UndoLevels        int    `json:"undoLevels"`
	FontFamily        string `json:"fontFamily"`
	FontSize          int    `json:"fontSize"`
	ImageDirectory    string `json:"imageDirectory"`
	ImageName         string `json:"imageName"`
	AutoSave          bool   `json:"autoSave"`
	AutoSaveSeconds   int    `json:"autoSaveSeconds"`
	BackupGenerations int    `json:"backupGenerations"`
	BackupMiB         int    `json:"backupMiB"`
}

func Default() Settings {
	return Settings{Theme: "system", Accent: "blue", Editor: "neovim", InitMode: "custom", UndoLevels: 1000, FontFamily: "Consolas, 'Yu Gothic', monospace", FontSize: 15, ImageDirectory: "images", ImageName: "{date}-{time}-{counter}", AutoSave: false, AutoSaveSeconds: 60, BackupGenerations: 10, BackupMiB: 512}
}

func (s Settings) Validate() error {
	if s.Theme != "system" && s.Theme != "light" && s.Theme != "dark" {
		return fmt.Errorf("テーマが不正です")
	}
	switch s.Accent {
	case "blue", "green", "purple", "orange", "gray":
	default:
		return fmt.Errorf("アクセント色が不正です")
	}
	if s.Editor != "neovim" && s.Editor != "builtin" {
		return fmt.Errorf("編集方式が不正です")
	}
	if s.InitMode != "clean" && s.InitMode != "custom" {
		return fmt.Errorf("Neovim設定の種類が不正です")
	}
	if s.UndoLevels < 1 || s.UndoLevels > 10000 {
		return fmt.Errorf("Undo保持数は1〜10000で指定してください")
	}
	if s.FontSize < 10 || s.FontSize > 40 || len(s.FontFamily) > 200 {
		return fmt.Errorf("フォントサイズは10〜40で指定してください")
	}
	if !bundle.ValidPath(s.ImageDirectory) {
		return fmt.Errorf("画像保存先は文書内の相対ディレクトリを指定してください")
	}
	name := s.ImageName
	for _, token := range []string{"{date}", "{time}", "{counter}"} {
		name = strings.ReplaceAll(name, token, "1")
	}
	if !bundle.ValidPath(name) || strings.ContainsAny(name, "/{}") {
		return fmt.Errorf("画像名には{date}・{time}・{counter}とファイル名に使える文字を指定してください")
	}
	if s.AutoSaveSeconds < 5 || s.AutoSaveSeconds > 86400 {
		return fmt.Errorf("自動保存間隔は5〜86400秒で指定してください")
	}
	if s.BackupGenerations < 0 || s.BackupGenerations > 1000 || s.BackupMiB < 1 || s.BackupMiB > 102400 {
		return fmt.Errorf("バックアップは0〜1000世代、容量は1〜102400MiBで指定してください")
	}
	return nil
}

func Load(filename string) (Settings, error) {
	s := Default()
	b, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	// 旧「普段の設定」は、標準位置のinit.luaを指定する設定へ移行します。
	if s.InitMode == "user" {
		s.InitMode = "custom"
		s.InitPath = ""
	}
	return s, s.Validate()
}
