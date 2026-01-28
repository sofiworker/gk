package gserver

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"path/filepath"
	"strings"
	"sync"
)

type Render interface {
	Render(data interface{}) (io.Reader, error)
}

// HTMLRenderer is an optional extension for template rendering.
// If the configured Render also implements HTMLRenderer, Context.HTML will call it.
type HTMLRenderer interface {
	RenderHTML(name string, data interface{}) (io.Reader, error)
}

type AutoRender struct {
	prefix  string
	suffix  string
	fileExt string

	mu    sync.RWMutex
	tmpls map[string]*template.Template
}

func (a *AutoRender) Render(data interface{}) (io.Reader, error) {
	if data == nil {
		return bytes.NewReader(nil), nil
	}
	switch v := data.(type) {
	case io.Reader:
		return v, nil
	case []byte:
		return bytes.NewReader(v), nil
	case string:
		return strings.NewReader(v), nil
	default:
		return nil, errors.New("unsupported render data type")
	}
}

func NewAutoRender(prefix string) *AutoRender {
	return &AutoRender{
		prefix:  prefix,
		fileExt: ".html",
		tmpls:   make(map[string]*template.Template),
	}
}

func (a *AutoRender) WithSuffix(suffix string) *AutoRender {
	a.suffix = suffix
	return a
}

func (a *AutoRender) WithExt(ext string) *AutoRender {
	a.fileExt = ext
	return a
}

func (a *AutoRender) RenderHTML(name string, data interface{}) (io.Reader, error) {
	t, err := a.template(name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

func (a *AutoRender) template(name string) (*template.Template, error) {
	if name == "" {
		return nil, errors.New("template name is empty")
	}
	key := name
	a.mu.RLock()
	if t, ok := a.tmpls[key]; ok {
		a.mu.RUnlock()
		return t, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()
	if t, ok := a.tmpls[key]; ok {
		return t, nil
	}

	path := a.templatePath(name)
	t, err := template.ParseFiles(path)
	if err != nil {
		return nil, err
	}
	a.tmpls[key] = t
	return t, nil
}

func (a *AutoRender) templatePath(name string) string {
	ext := a.fileExt
	if ext == "" {
		ext = ".html"
	}
	if ext[0] != '.' {
		ext = "." + ext
	}
	file := name + a.suffix + ext
	if a.prefix == "" {
		return file
	}
	return filepath.Join(a.prefix, file)
}
