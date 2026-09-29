package main

import (
	"fmt"
	"slices"
)

// MovePage は並べ替え前の順序を確認し、ファイル名を変えずにページを移動します。
func (a *App) MovePage(expected []string, name, target string, after bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil || a.session.Doc.Mode == "slides" {
		return fmt.Errorf("通常の文書を開いてください")
	}
	if a.singleMarkdownLocked() {
		return fmt.Errorf("単一Markdownではページを並べ替えられません")
	}
	if _, ok := a.session.Doc.Files["book.toml"]; ok {
		return fmt.Errorf("mdBookは目次で並べ替えてください")
	}
	if err := a.syncNativeLocked(); err != nil {
		return err
	}
	if err := a.session.Capture(); err != nil {
		return err
	}
	order := a.session.Doc.Pages()
	if !slices.Equal(order, expected) {
		return fmt.Errorf("ページ構成が変更されました。最新の一覧を確認してください")
	}
	from, to := slices.Index(order, name), slices.Index(order, target)
	if from < 0 || to < 0 {
		return fmt.Errorf("移動対象のページがありません")
	}
	if from == to {
		return nil
	}
	order = slices.Delete(order, from, from+1)
	to = slices.Index(order, target)
	if after {
		to++
	}
	order = slices.Insert(order, to, name)
	return a.session.SetPageOrder(order)
}
