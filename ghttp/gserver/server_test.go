package gserver

import (
	"fmt"
	"testing"
)

func TestNew(t *testing.T) {
	server := NewServer()
	group := server.Group("/v1")
	{
		group.GET("/test", func(c *Context) {
			fmt.Println("test")
		})
		group.POST("/test2", func(c *Context) {
			fmt.Println("test2")
		})
	}
	group2 := group.Group("/v1.1/test")
	{
		group2.GET("/test", func(c *Context) {
			fmt.Println("test v1.1")
		})
	}

	group3 := group2.Group("/v1.12/test")
	{
		group3.POST("/test/:id", func(c *Context) {
			fmt.Println("test v1.12")
		})
		group3.POST("/test/*id", func(c *Context) {
			fmt.Println("test v1.12")
		})
	}

	server.GET("/test/:name/:last_name/*wild", func(c *Context) {

	})

	select {}
}

func TestRoute(t *testing.T) {
	// Test routes
	routes := []string{
		"/test",
		"/test/",
		"/simple",
		"/project/:name",
		"/",
		"/news/home",
		"/news",
		"/simple-two/one",
		"/simple-two/one-two",
		"/project/:name/build/*params",
		"/project/:name/bui",
		"/user/:id/status",
		"/user/:id",
		"/user/:id/profile",
		"/a/b/c/d/e/f/g/h/i/j/k",
		"/a/b/c/d/e/f/g/h/i/j/k/:id",
		"/a/b/c/d/e/f/g/h/i/j/k/:id/*params",
		"/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z",
		"/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z/:id",
		"/a/b/c/d/e/f/g/h/i/j/k/l/m/n/o/p/q/r/s/t/u/v/w/x/y/z/:id/*params",
		"/a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a",
		"/a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/:a/*params",
	}
	matcher := newServerMatcher()
	for _, route := range routes {
		err := matcher.AddRoute("GET", route, func(c *Context) {})
		if err != nil {
			t.Errorf("Failed to add route %s: %v", route, err)
		}
	}

	for _, route := range routes {
		matchResult := matcher.Match("GET", route)
		fmt.Println(matchResult == nil)
	}
}
