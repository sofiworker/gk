package gserver

import "testing"

func TestRouterGroup_GET(t *testing.T) {
	t.Skip()
	newserver := NewServer()
	newserver.GET("/test", func(c *Context) {
	})
}
