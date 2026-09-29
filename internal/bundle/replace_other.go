//go:build !windows

package bundle

import "os"

func replace(from, to string) error { return os.Rename(from, to) }
