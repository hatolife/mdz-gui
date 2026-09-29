//go:build !windows

package main

// prepareCLIOutput はWindows以外では標準出力をそのまま使用します。
func prepareCLIOutput(args []string) {
}
