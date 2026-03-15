package gclient

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (r *Request) Download(filePath string) error {
	if strings.TrimSpace(filePath) == "" {
		return errors.New("file path is empty")
	}
	r.SetResponseSaveFileName(filePath)
	r.SetResponseSaveToFile(true)
	resp, err := r.effectiveClient().execute(r)
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("response is nil")
	}
	return nil
}

func (r *Response) Save(filePath string) error {
	if r == nil {
		return nil
	}
	if strings.TrimSpace(filePath) == "" {
		return errors.New("file path is empty")
	}
	if dir := filepath.Dir(filePath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(filePath, r.Body, 0o644)
}

func (r *Response) Size() int64 {
	if r == nil {
		return 0
	}
	return int64(len(r.Body))
}
