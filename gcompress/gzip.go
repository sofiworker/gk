package gcompress

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"
)

// GzipUtil gzip压缩解压工具类
type GzipUtil struct {
	CompressionLevel int // 压缩级别，默认gzip.DefaultCompression
}

// NewGzipUtil 创建gzip工具实例
func NewGzipUtil() *GzipUtil {
	return &GzipUtil{
		CompressionLevel: gzip.DefaultCompression,
	}
}

// WithCompressionLevel 设置压缩级别
func (g *GzipUtil) WithCompressionLevel(level int) *GzipUtil {
	g.CompressionLevel = level
	return g
}

// Compress 压缩字节数据
func (g *GzipUtil) Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, g.CompressionLevel)
	if err != nil {
		return nil, fmt.Errorf("创建gzip writer失败: %v", err)
	}

	defer writer.Close()

	if _, err := writer.Write(data); err != nil {
		return nil, fmt.Errorf("压缩数据失败: %v", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭gzip writer失败: %v", err)
	}

	return buf.Bytes(), nil
}

// Decompress 解压字节数据
func (g *GzipUtil) Decompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("创建gzip reader失败: %v", err)
	}
	defer reader.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return nil, fmt.Errorf("解压数据失败: %v", err)
	}

	return buf.Bytes(), nil
}

// CompressString 压缩字符串
func (g *GzipUtil) CompressString(s string) ([]byte, error) {
	return g.Compress([]byte(s))
}

// DecompressToString 解压为字符串
func (g *GzipUtil) DecompressToString(data []byte) (string, error) {
	decompressed, err := g.Decompress(data)
	if err != nil {
		return "", err
	}
	return string(decompressed), nil
}

// CompressFile 压缩文件
func (g *GzipUtil) CompressFile(sourcePath, targetPath string) error {
	// 读取源文件
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %v", err)
	}
	defer sourceFile.Close()

	// 创建目标文件
	targetFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %v", err)
	}
	defer targetFile.Close()

	// 创建gzip writer
	writer, err := gzip.NewWriterLevel(targetFile, g.CompressionLevel)
	if err != nil {
		return fmt.Errorf("创建gzip writer失败: %v", err)
	}
	defer writer.Close()

	// 设置文件名（可选）
	if !strings.HasSuffix(targetPath, ".gz") {
		writer.Name = strings.TrimSuffix(sourcePath, ".gz")
	}

	// 复制数据
	if _, err := io.Copy(writer, sourceFile); err != nil {
		return fmt.Errorf("压缩文件失败: %v", err)
	}

	return nil
}

// DecompressFile 解压文件
func (g *GzipUtil) DecompressFile(sourcePath, targetPath string) error {
	// 打开源文件
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %v", err)
	}
	defer sourceFile.Close()

	// 创建gzip reader
	reader, err := gzip.NewReader(sourceFile)
	if err != nil {
		return fmt.Errorf("创建gzip reader失败: %v", err)
	}
	defer reader.Close()

	// 创建目标文件
	targetFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %v", err)
	}
	defer targetFile.Close()

	// 复制数据
	if _, err := io.Copy(targetFile, reader); err != nil {
		return fmt.Errorf("解压文件失败: %v", err)
	}

	return nil
}

// IsGzipped 检查数据是否是gzip格式
func (g *GzipUtil) IsGzipped(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	return data[0] == 0x1f && data[1] == 0x8b
}

// 包级函数 - 便捷的静态方法

// Compress 压缩数据（使用默认配置）
func Compress(data []byte) ([]byte, error) {
	return NewGzipUtil().Compress(data)
}

// Decompress 解压数据（使用默认配置）
func Decompress(data []byte) ([]byte, error) {
	return NewGzipUtil().Decompress(data)
}

// CompressString 压缩字符串（使用默认配置）
func CompressString(s string) ([]byte, error) {
	return NewGzipUtil().CompressString(s)
}

// DecompressToString 解压为字符串（使用默认配置）
func DecompressToString(data []byte) (string, error) {
	return NewGzipUtil().DecompressToString(data)
}
