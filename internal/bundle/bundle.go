// Package bundle はMDZip文書をメモリ上で編集し、安全に保存します。
package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxFile = 32 << 20
const MaxTotal = 128 << 20
const MaxEntries = 4096

// Document は未知の添付ファイルとmanifestのフィールドも保持します。
type Document struct {
	Files    map[string][]byte
	Manifest map[string]json.RawMessage
	Entry    string
	Mode     string
}

// New は新規文書を作成します。
func New() *Document {
	return &Document{Files: map[string][]byte{"index.md": []byte("# 新しい文書\n\nここから書き始めます。\n")}, Manifest: map[string]json.RawMessage{}, Entry: "index.md", Mode: "document"}
}

// ValidPath はZIP内のパスをOSに依存せず検査します。
func ValidPath(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.HasPrefix(name, "/") || strings.ContainsAny(name, `\:*?"<>|`) {
		return false
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func IsMarkdown(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// Read は展開先を作らずに読み込み、件数と実際の展開サイズを制限します。
func Read(filename string) (*Document, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	if len(z.File) > MaxEntries {
		return nil, fmt.Errorf("ファイル数が上限を超えています")
	}
	d := &Document{Files: map[string][]byte{}, Manifest: map[string]json.RawMessage{}, Mode: "document"}
	total := 0
	seen := map[string]bool{}
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !ValidPath(name) || seen[name] {
			return nil, fmt.Errorf("不正または重複したパス: %s", f.Name)
		}
		seen[name] = true
		if f.Flags&1 != 0 || f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("暗号化またはシンボリックリンクは非対応: %s", name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > MaxFile {
			return nil, fmt.Errorf("ファイルが大きすぎます: %s", name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, MaxFile+1))
		r.Close()
		if err != nil {
			return nil, err
		}
		total += len(b)
		if len(b) > MaxFile || total > MaxTotal {
			return nil, fmt.Errorf("展開サイズが上限を超えています")
		}
		if (IsMarkdown(name) || name == "manifest.json") && !utf8.Valid(b) {
			return nil, fmt.Errorf("UTF-8ではありません: %s", name)
		}
		d.Files[name] = b
	}
	return resolve(d)
}

// FromFiles は作業ディレクトリの文書を検証します。
func FromFiles(files map[string][]byte) (*Document, error) {
	for name, b := range files {
		if !ValidPath(name) || ((IsMarkdown(name) || name == "manifest.json") && !utf8.Valid(b)) {
			return nil, fmt.Errorf("不正なファイル: %s", name)
		}
	}
	return resolve(&Document{Files: files, Manifest: map[string]json.RawMessage{}, Mode: "document"})
}

func resolve(d *Document) (*Document, error) {
	if b, ok := d.Files["manifest.json"]; ok {
		if err := json.Unmarshal(b, &d.Manifest); err != nil || d.Manifest == nil {
			return nil, fmt.Errorf("manifest.jsonが不正です")
		}
		if v, ok := d.Manifest["mode"]; ok {
			if err := json.Unmarshal(v, &d.Mode); err != nil || (d.Mode != "document" && d.Mode != "project" && d.Mode != "slides") {
				return nil, fmt.Errorf("ERR_MODE_UNSUPPORTED")
			}
		}
		if v, ok := d.Manifest["spec"]; ok {
			var spec struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(v, &spec); err != nil {
				return nil, fmt.Errorf("specが不正です")
			}
			if spec.Version != "" {
				major, err := strconv.Atoi(strings.Split(spec.Version, ".")[0])
				if err != nil || major > 1 || major < 0 {
					return nil, fmt.Errorf("未対応のMDZipバージョン: %s", spec.Version)
				}
			}
		}
		if v, ok := d.Manifest["entryPoint"]; ok {
			if err := json.Unmarshal(v, &d.Entry); err != nil || !ValidPath(d.Entry) || !IsMarkdown(d.Entry) {
				return nil, fmt.Errorf("entryPointが不正です")
			}
			if _, ok := d.Files[d.Entry]; !ok {
				return nil, fmt.Errorf("entryPointのファイルがありません")
			}
		}
	}
	if d.Entry == "" {
		if _, ok := d.Files["index.md"]; ok {
			d.Entry = "index.md"
		} else {
			var roots []string
			for name := range d.Files {
				if !strings.Contains(name, "/") && IsMarkdown(name) {
					roots = append(roots, name)
				}
			}
			if len(roots) != 1 {
				return nil, fmt.Errorf("開始ページを決められません。index.mdまたはmanifestのentryPointを指定してください")
			}
			d.Entry = roots[0]
		}
	}
	if d.Mode == "slides" {
		deck, err := d.Deck()
		if err != nil {
			return nil, err
		}
		d.Entry = deck.Slides[0].File
	}
	return d, nil
}

// Put はサイズ制限を守って本文や画像を追加します。
func (d *Document) Put(name string, data []byte) error {
	if !ValidPath(name) || name == "manifest.json" {
		return fmt.Errorf("不正なファイル名です")
	}
	if IsMarkdown(name) && !utf8.Valid(data) {
		return fmt.Errorf("本文はUTF-8で指定してください")
	}
	total := len(data)
	for key, b := range d.Files {
		if key != name {
			total += len(b)
		}
	}
	if len(data) > MaxFile || total > MaxTotal {
		return fmt.Errorf("文書のサイズ上限を超えています")
	}
	if _, ok := d.Files[name]; !ok && len(d.Files) >= MaxEntries-1 {
		return fmt.Errorf("ファイル数の上限を超えています")
	}
	d.Files[name] = append([]byte(nil), data...)
	return nil
}

func (d *Document) Pages() []string {
	if d.Mode == "slides" {
		deck, err := d.Deck()
		if err != nil {
			return []string{}
		}
		names := make([]string, 0, len(deck.Slides))
		for _, slide := range deck.Slides {
			names = append(names, slide.File)
		}
		return names
	}
	var names []string
	for name := range d.Files {
		if IsMarkdown(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	// 独自の表示順を優先し、外部で追加されたページは末尾へ補います。
	var order []string
	if json.Unmarshal(d.Manifest["x-mdz-gui-pageOrder"], &order) == nil {
		remaining := make(map[string]bool, len(names))
		for _, name := range names {
			remaining[name] = true
		}
		ordered := make([]string, 0, len(names))
		for _, name := range order {
			if remaining[name] {
				ordered = append(ordered, name)
				delete(remaining, name)
			}
		}
		for _, name := range names {
			if remaining[name] {
				ordered = append(ordered, name)
			}
		}
		return ordered
	}
	return names
}

// Write は同じディレクトリの一時ファイルを完成させてから置換します。
func (d *Document) Write(filename, version string) error {
	manifest := make(map[string]json.RawMessage, len(d.Manifest))
	for k, v := range d.Manifest {
		manifest[k] = v
	}
	set := func(k string, v any) { manifest[k], _ = json.Marshal(v) }
	set("spec", map[string]string{"name": "mdzip-spec", "version": "1.1.0"})
	set("entryPoint", d.Entry)
	set("mode", d.Mode)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, ok := manifest["created"]; !ok {
		set("created", map[string]string{"when": now})
	}
	set("modified", map[string]string{"when": now})
	// producer内に他の拡張フィールドがあれば保持します。
	producer := map[string]json.RawMessage{}
	if raw, ok := manifest["producer"]; ok {
		_ = json.Unmarshal(raw, &producer)
	}
	if producer == nil {
		producer = map[string]json.RawMessage{}
	}
	producer["application"], _ = json.Marshal(map[string]string{"name": "mdz-gui", "version": version, "url": "https://github.com/hatolife/mdz-gui"})
	set("producer", producer)
	meta, err := json.MarshalIndent(manifest, "", "\t")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".mdz-save-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	z := zip.NewWriter(f)
	names := make([]string, 0, len(d.Files)+1)
	for name := range d.Files {
		if name != "manifest.json" {
			names = append(names, name)
		}
	}
	names = append(names, "manifest.json")
	sort.Strings(names)
	for _, name := range names {
		b := d.Files[name]
		if name == "manifest.json" {
			b = meta
		}
		if IsMarkdown(name) {
			b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		}
		w, e := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if e == nil {
			_, e = w.Write(b)
		}
		if e != nil {
			z.Close()
			f.Close()
			return e
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := replace(temp, filename); err != nil {
		return err
	}
	d.Manifest = manifest
	d.Files["manifest.json"] = meta
	return nil
}

// ReplaceFile は保存済みの一時ファイルを置換します。
func ReplaceFile(from, to string) error { return replace(from, to) }
