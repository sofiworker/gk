package ghttp

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type safeFS struct {
	root string
}

// NewSafeFS creates an http.FileSystem that confines all opens beneath root.
func NewSafeFS(root string) (http.FileSystem, error) {
	if strings.TrimSpace(root) == "" {
		return nil, ErrStaticRootRequired
	}
	return &safeFS{root: root}, nil
}

func (fsys *safeFS) Open(name string) (http.File, error) {
	cleaned, err := cleanVFSName(name)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	root, err := os.OpenRoot(fsys.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(cleaned)
}

func cleanVFSName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fs.ErrNotExist
	}
	unescaped, err := url.PathUnescape(name)
	if err != nil {
		return "", fs.ErrNotExist
	}
	name = unescaped
	if strings.Contains(name, "\\") {
		return "", fs.ErrNotExist
	}

	name = strings.TrimLeft(name, "/")
	if name == "" || name == "." {
		return ".", nil
	}
	for _, segment := range strings.Split(name, "/") {
		switch segment {
		case "", ".":
			continue
		case "..":
			return "", fs.ErrNotExist
		default:
			if hasWindowsDrivePrefix(segment) {
				return "", fs.ErrNotExist
			}
		}
	}

	cleaned := path.Clean(name)
	if cleaned == "." {
		return ".", nil
	}
	if !fs.ValidPath(cleaned) {
		return "", fs.ErrNotExist
	}
	if !filepath.IsLocal(cleaned) {
		return "", fs.ErrNotExist
	}
	if hasWindowsDrivePrefix(cleaned) {
		return "", fs.ErrNotExist
	}
	return cleaned, nil
}

func hasWindowsDrivePrefix(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	c := name[0]
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}
