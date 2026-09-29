package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// RenameBook は本のタイトルだけを変更し、他の設定とコメントを保持します。
func (a *App) RenameBook(revision, title string) error {
	config, err := a.GetBookConfiguration()
	if err != nil {
		return err
	}
	if config.Revision != revision {
		return fmt.Errorf("設定が変更されました。開き直してください")
	}
	text, err := replaceBookTitle([]byte(config.Text), title)
	if err != nil {
		return err
	}
	return a.SaveBookConfiguration(revision, string(text))
}

// replaceBookTitle はTOMLの構文位置を使って値を置き換えます。
func replaceBookTitle(data []byte, title string) ([]byte, error) {
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("本のタイトルを入力してください")
	}
	var config map[string]any
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	encoded, err := toml.Marshal(map[string]string{"title": title})
	if err != nil {
		return nil, err
	}
	value := bytes.TrimSpace(encoded[bytes.IndexByte(encoded, '=')+1:])
	var parser unstable.Parser
	parser.Reset(data)
	var table []string
	start, end, insert := -1, -1, -1
	inlineInsert, inlineChildren := -1, false
	keys := func(node *unstable.Node) []string {
		var result []string
		it := node.Key()
		for it.Next() {
			result = append(result, string(it.Node().Data))
		}
		return result
	}
	var visit func(*unstable.Node, []string)
	visit = func(node *unstable.Node, prefix []string) {
		full := append(append([]string{}, prefix...), keys(node)...)
		v := node.Value()
		if len(full) == 2 && full[0] == "book" && full[1] == "title" && v.Kind == unstable.String {
			start = int(v.Raw.Offset)
			end = start + int(v.Raw.Length)
		}
		if v.Kind == unstable.InlineTable {
			if len(full) == 1 && full[0] == "book" {
				inlineInsert = int(v.Raw.Offset) + 1
				inlineChildren = v.Child() != nil
			}
			it := v.Children()
			for it.Next() {
				visit(it.Node(), full)
			}
		}
	}
	for parser.NextExpression() {
		node := parser.Expression()
		switch node.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = keys(node)
			if node.Kind == unstable.Table && len(table) == 1 && table[0] == "book" {
				offset := int(node.Child().Raw.Offset)
				if newline := bytes.IndexByte(data[offset:], '\n'); newline >= 0 {
					insert = offset + newline + 1
				} else {
					insert = len(data)
				}
			}
		case unstable.KeyValue:
			visit(node, table)
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	if start >= 0 {
		return append(append(bytes.Clone(data[:start]), value...), data[end:]...), nil
	}
	line := append([]byte("title = "), value...)
	if inlineInsert >= 0 {
		if inlineChildren {
			line = append(line, ',', ' ')
		}
		return append(append(bytes.Clone(data[:inlineInsert]), line...), data[inlineInsert:]...), nil
	}
	if insert >= 0 {
		line = append(line, '\n')
		if insert == len(data) && (insert == 0 || data[insert-1] != '\n') {
			line = append([]byte{'\n'}, line...)
		}
		return append(append(bytes.Clone(data[:insert]), line...), data[insert:]...), nil
	}
	// 明示したbookテーブルがない場合はルートのドット区切りキーを追加します。
	line = append(append([]byte("book."), line...), '\n')
	return append(line, data...), nil
}
