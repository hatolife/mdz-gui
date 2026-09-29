package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/hatolife/mdz-gui/internal/bundle"
)

// AudienceSlide は投影に必要な情報だけを渡し、発表者ノートは含めません。
type AudienceSlide struct {
	ID         string `json:"id"`
	File       string `json:"file"`
	Title      string `json:"title"`
	Layout     string `json:"layout"`
	FontSize   int    `json:"fontSize"`
	Background string `json:"background"`
	HTML       string `json:"html"`
}
type PresentationState struct {
	ID          string          `json:"id"`
	Slides      []AudienceSlide `json:"slides,omitempty"`
	Theme       string          `json:"theme"`
	Aspect      string          `json:"aspect"`
	MarginColor string          `json:"marginColor"`
	Index       int             `json:"index"`
	Fullscreen  bool            `json:"fullscreen"`
	Ready       bool            `json:"ready"`
	Closed      bool            `json:"closed"`
}
type presentation struct {
	mu       sync.Mutex
	state    PresentationState
	images   map[string][]byte
	server   *http.Server
	process  *exec.Cmd
	done     chan struct{}
	endpoint string
}

// StartPresentation は現在の資料のスナップショットから投影専用ウィンドウを開きます。
func (a *App) StartPresentation(slides []AudienceSlide, index int) (PresentationState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.syncNativeLocked(); err != nil {
		return PresentationState{}, err
	}
	info, err := a.slidesLocked()
	if err != nil {
		return PresentationState{}, err
	}
	if len(slides) != len(info.Deck.Slides) || index < 0 || index >= len(slides) {
		return PresentationState{}, fmt.Errorf("スライド構成が変更されています")
	}
	total := 0
	for i, s := range slides {
		if s.ID != info.Deck.Slides[i].ID || s.File != info.Deck.Slides[i].File {
			return PresentationState{}, fmt.Errorf("スライド構成が変更されています")
		}
		total += len(s.HTML)
	}
	if total > bundle.MaxTotal {
		return PresentationState{}, fmt.Errorf("発表用データが大きすぎます")
	}
	a.stopPresentationLocked()
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return PresentationState{}, err
	}
	id := hex.EncodeToString(key)
	p := &presentation{state: PresentationState{ID: id, Slides: slides, Theme: info.Deck.Theme, Aspect: info.Deck.Aspect, MarginColor: info.Deck.MarginColor, Index: index}, images: map[string][]byte{}}
	// 投影側には本文ファイル、設定、ノートを配信しません。
	for name, data := range a.session.Doc.Files {
		switch strings.ToLower(path.Ext(name)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
			p.images[name] = append([]byte(nil), data...)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return PresentationState{}, err
	}
	p.endpoint = "http://" + listener.Addr().String() + "/" + id + "/"
	p.server = &http.Server{Handler: http.HandlerFunc(p.serve), ReadHeaderTimeout: 5 * time.Second}
	go p.server.Serve(listener)
	a.presentation = p
	if a.launchAudience != nil {
		err = a.launchAudience(p.endpoint)
	} else {
		var executable string
		executable, err = os.Executable()
		if err == nil {
			p.done = make(chan struct{})
			p.process = exec.Command(executable, "--audience", p.endpoint)
			err = p.process.Start()
		}
		if err == nil {
			go func() {
				_ = p.process.Wait()
				p.mu.Lock()
				p.state.Closed = true
				p.mu.Unlock()
				close(p.done)
			}()
		}
	}
	if err != nil {
		a.stopPresentationLocked()
		return PresentationState{}, fmt.Errorf("投影用ウィンドウを開けません: %w", err)
	}
	return p.snapshot(true), nil
}
func (p *presentation) snapshot(withSlides bool) PresentationState {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.state
	if !withSlides {
		s.Slides = nil
	}
	return s
}
func (a *App) PresentationState() PresentationState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.presentation == nil {
		return PresentationState{Closed: true}
	}
	return a.presentation.snapshot(false)
}
func (a *App) PresentationCommand(command string, index int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.presentation == nil {
		return fmt.Errorf("2画面発表を開始してください")
	}
	return a.presentation.command(command, index)
}
func (p *presentation) command(command string, index int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := &p.state
	switch command {
	case "next":
		s.Index++
	case "prev":
		s.Index--
	case "first":
		s.Index = 0
	case "last":
		s.Index = len(s.Slides) - 1
	case "goto":
		s.Index = index
	case "fullscreen":
		s.Fullscreen = !s.Fullscreen
	case "windowed":
		s.Fullscreen = false
	case "ready":
		s.Ready = true
	case "closed":
		s.Closed = true
	default:
		return fmt.Errorf("不明な発表操作です")
	}
	if s.Index < 0 {
		s.Index = 0
	}
	if s.Index >= len(s.Slides) {
		s.Index = len(s.Slides) - 1
	}
	return nil
}
func (a *App) StopPresentation() { a.mu.Lock(); defer a.mu.Unlock(); a.stopPresentationLocked() }
func (a *App) stopPresentationLocked() {
	p := a.presentation
	if p == nil {
		return
	}
	a.presentation = nil
	// 通常は投影側の終了処理を待ち、応答しない場合だけプロセスを停止します。
	p.mu.Lock()
	p.state.Closed = true
	p.mu.Unlock()
	if p.process != nil && p.process.Process != nil {
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			_ = p.process.Process.Kill()
		}
	}
	_ = p.server.Close()
}
func (p *presentation) serve(w http.ResponseWriter, r *http.Request) {
	prefix := "/" + p.state.ID + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	resource := strings.TrimPrefix(r.URL.Path, prefix)
	if resource == "state" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(p.snapshot(r.URL.Query().Get("deck") == "1"))
		return
	}
	if resource == "command" && r.Method == http.MethodPost {
		var input struct {
			Command string `json:"command"`
			Index   int    `json:"index"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&input) != nil {
			http.Error(w, "不正な操作", 400)
			return
		}
		if err := p.command(input.Command, input.Index); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.WriteHeader(204)
		return
	}
	if strings.HasPrefix(resource, "bundle/") && r.Method == http.MethodGet {
		name := strings.TrimPrefix(resource, "bundle/")
		data, ok := p.images[name]
		if ok {
			w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(name)))
			w.Write(data)
			return
		}
	}
	http.NotFound(w, r)
}
