// Package workspace は展開文書、復旧データ、世代バックアップを管理します。
package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hatolife/mdz-gui/internal/bundle"
	"github.com/hatolife/mdz-gui/internal/settings"
)

type Meta struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	Updated   time.Time `json:"updated"`
	Dirty     bool      `json:"dirty"`
	PID       int       `json:"pid"`
	SavedHash string    `json:"savedHash"`
}

type Session struct {
	Meta
	Root string
	Doc  *bundle.Document
}

// AtomicWrite は元ファイルを先に削除せずに置換します。
func AtomicWrite(filename string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".mdz-tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
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
	return bundle.ReplaceFile(f.Name(), filename)
}

func New(base string, doc *bundle.Document, filename string, dirty bool) (*Session, error) {
	if err := os.MkdirAll(filepath.Join(base, "workspaces"), 0700); err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp(filepath.Join(base, "workspaces"), "session-")
	if err != nil {
		return nil, err
	}
	s := &Session{Root: root, Doc: doc, Meta: Meta{ID: filepath.Base(root), Filename: filename, Dirty: dirty, PID: os.Getpid()}}
	if filename != "" {
		s.SavedHash, err = FileHash(filename)
		if err != nil {
			os.RemoveAll(root)
			return nil, err
		}
	}
	if err := os.MkdirAll(s.Content(), 0700); err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	// Windows上でも展開結果が一意になるように事前検証します。
	seen := map[string]bool{}
	for name := range doc.Files {
		if !PortablePath(name) {
			os.RemoveAll(root)
			return nil, fmt.Errorf("展開できないファイル名: %s", name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			os.RemoveAll(root)
			return nil, fmt.Errorf("大文字小文字だけが異なるファイル名: %s", name)
		}
		seen[key] = true
	}
	for name, b := range doc.Files {
		if err := s.write(name, b); err != nil {
			os.RemoveAll(root)
			return nil, err
		}
	}
	// 新規文書にも復旧可能なmanifestを置きます。
	if _, ok := doc.Files["manifest.json"]; !ok {
		b, _ := json.Marshal(map[string]any{"entryPoint": doc.Entry, "mode": doc.Mode})
		doc.Files["manifest.json"] = b
		if err := s.write("manifest.json", b); err != nil {
			os.RemoveAll(root)
			return nil, err
		}
	}
	if err := s.Checkpoint(); err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	return s, nil
}

func (s *Session) Content() string { return filepath.Join(s.Root, "content") }

// PortablePath はWindowsの予約名と末尾の空白・ピリオドも拒否します。
func PortablePath(name string) bool {
	if !bundle.ValidPath(name) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		stem := strings.ToUpper(strings.Split(part, ".")[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
			return false
		}
	}
	return true
}

func (s *Session) write(name string, b []byte) error {
	if !PortablePath(name) {
		return fmt.Errorf("不正な作業ファイル名: %s", name)
	}
	// Rootは途中のシンボリックリンクによる作業領域外への書き込みを拒否します。
	r, err := os.OpenRoot(s.Content())
	if err != nil {
		return err
	}
	defer r.Close()
	p := filepath.FromSlash(name)
	if err := r.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	// リンク自体も拒否し、既存ファイルの内容を途中まで切り詰めません。
	if info, e := r.Lstat(p); e == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("通常ファイルではありません: %s", name)
	}
	temp := filepath.Join(filepath.Dir(p), fmt.Sprintf(".mdz-tmp-%d", time.Now().UnixNano()))
	f, err := r.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer r.Remove(temp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Windowsではos.Root.Renameが既存ファイルを置換できます。
	return r.Rename(temp, p)
}

func (s *Session) Put(name string, b []byte) error {
	if old, ok := s.Doc.Files[name]; ok && bytes.Equal(old, b) {
		return nil
	}
	old, exists := s.Doc.Files[name]
	if err := s.Doc.Put(name, b); err != nil {
		return err
	}
	if err := s.write(name, b); err != nil {
		if exists {
			s.Doc.Files[name] = old
		} else {
			delete(s.Doc.Files, name)
		}
		return err
	}
	s.Dirty = true
	return s.Checkpoint()
}

// SetPageOrder は本文のパスを変更せずに、復旧可能なページ順を保存します。
func (s *Session) SetPageOrder(order []string) error {
	pages := s.Doc.Pages()
	if len(order) != len(pages) {
		return fmt.Errorf("ページ構成が変更されています")
	}
	remaining := make(map[string]bool, len(pages))
	for _, name := range pages {
		remaining[name] = true
	}
	for _, name := range order {
		if !remaining[name] {
			return fmt.Errorf("ページ順に不正な参照または重複があります")
		}
		delete(remaining, name)
	}
	manifest := map[string]json.RawMessage{}
	if err := json.Unmarshal(s.Doc.Files["manifest.json"], &manifest); err != nil {
		return err
	}
	manifest["x-mdz-gui-pageOrder"], _ = json.Marshal(order)
	data, err := json.MarshalIndent(manifest, "", "\t")
	if err != nil {
		return err
	}
	// ディスクへの書き込みに成功してからメモリ上の設定を更新します。
	if err := s.write("manifest.json", data); err != nil {
		return err
	}
	s.Doc.Files["manifest.json"] = data
	s.Doc.Manifest = manifest
	s.Dirty = true
	return s.Checkpoint()
}

func (s *Session) Checkpoint() error {
	s.Updated = time.Now().UTC()
	b, err := json.MarshalIndent(s.Meta, "", "\t")
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(s.Root, "session.json"), b)
}

