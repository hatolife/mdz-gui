package main

import (
	"log"
	"os"

	"github.com/alexflint/go-arg"
)

type Args struct {
	Version  bool   `arg:"-v,--version" help:"バージョンを表示して終了します。"`
	Audience string `arg:"--audience" help:"投影専用ウィンドウの接続先（内部用）。"`
	File     string `arg:"positional" help:"開くMDZまたはMarkdownファイル。"`
}

func ParseArgs() Args {
	var args Args
	parser, err := arg.NewParser(arg.Config{Program: "mdz-gui", Out: os.Stdout}, &args)
	if err != nil {
		log.Fatal(err)
	}
	parser.MustParse(os.Args[1:])
	return args
}

func isCLIInfoRequest(args []string) bool {
	for _, argument := range args {
		switch argument {
		case "-h", "--help", "-v", "--version":
			return true
		}
	}
	return false
}
