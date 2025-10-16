package gserver

import (
	"io"
	"net/http"
)

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Context struct {
	noCopy   noCopy
	Request  *http.Request
	Response http.ResponseWriter
	Writer   io.Writer
	Params   map[string]string
	Values   map[string]interface{}
}
