package main

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/alexflint/go-arg"
	"github.com/hatolife/mdz-gui/internal/bundle"
)

type Args struct {
	Source string `arg:"--source" default:"docs/help" help:"ヘルプのソースディレクトリ。"`
	Output string `arg:"--output" default:"build/bin/mdz-gui-help.mdz" help:"出力するMDZ。"`
}

func init() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lshortfile)
}

func main() {
	args := ParseArgs()
	if err := packageHelp(args); err != nil {
		log.Fatal(err)
	}
}

func ParseArgs() Args {
	var args Args
	arg.MustParse(&args)
	return args
}

// packageHelp はヘルプのソース一式をMDZにまとめます。
func packageHelp(args Args) error {
	files := map[string][]byte{}
	root := os.DirFS(args.Source)
	if err := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(root, name)
		if err == nil {
			files[name] = data
		}
		return err
	}); err != nil {
		return err
	}
	doc, err := bundle.FromFiles(files)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(args.Output), 0755); err != nil {
		return err
	}
	return doc.Write(args.Output, "dev")
}
