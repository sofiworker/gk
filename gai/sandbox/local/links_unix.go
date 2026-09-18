//go:build unix

package local

import (
	"io/fs"
	"syscall"
)

func multipleLinks(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || stat.Nlink > 1
}

func supportedPlatform() bool { return true }
