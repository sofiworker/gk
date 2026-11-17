//go:build !unix

package local

import "io/fs"

// 无法验证链接数的平台拒绝普通文件导入和写回。
// Platforms without link-count validation reject regular-file import and synchronization.
func multipleLinks(fs.FileInfo) bool { return true }

func supportedPlatform() bool { return false }
