package gcompress

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
)

type TarGzUtil struct{}

func NewTarGzUtil() *TarGzUtil {
	return &TarGzUtil{}
}

// Compress 创建TAR.GZ压缩归档
func (tg *TarGzUtil) Compress(source, target string) error {
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

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

// Decompress 解压TAR.GZ文件
func (tg *TarGzUtil) Decompress(source, target string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

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
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
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
