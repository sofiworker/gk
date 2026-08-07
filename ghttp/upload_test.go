package ghttp

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileHeaderOpen(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "test.txt")
	require.NoError(t, err)
	_, _ = part.Write([]byte("hello world"))
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	_ = req.ParseMultipartForm(32 << 20)

	fh := &FileHeader{FileHeader: req.MultipartForm.File["file"][0]}
	f, err := fh.Open()
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
}

func TestFileHeaderBytes(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.txt")
	_, _ = part.Write([]byte("file content"))
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	_ = req.ParseMultipartForm(32 << 20)

	fh := &FileHeader{FileHeader: req.MultipartForm.File["file"][0]}
	data, err := fh.Bytes()
	require.NoError(t, err)
	assert.Equal(t, "file content", string(data))
}

func TestFileHeaderSave(t *testing.T) {
	tmpDir := t.TempDir()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "save.txt")
	_, _ = part.Write([]byte("saved content"))
	writer.Close()

	req := httptest.NewRequest("POST", "/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	_ = req.ParseMultipartForm(32 << 20)

	fh := &FileHeader{FileHeader: req.MultipartForm.File["file"][0]}
	dst := filepath.Join(tmpDir, "subdir", "saved.txt")
	err := fh.Save(dst)
	require.NoError(t, err)

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "saved content", string(data))
}