// Capture はエディターが書き込んだ内容を検査して取り込みます。
func (s *Session) Capture() error {
	r, err := os.OpenRoot(s.Content())
	if err != nil {
		return err
	}
	defer r.Close()
	files := map[string][]byte{}
	total := 0
	err = fs.WalkDir(r.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if !PortablePath(name) || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("不正な作業ファイル: %s", name)
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(filepath.Base(name), ".mdz-tmp-") {
			return nil
		}
		if len(files) >= bundle.MaxEntries {
			return fmt.Errorf("ファイル数が上限を超えています")
		}
		f, e := r.Open(name)
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(f, bundle.MaxFile+1))
		f.Close()
		if e != nil {
			return e
		}
		total += len(b)
		if len(b) > bundle.MaxFile || total > bundle.MaxTotal {
			return fmt.Errorf("文書サイズが上限を超えています")
		}
		files[name] = b
		return nil
	})
	if err != nil {
		return err
	}
	doc, err := bundle.FromFiles(files)
	if err != nil {
		return err
	}
	if !sameFiles(s.Doc.Files, doc.Files) {
		s.Dirty = true
	}
	s.Doc = doc
	return nil
}

func sameFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

func FileHash(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Save は外部変更を検出してから旧版を退避し、新しいMDZへ置換します。
func (s *Session) Save(base, filename, version string, cfg settings.Settings) error {
	if err := s.Capture(); err != nil {
		return err
	}
	if filepath.Clean(filename) == filepath.Clean(s.Filename) && s.SavedHash != "" {
		h, err := FileHash(filename)
		if err != nil || h != s.SavedHash {
			return fmt.Errorf("保存先が外部で変更・削除されています。名前を付けて保存してください")
		}
	}
	if _, err := os.Stat(filename); err == nil && cfg.BackupGenerations > 0 {
		if err := backup(base, filename, cfg); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := s.Doc.Write(filename, version); err != nil {
		return err
	}
	s.Filename = filename
	s.Dirty = false
	var err error
	s.SavedHash, err = FileHash(filename)
	if err != nil {
		return err
	}
	if err := s.write("manifest.json", s.Doc.Files["manifest.json"]); err != nil {
		return err
	}
	return s.Checkpoint()
}

// SaveMarkdown は単一Markdownの本文だけを元ファイルへ安全に書き戻します。
func (s *Session) SaveMarkdown(base, filename string, cfg settings.Settings) error {
	if err := s.Capture(); err != nil {
		return err
	}
	body, ok := s.Doc.Files[s.Doc.Entry]
	if !ok || !bundle.IsMarkdown(s.Doc.Entry) {
		return fmt.Errorf("単一Markdownの本文が見つかりません")
	}
	for name := range s.Doc.Files {
		if name != "manifest.json" && name != s.Doc.Entry {
			return fmt.Errorf("単一Markdownとして保存できない追加ファイルがあります。MDZとして保存してください: %s", name)
		}
	}
	if filepath.Clean(filename) == filepath.Clean(s.Filename) && s.SavedHash != "" {
		h, err := FileHash(filename)
		if err != nil || h != s.SavedHash {
			return fmt.Errorf("保存先が外部で変更・削除されています。MDZとして保存するか、開き直してください")
		}
	}
	if _, err := os.Stat(filename); err == nil && cfg.BackupGenerations > 0 {
		if err := backup(base, filename, cfg); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := AtomicWrite(filename, body); err != nil {
		return err
	}
	s.Filename = filename
	s.Dirty = false
	var err error
	s.SavedHash, err = FileHash(filename)
	if err != nil {
		return err
	}
	return s.Checkpoint()
}

func backup(base, filename string, cfg settings.Settings) error {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	key := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(abs))))
	dir := filepath.Join(base, "backups", hex.EncodeToString(key[:16]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	extension := strings.ToLower(filepath.Ext(filename))
	switch extension {
	case ".mdz", ".md", ".markdown":
	default:
		extension = ".bak"
	}
	name := filepath.Join(dir, time.Now().UTC().Format("20060102-150405.000000000")+extension)
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	out, err := os.CreateTemp(dir, ".backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := bundle.ReplaceFile(out.Name(), name); err != nil {
		return err
	}
	_ = AtomicWrite(filepath.Join(dir, "source.txt"), []byte(abs))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), extension) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	var size int64
	for i, n := range names {
		p := filepath.Join(dir, n)
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		size += info.Size()
		// 直前の1世代は容量を超えても残します。
		if i >= cfg.BackupGenerations || (i > 0 && size > int64(cfg.BackupMiB)<<20) {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
	}
	return nil
}

func Recoveries(base string) ([]Meta, error) {
	result := []Meta{}
	entries, err := os.ReadDir(filepath.Join(base, "workspaces"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "session-") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(base, "workspaces", e.Name(), "session.json"))
		if err != nil {
			continue
		}
		var m Meta
		if json.Unmarshal(b, &m) != nil || m.ID != e.Name() || processAlive(m.PID) {
			continue
		}
		// クラッシュ直前にdirty情報を更新できなかった場合も復旧対象にします。
		result = append(result, m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Updated.After(result[j].Updated) })
	return result, nil
}

func Recover(base, id string) (*Session, error) {
	items, err := Recoveries(base)
	if err != nil {
		return nil, err
	}
	for _, m := range items {
		if m.ID == id {
			s := &Session{Meta: m, Root: filepath.Join(base, "workspaces", id), Doc: bundle.New()}
			if err := s.Capture(); err != nil {
				return nil, err
			}
			s.PID = os.Getpid()
			s.Dirty = true
			return s, s.Checkpoint()
		}
	}
	return nil, fmt.Errorf("復旧対象が見つかりません")
}

func (s *Session) Close() error { return os.RemoveAll(s.Root) }
