package process

import (
	"context"
	"os/exec"
)

// Command は画面操作を伴わない外部コマンドを作成します。
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	configure(cmd)
	return cmd
}

// CommandContext はキャンセル可能なバックグラウンドコマンドを作成します。
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configure(cmd)
	return cmd
}
