package gserver

import "io"

type Render interface {
	Render(data interface{}) (io.Reader, error)
}

type AutoRender struct {
	prefix  string
	suffix  string
	fileExt string
}

func (a *AutoRender) Render(data interface{}) (io.Reader, error) {
	return nil, nil
}
