package gcompress

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
)

type TarUtil struct{}

func NewTarUtil() *TarUtil {
	return &TarUtil{}
}

// Compress 创建TAR归档
func (t *TarUtil) Compress(source, target string) error {
	tarFile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer tarFile.Close()

	tarWriter := tar.NewWriter(tarFile)
	defer tarWriter.Close()

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 创建tar头信息
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// 设置相对路径
		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		header.Name = relPath

		// 写入头信息
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// 如果是文件，写入内容
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(tarWriter, file)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// Decompress 解压TAR归档
func (t *TarUtil) Decompress(source, target string) error {
	tarFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer tarFile.Close()

	tarReader := tar.NewReader(tarFile)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(target, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			// 创建目录
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			// 创建文件
			if err := os.MkdirAll(filepath.Dir(targetPath), os.ModePerm); err != nil {
				return err
			}

			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return err
			}
			file.Close()
		}
	}

	return nil
}
