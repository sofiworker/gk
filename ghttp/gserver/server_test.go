package gserver

import "testing"

func TestNew(t *testing.T) {
	server := NewServer()
	group := server.Group("/1")
	{
		group.GET("/test", Wrap(func(ctx *Context) Result {
			return Error(nil)
		}))
	}
}
