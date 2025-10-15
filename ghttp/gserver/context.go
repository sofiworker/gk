package gserver

import (
	"net/http"
)

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Context struct {
	noCopy   noCopy
	Request  *http.Request
	Response http.ResponseWriter
	Params   map[string]string
	Values   map[string]interface{}

	statusCode int
	responded  bool
}
