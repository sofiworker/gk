package ghttp

import (
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Renderer is the template rendering interface.
type Renderer interface {
	HTML(name string, data interface{}, w io.Writer) error
}

// GoRenderer uses Go's html/template, gin-style.
type GoRenderer struct {
	dir     string
	ext     string
	funcMap template.FuncMap
	reload  bool
	mu      sync.RWMutex
	cache   map[string]*template.Template
}

// NewRenderer creates a new GoRenderer.
func NewRenderer(dir, ext string, funcMap template.FuncMap, reload bool) *GoRenderer {
	if ext == "" {
		ext = ".html"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return &GoRenderer{
		dir:     dir,
		ext:     ext,
		funcMap: funcMap,
		reload:  reload,
		cache:   make(map[string]*template.Template),
	}
}

func (r *GoRenderer) HTML(name string, data interface{}, w io.Writer) error {
	t, err := r.getTemplate(name)
	if err != nil {
		return err
	}
	return t.Execute(w, data)
}

func (r *GoRenderer) getTemplate(name string) (*template.Template, error) {
	if !r.reload {
		r.mu.RLock()
		t, ok := r.cache[name]
		r.mu.RUnlock()
		if ok {
			return t, nil
		}
	}

	tmplPath := filepath.Join(r.dir, name+r.ext)
	content, err := os.ReadFile(tmplPath)
	if err != nil {
		return nil, err
	}

	t := template.New(name).Funcs(r.funcMap)
	t, err = t.Parse(string(content))
	if err != nil {
		return nil, err
	}

	if !r.reload {
		r.mu.Lock()
		r.cache[name] = t
		r.mu.Unlock()
	}
	return t, nil
}
