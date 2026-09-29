package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPresentationIsolationAndLifecycle(t *testing.T) {
	a := slideApp(t)
	endpoint := ""
	a.launchAudience = func(value string) error { endpoint = value; return nil }
	info, _ := a.Slides()
	info.Deck.Slides[0].Notes = "PRIVATE SPEAKER NOTES"
	if _, err := a.ConfigureSlides(info.Revision, info.Deck); err != nil {
		t.Fatal(err)
	}
	slides := []AudienceSlide{}
	for _, slide := range info.Deck.Slides {
		slides = append(slides, AudienceSlide{ID: slide.ID, File: slide.File, Title: slide.Title, Layout: slide.Layout, FontSize: slide.FontSize, HTML: "<h1>test</h1>"})
	}
	state, err := a.StartPresentation(slides, 0)
	if err != nil {
		t.Fatal(err)
	}
	client := &AudienceWindow{endpoint: endpoint, client: &http.Client{Timeout: time.Second}}
	defer a.StopPresentation()
	initial, err := client.Poll(true)
	if err != nil {
		t.Fatal(err)
	}
	if initial.ID != state.ID || len(initial.Slides) != 8 {
		t.Fatal(initial)
	}
	data, _ := json.Marshal(initial)
	if strings.Contains(string(data), "PRIVATE SPEAKER NOTES") || strings.Contains(string(data), "notes") {
		t.Fatal("ノートが投影側へ送信されました")
	}
	for _, command := range []string{"ready", "next", "fullscreen"} {
		if err = client.Control(command, 0); err != nil {
			t.Fatal(err)
		}
	}
	state = a.PresentationState()
	if state.Index != 1 || !state.Ready || !state.Fullscreen {
		t.Fatal(state)
	}
	if err = a.PresentationCommand("last", 0); err != nil {
		t.Fatal(err)
	}
	last, err := client.Poll(false)
	if err != nil || last.Index != 7 || len(last.Slides) != 0 {
		t.Fatal(last, err)
	}
	if err = client.Control("next", 0); err != nil {
		t.Fatal(err)
	}
	if a.PresentationState().Index != 7 {
		t.Fatal("末尾を越えました")
	}
	if err = client.Control("windowed", 0); err != nil {
		t.Fatal(err)
	}
	if a.PresentationState().Fullscreen {
		t.Fatal("全画面を解除できません")
	}
	for _, resource := range []string{"bundle/slides.json", "bundle/slides/slide-001.md", "bundle/../manifest.json"} {
		r, e := client.client.Get(endpoint + resource)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Fatal(resource, r.StatusCode)
		}
	}
	r, err := client.client.Get(endpoint + "bundle/images/slide-sample.svg")
	if err != nil {
		t.Fatal(err)
	}
	image, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || !strings.Contains(string(image), "<svg") {
		t.Fatal("画像が投影側で取得できません")
	}
	wrong := strings.Replace(endpoint, state.ID, strings.Repeat("0", 64), 1)
	r, err = client.client.Get(wrong + "state")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 404 {
		t.Fatal("不正な接続鍵を受け付けました")
	}
	if err = client.Control("closed", 0); err != nil {
		t.Fatal(err)
	}
	if !a.PresentationState().Closed {
		t.Fatal("ウィンドウ終了を検出できません")
	}
	a.StopPresentation()
	if _, err = client.Poll(false); err == nil {
		t.Fatal("終了後も投影データを配信しています")
	}
}

func TestSlideGapMovesAndMarginColor(t *testing.T) {
	a := slideApp(t)
	info, _ := a.Slides()
	first, last := info.Deck.Slides[0].ID, info.Deck.Slides[7].ID
	info, err := a.ChangeSlide(info.Revision, last, "move-before", first)
	if err != nil {
		t.Fatal(err)
	}
	if info.Deck.Slides[0].ID != last {
		t.Fatal("先頭への挿入に失敗しました")
	}
	info, err = a.ChangeSlide(info.Revision, last, "move-after", first)
	if err != nil {
		t.Fatal(err)
	}
	if info.Deck.Slides[1].ID != last {
		t.Fatal("後ろへの挿入に失敗しました")
	}
	info.Deck.MarginColor = "#eeeeee"
	info, err = a.ConfigureSlides(info.Revision, info.Deck)
	if err != nil {
		t.Fatal(err)
	}
	if info.Deck.MarginColor != "#eeeeee" {
		t.Fatal("余白色が失われました")
	}
	info.Deck.MarginColor = "invalid"
	if _, err = a.ConfigureSlides(info.Revision, info.Deck); err == nil {
		t.Fatal("不正な余白色を許可しました")
	}
}

func TestDependencyChecks(t *testing.T) {
	a := slideApp(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range a.CheckDependencies(executable, executable, executable) {
		if !item.Found || item.Path == "" {
			t.Fatal(item)
		}
	}
	missing := a.CheckDependencies("/missing/mdz/nvim", "/missing/mdz/init.lua", "/missing/mdz/mdbook")
	for _, item := range missing {
		if item.Found || item.Message == "" {
			t.Fatal(item)
		}
	}
}
