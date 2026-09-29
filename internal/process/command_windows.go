package process

import (
	"os/exec"
	"syscall"
)

func configure(cmd *exec.Cmd) {
	// コンソールを作成せず、Windows Terminalの設定に依存する表示も防ぎます。
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
