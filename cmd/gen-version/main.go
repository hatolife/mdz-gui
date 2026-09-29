package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const baseVersion = "v0.0.1"

func main() {
	root := gitOutput("", "rev-parse", "--show-toplevel")
	commitTime := gitOutput(root, "show", "-s", "--format=%cd", "--date=format:%Y-%m-%d-%H:%M", "HEAD")
	commitShort := gitOutput(root, "rev-parse", "--short=7", "HEAD")
	version := fmt.Sprintf("%s-%s-%s", baseVersion, commitTime, commitShort)

	content := fmt.Sprintf(`package main

func init() {
	if version == developmentVersion+"-unbuilt" {
		version = %q
	}
}
`, version)

	output := filepath.Join(root, "version_generated.go")
	if err := os.WriteFile(output, []byte(content), 0644); err != nil {
		panic(err)
	}
	fmt.Println("version:", version)
}

func gitOutput(directory string, args ...string) string {
	cmd := exec.Command("git", args...)
	if directory != "" {
		cmd.Dir = directory
	}
	out, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(out))
}
