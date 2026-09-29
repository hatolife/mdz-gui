package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hatolife/mdz-gui/internal/settings"
)

// TestBrowserBridge は明示した場合だけ起動する、実バックエンドを使った画面テスト用です。
func TestBrowserBridge(t *testing.T) {
	portFile := os.Getenv("MDZ_BROWSER_BRIDGE")
	if portFile == "" {
		t.Skip("ブラウザーテスト用のブリッジは通常起動しません")
	}
	base := t.TempDir()
	cfg := settings.Default()
	cfg.Editor = "builtin"
	cfg.NvimPath = os.Getenv("MDZ_NVIM_TEST")
	cfg.MdbookPath = os.Getenv("MDZ_MDBOOK_TEST")
	type Event struct {
		Name string `json:"name"`
		Data any    `json:"data"`
	}
	var mu sync.Mutex
	events := []Event{}
	a := &App{base: base, cfg: cfg, launchAudience: func(string) error { return nil }, notify: func(name string, data any) { mu.Lock(); events = append(events, Event{name, data}); mu.Unlock() }}
	defer a.shutdown()
	mux := http.NewServeMux()
	var server *http.Server
	mux.HandleFunc("/test-stop", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); go server.Close() })
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(events)
		events = []Event{}
	})
	mux.HandleFunc("/test-info", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"base": base, "saved": filepath.Join(base, "document.mdz")})
	})
	mux.HandleFunc("/rpc/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[len("/rpc/"):]
		var args []json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if name == "Save" {
			a.mu.Lock()
			err := a.saveLocked(filepath.Join(base, "document.mdz"))
			a.mu.Unlock()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			json.NewEncoder(w).Encode(true)
			return
		}
		method := reflect.ValueOf(a).MethodByName(name)
		if !method.IsValid() || method.Type().NumIn() != len(args) {
			http.Error(w, "unknown method", 400)
			return
		}
		values := []reflect.Value{}
		for i, arg := range args {
			v := reflect.New(method.Type().In(i))
			if err := json.Unmarshal(arg, v.Interface()); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			values = append(values, v.Elem())
		}
		results := method.Call(values)
		var value any
		for _, result := range results {
			if result.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
				if !result.IsNil() {
					http.Error(w, result.Interface().(error).Error(), 500)
					return
				}
			} else {
				value = result.Interface()
			}
		}
		json.NewEncoder(w).Encode(value)
	})
	mux.HandleFunc("/test-audience/", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		p := a.presentation
		a.mu.Unlock()
		if p == nil {
			http.NotFound(w, r)
			return
		}
		request, err := http.NewRequest(r.Method, p.endpoint+strings.TrimPrefix(r.URL.RequestURI(), "/test-audience/"), r.Body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer response.Body.Close()
		w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
		w.WriteHeader(response.StatusCode)
		io.Copy(w, response.Body)
	})
	mux.HandleFunc("/bundle/", a.serveAsset)
	mux.Handle("/", http.FileServer(http.Dir("frontend/dist")))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(fmt.Sprintf("http://%s", listener.Addr())), 0600); err != nil {
		t.Fatal(err)
	}
	server = &http.Server{Handler: mux}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
