package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

func TestBackgroundCommandHasNoConsole(t *testing.T) {
	if os.Getenv("MDZ_CONSOLE_TEST_CHILD") == "1" {
		window, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		fmt.Printf("console=%d", window)
		os.Exit(0)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"command", "context"} {
		t.Run(name, func(t *testing.T) {
			var cmd *exec.Cmd
			if name == "command" {
				cmd = Command(executable, "-test.run=^TestBackgroundCommandHasNoConsole$")
			} else {
				cmd = CommandContext(context.Background(), executable, "-test.run=^TestBackgroundCommandHasNoConsole$")
			}
			cmd.Env = append(os.Environ(), "MDZ_CONSOLE_TEST_CHILD=1")
			output, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != "console=0" {
				t.Fatalf("output=%q err=%v", output, err)
			}
		})
	}
}
