package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hatolife/mdz-gui/internal/bundle"
)

func TestUnsavedChoice(t *testing.T) {
	for _, choice := range []string{"discard", "save", "cancel"} {
		t.Run(choice, func(t *testing.T) {
			a := makeBook(t)
			const edited = "# 変更した内容\n"
			if err := a.Update(a.session.Doc.Entry, edited); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(t.TempDir(), "book.mdz")
			a.session.Filename = filename
			a.notify = func(event string, data any) {
				if event == "confirm-unsaved" {
					a.ResolveUnsaved(choice)
				}
			}
			if got := a.discardAllowed(); got != (choice != "cancel") {
				t.Fatalf("%s: allowed=%v", choice, got)
			}
			if choice == "save" {
				doc, err := bundle.Read(filename)
				if err != nil {
					t.Fatal(err)
				}
				if string(doc.Files[a.session.Doc.Entry]) != edited || a.State().Dirty {
					t.Fatal("保存内容または未保存状態が不正です")
				}
			} else if _, err := os.Stat(filename); !os.IsNotExist(err) {
				t.Fatal("保存を選んでいないのにファイルが作成されました")
			}
		})
	}
}

func TestUnsavedSaveFailureKeepsDocument(t *testing.T) {
	a := makeBook(t)
	a.session.Filename = filepath.Join(a.session.Content(), "invalid.mdz")
	reported := false
	a.notify = func(event string, data any) {
		if event == "confirm-unsaved" {
			a.ResolveUnsaved("save")
		}
		if event == "app-error" {
			reported = true
		}
	}
	if a.discardAllowed() || !reported || !a.State().Dirty {
		t.Fatal("保存失敗時は文書を保持し、エラーを表示する必要があります")
	}
}
