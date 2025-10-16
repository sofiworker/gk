package gcompress

import (
	"fmt"
	"path/filepath"
	"strings"
)

type CompressionManager struct {
	ZipUtil   *ZipUtil
	TarUtil   *TarUtil
	TarGzUtil *TarGzUtil
}

func NewCompressionManager() *CompressionManager {
	return &CompressionManager{
		ZipUtil:   NewZipUtil(),
		TarUtil:   NewTarUtil(),
		TarGzUtil: NewTarGzUtil(),
	}
}

// AutoCompress 根据扩展名自动选择压缩方式
func (cm *CompressionManager) AutoCompress(source, target string) error {
	ext := strings.ToLower(filepath.Ext(target))

	switch ext {
	case ".zip":
		return cm.ZipUtil.Compress(source, target)
	case ".tar":
		return cm.TarUtil.Compress(source, target)
	case ".gz", ".tgz":
		// 检查是否是.tar.gz
		if strings.HasSuffix(strings.ToLower(target), ".tar.gz") ||
			strings.HasSuffix(strings.ToLower(target), ".tgz") {
			return cm.TarGzUtil.Compress(source, target)
		}
		return fmt.Errorf("不支持纯gzip压缩，请使用GzipUtil")
	default:
		return fmt.Errorf("不支持的压缩格式: %s", ext)
	}
}

// AutoDecompress 根据扩展名自动选择解压方式
func (cm *CompressionManager) AutoDecompress(source, target string) error {
	ext := strings.ToLower(filepath.Ext(source))

	switch ext {
	case ".zip":
		return cm.ZipUtil.Decompress(source, target)
	case ".tar":
		return cm.TarUtil.Decompress(source, target)
	case ".gz":
		// 检查是否是.tar.gz
		if strings.HasSuffix(strings.ToLower(source), ".tar.gz") ||
			strings.HasSuffix(strings.ToLower(source), ".tgz") {
			return cm.TarGzUtil.Decompress(source, target)
		}
		return fmt.Errorf("不支持纯gzip解压，请使用GzipUtil")
	default:
		return fmt.Errorf("不支持的压缩格式: %s", ext)
	}
}

// 包级便捷函数
var (
	DefaultZip     = NewZipUtil()
	DefaultTar     = NewTarUtil()
	DefaultTarGz   = NewTarGzUtil()
	DefaultManager = NewCompressionManager()
)

// ZipCompress 便捷的ZIP压缩函数
func ZipCompress(source, target string) error {
	return DefaultZip.Compress(source, target)
}

// ZipDecompress 便捷的ZIP解压函数
func ZipDecompress(source, target string) error {
	return DefaultZip.Decompress(source, target)
}

// TarGzCompress 便捷的TAR.GZ压缩函数
func TarGzCompress(source, target string) error {
	return DefaultTarGz.Compress(source, target)
}

// TarGzDecompress 便捷的TAR.GZ解压函数
func TarGzDecompress(source, target string) error {
	return DefaultTarGz.Decompress(source, target)
}
