package wsdl

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var errEmptySchemaLocation = errors.New("empty schema location")
var errNonLocalSchemaLocation = errors.New("non-local schema location")

type localLoader struct {
	visited map[string]struct{}
}

func newLocalLoader() *localLoader {
	return &localLoader{
		visited: make(map[string]struct{}),
	}
}

func (l *localLoader) load(baseDir, location string) ([]byte, string, bool, error) {
	if l == nil {
		return nil, "", false, errors.New("nil local loader")
	}

	loc := strings.TrimSpace(location)
	if loc == "" {
		return nil, "", false, errEmptySchemaLocation
	}

	if isRemoteLocation(loc) {
		return nil, "", false, fmt.Errorf("%w: %q", errNonLocalSchemaLocation, location)
	}

	path := loc
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	path = filepath.Clean(path)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", false, fmt.Errorf("resolve schema path %q: %w", location, err)
	}

	if _, ok := l.visited[absPath]; ok {
		return nil, filepath.Dir(absPath), false, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("read schema file %q: %w", absPath, err)
	}

	l.visited[absPath] = struct{}{}
	return data, filepath.Dir(absPath), true, nil
}

func isRemoteLocation(location string) bool {
	u, err := url.Parse(location)
	if err != nil {
		return false
	}

	return u.Scheme != "" || u.Host != ""
}
