//go:build windows

package main

import (
	"os"
	"syscall"
)

const attachParentProcess = ^uintptr(0)

// prepareCLIOutput はGUIサブシステムの実行ファイルを端末から呼んだ場合だけ親コンソールへ接続します。
func prepareCLIOutput(args []string) {
	if !isCLIInfoRequest(args) || (streamAvailable(os.Stdout) && streamAvailable(os.Stderr)) {
		return
	}
	attachConsole := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	attached, _, _ := attachConsole.Call(attachParentProcess)
	if attached == 0 {
		return
	}
	if stdout, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = stdout
	}
	if stderr, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stderr = stderr
	}
}

func streamAvailable(file *os.File) bool {
	if file == nil {
		return false
	}
	_, err := file.Stat()
	return err == nil
}

