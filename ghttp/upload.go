package ghttp

import (
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

// FileHeader 包装 multipart.FileHeader 并提供便捷方法。
// FileHeader wraps multipart.FileHeader with convenience methods.
type FileHeader struct {
	*multipart.FileHeader
}

// Open 打开上传的文件。
// Open opens the uploaded file.
func (f *FileHeader) Open() (multipart.File, error) {
	return f.FileHeader.Open()
}

// Bytes 将整个文件内容读入内存。
// Bytes reads the entire file content into memory.
func (f *FileHeader) Bytes() ([]byte, error) {
	src, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	return io.ReadAll(src)
}

// Save 将上传文件写入指定路径。
// Save writes the uploaded file to the given path.
func (f *FileHeader) Save(dst string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}
