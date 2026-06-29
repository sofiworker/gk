package ghttp

import (
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

// FileHeader wraps multipart.FileHeader with convenience methods.
type FileHeader struct {
	*multipart.FileHeader
}

// Open opens the uploaded file.
func (f *FileHeader) Open() (multipart.File, error) {
	return f.FileHeader.Open()
}

// Bytes reads the entire file content into memory.
func (f *FileHeader) Bytes() ([]byte, error) {
	src, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	return io.ReadAll(src)
}

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
