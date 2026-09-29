// Package editor は実際のNeovimプロセスをRPCとUIプロトコルで接続します。
package editor

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/neovim/go-client/nvim"
	"github.com/hatolife/mdz-gui/internal/settings"
)

type Native struct {
	Client  *nvim.Nvim
	cancel  context.CancelFunc
	changed atomic.Bool
	Root    string
	wsl     bool
}

type Current struct {
	Name string `msgpack:"name" json:"name"`
	Text string `msgpack:"text" json:"text"`
}

type UndoState struct {
	CanUndo bool `msgpack:"canUndo" json:"canUndo"`
	CanRedo bool `msgpack:"canRedo" json:"canRedo"`
}

func Start(root, undoDir string, cfg settings.Settings, emit func(string, any)) (*Native, error) {
	launch, err := Resolve(cfg.NvimPath)
	if err != nil {
		return nil, err
	}
	nativeRoot, err := launch.Path(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(undoDir, 0700); err != nil {
		return nil, err
	}
	nativeUndo, err := launch.Path(undoDir)
	if err != nil {
		return nil, err
	}
	args := []string{"--embed", "-n", "--cmd", "let g:mdz_gui = 1"}
	switch cfg.InitMode {
	case "custom":
		_, initPath, initErr := launch.initFilePath(cfg.InitPath)
		if initErr == nil {
			if found, existsErr := launch.initFileExists(initPath); existsErr == nil && found {
				args = append(args, "-u", initPath)
				break
			}
		}
		// init.luaを利用できない場合は設定なしで安全に起動します。
		args = append(args, "--clean")
	default:
		args = append(args, "--clean")
	}
	ctx, cancel := context.WithCancel(context.Background())
	// 設定ファイルが起動時に待ち続けてもアプリを固めません。
	timer := time.AfterFunc(12*time.Second, cancel)
	defer timer.Stop()
	v, err := nvim.NewChildProcess(nvim.ChildProcessCommand(launch.Command), nvim.ChildProcessArgs(launch.Args(args)...), nvim.ChildProcessDir(filepath.Dir(root)), nvim.ChildProcessContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	e := &Native{Client: v, cancel: cancel, Root: nativeRoot, wsl: launch.WSL}
	fail := func(err error) (*Native, error) { e.Close(); return nil, err }
	if err := v.RegisterHandler("redraw", func(events ...[]interface{}) { emit("native-redraw", normalize(events)) }); err != nil {
		return fail(err)
	}
	if err := v.RegisterHandler("mdz_changed", func() { e.changed.Store(true); emit("native-changed", nil) }); err != nil {
		return fail(err)
	}
	if err := v.RegisterHandler("mdz_write", func() { e.changed.Store(true); emit("native-save", nil) }); err != nil {
		return fail(err)
	}
	if err := v.RegisterHandler("mdz_scroll", func(ratio float64) { emit("native-scroll", ratio) }); err != nil {
		return fail(err)
	}
	if err := v.AttachUI(80, 24, map[string]interface{}{"rgb": true, "ext_linegrid": true}); err != nil {
		return fail(err)
	}
	channel := v.ChannelID()
	var result any
	err = v.ExecLua(`
local channel, root, undo, levels = ...
vim.g.mdz_gui = true
vim.g.mdz_root = root
vim.o.hidden = true
vim.o.swapfile = false
vim.o.backup = false
vim.o.writebackup = false
vim.o.modeline = false
vim.o.exrc = false
vim.api.nvim_set_current_dir(root)
vim.o.undolevels = levels
vim.g.mdz_undo_levels = levels
vim.o.undofile = true
vim.opt.undodir = undo
vim.o.mouse = 'a'
local group = vim.api.nvim_create_augroup('MdzGui', {clear=true})
local attached = {}
local function attach(buf)
 if attached[buf] then return end
 attached[buf] = true
 vim.api.nvim_buf_attach(buf, false, {on_lines=function() vim.rpcnotify(channel, 'mdz_changed') end, on_detach=function() attached[buf]=nil end})
end
vim.api.nvim_create_autocmd({'BufEnter','BufReadPost','BufNewFile'}, {group=group, callback=function(ev)
 vim.bo[ev.buf].swapfile = false
 vim.bo[ev.buf].modeline = false
 vim.bo[ev.buf].undolevels = vim.g.mdz_undo_levels
 attach(ev.buf)
 vim.rpcnotify(channel, 'mdz_changed')
end})
vim.api.nvim_create_autocmd('BufWritePost', {group=group, callback=function() vim.rpcnotify(channel, 'mdz_write') end})
local function notify_scroll()
 local win=vim.api.nvim_get_current_win()
 local view=vim.fn.winsaveview()
 local total=vim.api.nvim_buf_line_count(0)
 local height=vim.api.nvim_win_get_height(win)
 local max_top=math.max(1,total-height+1)
 local ratio=max_top>1 and math.max(0,math.min(1,(view.topline-1)/(max_top-1))) or 0
 vim.rpcnotify(channel, 'mdz_scroll', ratio)
end
vim.api.nvim_create_autocmd({'WinScrolled','BufEnter'}, {group=group, callback=notify_scroll})
attach(vim.api.nvim_get_current_buf())
notify_scroll()
`, &result, channel, nativeRoot, nativeUndo, cfg.UndoLevels)
	if err != nil {
		return fail(err)
	}
	return e, nil
}

// Open はバッファを使い回してページ移動時のUndo履歴を保持します。
func (e *Native) Open(name string) error {
	var result any
	return e.Client.ExecLua(`local name=...; local b=vim.fn.bufadd(name); vim.fn.bufload(b); vim.api.nvim_set_current_buf(b)`, &result, e.filePath(name))
}

func (e *Native) Scroll(ratio float64) error {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	var result any
	return e.Client.ExecLua(`
local ratio=...
local win=vim.api.nvim_get_current_win()
local view=vim.fn.winsaveview()
local total=vim.api.nvim_buf_line_count(0)
local height=vim.api.nvim_win_get_height(win)
local max_top=math.max(1,total-height+1)
view.topline=max_top>1 and (1+math.floor(ratio*(max_top-1)+0.5)) or 1
vim.fn.winrestview(view)
`, &result, ratio)
}

// Flush は作業ファイルへの保存だけを行い、MDZ保存イベントを再発火させません。
func (e *Native) Flush() error {
	var result any
	return e.Client.ExecLua(`
local root = vim.fs.normalize(...):gsub('/+$','') .. '/'
local function belongs(name)
 name=vim.fs.normalize(name)
 if vim.fn.has('win32')==1 then return name:lower():sub(1,#root)==root:lower() end
 return name:sub(1,#root)==root
end
for _,buf in ipairs(vim.api.nvim_list_bufs()) do
 if vim.api.nvim_buf_is_loaded(buf) and vim.bo[buf].buftype=='' and belongs(vim.api.nvim_buf_get_name(buf)) and vim.bo[buf].modified then
  vim.api.nvim_buf_call(buf, function() vim.cmd('silent noautocmd write') end)
 end
end
`, &result, e.Root)
}

func (e *Native) Current() (Current, error) {
	var c Current
	err := e.Client.ExecLua(`local b=vim.api.nvim_get_current_buf(); return {name=vim.api.nvim_buf_get_name(b),text=table.concat(vim.api.nvim_buf_get_lines(b,0,-1,false),'\n') .. (vim.bo[b].endofline and '\n' or '')}`, &c)
	if err != nil {
		return c, err
	}
	rel, err := filepath.Rel(e.Root, c.Name)
	if e.wsl {
		root := strings.TrimRight(e.Root, "/") + "/"
		cleaned := path.Clean(c.Name)
		if !strings.HasPrefix(cleaned, root) {
			return Current{}, fmt.Errorf("文書外のNeovimバッファです。文書のページを選択してください")
		}
		rel, err = strings.TrimPrefix(cleaned, root), nil
	}
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return Current{}, fmt.Errorf("文書外のNeovimバッファです。文書のページを選択してください")
	}
	c.Name = filepath.ToSlash(rel)
	return c, nil
}

func (e *Native) Changed() bool { return e.changed.Swap(false) }
func (e *Native) Input(keys string) error {
	for len(keys) > 0 {
		n, err := e.Client.Input(keys)
		if err != nil {
			return err
		}
		if n <= 0 {
			return fmt.Errorf("Neovimの入力キューがいっぱいです")
		}
		keys = keys[n:]
	}
	return nil
}
func (e *Native) Paste(text string) error { _, err := e.Client.Paste(text, true, -1); return err }
func (e *Native) Undo(redo bool) error {
	command := "undo"
	if redo {
		command = "redo"
	}
	return e.Client.Command("stopinsert | " + command)
}
func (e *Native) UndoState() (UndoState, error) {
	var state UndoState
	err := e.Client.ExecLua(`local tree=vim.fn.undotree(); return {canUndo=tree.seq_cur>0,canRedo=tree.seq_cur<tree.seq_last}`, &state)
	return state, err
}
func (e *Native) Resize(cols, rows int) error {
	if cols < 10 {
		cols = 10
	}
	if rows < 3 {
		rows = 3
	}
	if cols > 600 {
		cols = 600
	}
	if rows > 300 {
		rows = 300
	}
	return e.Client.TryResizeUI(cols, rows)
}
func (e *Native) Close() { e.cancel(); _ = e.Client.Close() }

// normalize はRPCの辞書をWailsで送れるJSON構造に変換します。
func normalize(value any) any {
	switch v := value.(type) {
	case []interface{}:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = normalize(x)
		}
		return out
	case [][]interface{}:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = normalize(x)
		}
		return out
	case map[interface{}]interface{}:
		out := map[string]any{}
		for k, x := range v {
			out[fmt.Sprint(k)] = normalize(x)
		}
		return out
	case map[string]interface{}:
		out := map[string]any{}
		for k, x := range v {
			out[k] = normalize(x)
		}
		return out
	default:
		return value
	}
}

// SetUndoLevels は既存バッファと今後開くバッファへ保持数を適用します。
func (e *Native) SetUndoLevels(levels int) error {
	var result any
	return e.Client.ExecLua(`local n=...;vim.g.mdz_undo_levels=n;vim.o.undolevels=n;for _,b in ipairs(vim.api.nvim_list_bufs()) do vim.bo[b].undolevels=n end`, &result, levels)
}

// ReloadFile はGUIから変更した管理ファイルだけを再読込し、本文のUndo履歴を維持します。
func (e *Native) ReloadFile(name string) error {
	var result any
	return e.Client.ExecLua(`local b=vim.fn.bufnr(...); if b>=0 and vim.api.nvim_buf_is_loaded(b) then vim.api.nvim_buf_call(b,function() vim.cmd('silent noautocmd edit!') end) end`, &result, e.filePath(name))
}

// filePath は実際にNeovimが動作する環境の区切り文字を使用します。
func (e *Native) filePath(name string) string {
	if e.wsl {
		return path.Join(e.Root, name)
	}
	return filepath.Join(e.Root, filepath.FromSlash(name))
}
