package gcompress

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ZipUtil struct{}

func NewZipUtil() *ZipUtil {
	return &ZipUtil{}
}

// Compress 压缩文件或目录到ZIP
func (z *ZipUtil) Compress(source, target string) error {
	zipFile, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("创建ZIP文件失败: %v", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 创建ZIP文件头
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return fmt.Errorf("创建文件头失败: %v", err)
		}

		// 设置文件路径（相对路径）
		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil // 跳过根目录
		}

		header.Name = filepath.ToSlash(relPath)

		// 如果是目录，添加斜杠后缀
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate // 设置压缩方法
		}

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("创建ZIP条目失败: %v", err)
		}

		// 如果是文件，写入文件内容
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("打开文件失败: %v", err)
			}
			defer file.Close()

			_, err = io.Copy(writer, file)
			if err != nil {
				return fmt.Errorf("写入ZIP内容失败: %v", err)
			}
		}

		return nil
	})
}

// Decompress 解压ZIP文件
func (z *ZipUtil) Decompress(source, target string) error {
	zipReader, err := zip.OpenReader(source)
	if err != nil {
		return fmt.Errorf("打开ZIP文件失败: %v", err)
	}
	defer zipReader.Close()

	for _, file := range zipReader.File {
		filePath := filepath.Join(target, file.Name)

		// 检查路径安全性，防止目录遍历攻击
		if !strings.HasPrefix(filePath, filepath.Clean(target)+string(os.PathSeparator)) {
			return fmt.Errorf("非法的文件路径: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			// 创建目录
			if err := os.MkdirAll(filePath, os.ModePerm); err != nil {
				return fmt.Errorf("创建目录失败: %v", err)
			}
			continue
		}

		// 确保父目录存在
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return fmt.Errorf("创建父目录失败: %v", err)
		}

		// 创建目标文件
		targetFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return fmt.Errorf("创建目标文件失败: %v", err)
		}

		// 打开ZIP中的文件
		sourceFile, err := file.Open()
		if err != nil {
			targetFile.Close()
			return fmt.Errorf("打开ZIP内文件失败: %v", err)
		}

		// 复制文件内容
		_, err = io.Copy(targetFile, sourceFile)
		targetFile.Close()
		sourceFile.Close()

		if err != nil {
			return fmt.Errorf("解压文件失败: %v", err)
		}
	}

	return nil
}

// ListFiles 列出ZIP文件中的内容
func (z *ZipUtil) ListFiles(source string) ([]string, error) {
	zipReader, err := zip.OpenReader(source)
	if err != nil {
		return nil, err
	}
	defer zipReader.Close()

	var files []string
	for _, file := range zipReader.File {
		files = append(files, file.Name)
	}

	return files, nil
}
