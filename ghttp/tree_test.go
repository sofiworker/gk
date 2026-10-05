// Copyright 2013 Julien Schmidt. All rights reserved.
// Adapted from httprouter/gin implementation for ghttp v3
// Use of this source code is governed by a BSD-style license.

package ghttp

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

// 用作解决方法，因为我们无法比较函数或其地址
// Used as a workaround since we can't compare functions or their addresses
var fakeHandlerValue string

func fakeHandler(val string) Handler {
	return func(context.Context, *Request, *Response) error {
		fakeHandlerValue = val
		return nil
	}
}

type testRequest struct {
	path       string
	nilHandler bool
	route      string
	ps         Params
}

type testRequests []testRequest

func getParams() *Params {
	ps := make(Params, 0, 20)
	return &ps
}

func getSkippedNodes() *[]skippedNode {
	ps := make([]skippedNode, 0, 20)
	return &ps
}

func checkRequests(t *testing.T, tree *radixNode, requests testRequests, unescapes ...bool) {
	t.Helper()
	unescape := false
	if len(unescapes) >= 1 {
		unescape = unescapes[0]
	}

	for _, request := range requests {
		value := tree.getValue(request.path, getParams(), getSkippedNodes(), unescape)

		if value.handler == nil {
			if !request.nilHandler {
				t.Errorf("handle mismatch for route '%s': Expected non-nil handle", request.path)
			}
		} else if request.nilHandler {
			t.Errorf("handle mismatch for route '%s': Expected nil handle", request.path)
		} else {
			_ = value.handler(context.Background(), nil, nil)
			if fakeHandlerValue != request.route {
				t.Errorf("handle mismatch for route '%s': Wrong handle (%s != %s)", request.path, fakeHandlerValue, request.route)
			}
		}

		if value.params != nil {
			if !reflect.DeepEqual(*value.params, request.ps) {
				t.Errorf("Params mismatch for route '%s': got %v, want %v", request.path, *value.params, request.ps)
			}
		} else if len(request.ps) > 0 {
			t.Errorf("Params mismatch for route '%s': got nil, want %v", request.path, request.ps)
		}
	}
}

func checkPriorities(t *testing.T, n *radixNode) uint32 {
	t.Helper()
	var prio uint32
	for i := range n.children {
		prio += checkPriorities(t, n.children[i])
	}

	if n.handler != nil {
		prio++
	}

	if n.priority != prio {
		t.Errorf(
			"priority mismatch for node '%s': is %d, should be %d",
			n.path, n.priority, prio,
		)
	}

	return prio
}

func TestTreeAddAndGet(t *testing.T) {
	tree := &radixNode{}

	routes := [...]string{
		"/hi",
		"/contact",
		"/co",
		"/c",
		"/a",
		"/ab",
		"/doc/",
		"/doc/go_faq.html",
		"/doc/go1.html",
		"/α",
		"/β",
	}
	for _, route := range routes {
		tree.addRoute(route, fakeHandler(route))
	}

	checkRequests(t, tree, testRequests{
		{"/a", false, "/a", nil},
		{"/", true, "", nil},
		{"/hi", false, "/hi", nil},
		{"/contact", false, "/contact", nil},
		{"/co", false, "/co", nil},
		{"/con", true, "", nil},  // key mismatch
		{"/cona", true, "", nil}, // key mismatch
		{"/no", true, "", nil},   // no matching child
		{"/ab", false, "/ab", nil},
		{"/α", false, "/α", nil},
		{"/β", false, "/β", nil},
	})

	checkPriorities(t, tree)
}

func TestTreeWildcard(t *testing.T) {
	tree := &radixNode{}

	routes := [...]string{
		"/",
		"/cmd/:tool/",
		"/cmd/:tool/:sub",
		"/cmd/whoami",
		"/cmd/whoami/root",
		"/cmd/whoami/root/",
		"/src/*filepath",
		"/search/",
		"/search/:query",
		"/search/gin-gonic",
		"/search/google",
		"/user_:name",
		"/user_:name/about",
		"/files/:dir/*filepath",
		"/doc/",
		"/doc/go_faq.html",
		"/doc/go1.html",
		"/info/:user/public",
		"/info/:user/project/:project",
		"/info/:user/project/golang",
	}
	for _, route := range routes {
		tree.addRoute(route, fakeHandler(route))
	}

	checkRequests(t, tree, testRequests{
		{"/", false, "/", nil},
		{"/cmd/test/", false, "/cmd/:tool/", Params{Param{"tool", "test"}}},
		{"/cmd/test/3", false, "/cmd/:tool/:sub", Params{Param{Key: "tool", Value: "test"}, Param{Key: "sub", Value: "3"}}},
		{"/cmd/whoami", false, "/cmd/whoami", nil},
		{"/cmd/whoami/root", false, "/cmd/whoami/root", nil},
		{"/cmd/whoami/root/", false, "/cmd/whoami/root/", nil},
		{"/src/", false, "/src/*filepath", Params{Param{Key: "filepath", Value: "/"}}},
		{"/src/some/file.png", false, "/src/*filepath", Params{Param{Key: "filepath", Value: "/some/file.png"}}},
		{"/search/", false, "/search/", nil},
		{"/search/gin", false, "/search/:query", Params{Param{"query", "gin"}}},
		{"/search/gin-gonic", false, "/search/gin-gonic", nil},
		{"/search/google", false, "/search/google", nil},
		{"/user_gopher", false, "/user_:name", Params{Param{Key: "name", Value: "gopher"}}},
		{"/user_gopher/about", false, "/user_:name/about", Params{Param{Key: "name", Value: "gopher"}}},
		{"/files/js/inc/framework.js", false, "/files/:dir/*filepath", Params{Param{Key: "dir", Value: "js"}, Param{Key: "filepath", Value: "/inc/framework.js"}}},
		{"/info/gordon/public", false, "/info/:user/public", Params{Param{Key: "user", Value: "gordon"}}},
		{"/info/gordon/project/go", false, "/info/:user/project/:project", Params{Param{Key: "user", Value: "gordon"}, Param{Key: "project", Value: "go"}}},
		{"/info/gordon/project/golang", false, "/info/:user/project/golang", Params{Param{Key: "user", Value: "gordon"}}},
	})

	checkPriorities(t, tree)
}

func catchPanic(testFunc func()) (recv any) {
	defer func() {
		recv = recover()
	}()

	testFunc()
	return
}

type testRoute struct {
	path     string
	conflict bool
}

func testRoutes(t *testing.T, routes []testRoute) {
	t.Helper()
	tree := &radixNode{}

	for _, route := range routes {
		recv := catchPanic(func() {
			tree.addRoute(route.path, nil)
		})

		if route.conflict {
			if recv == nil {
				t.Errorf("no panic for conflicting route '%s'", route.path)
			}
		} else if recv != nil {
			t.Errorf("unexpected panic for route '%s': %v", route.path, recv)
		}
	}
}

func TestTreeWildcardConflict(t *testing.T) {
	routes := []testRoute{
		{"/cmd/:tool/:sub", false},
		{"/cmd/vet", false},
		{"/foo/bar", false},
		{"/foo/:name", false},
		{"/foo/:names", true},
		{"/cmd/*path", true},
		{"/src/*filepath", false},
		{"/src/:file", true},
		{"/search/:query", false},
		{"/search/valid", false},
		{"/user_:name", false},
		{"/user_x", false},
	}
	testRoutes(t, routes)
}

func TestTreeDuplicatePath(t *testing.T) {
	tree := &radixNode{}

	routes := [...]string{
		"/",
		"/doc/",
		"/src/*filepath",
		"/search/:query",
		"/user_:name",
	}
	for _, route := range routes {
		recv := catchPanic(func() {
			tree.addRoute(route, fakeHandler(route))
		})
		if recv != nil {
			t.Fatalf("panic inserting route '%s': %v", route, recv)
		}

		// Add again
		recv = catchPanic(func() {
			tree.addRoute(route, nil)
		})
		if recv == nil {
			t.Fatalf("no panic while inserting duplicate route '%s", route)
		}
	}

	checkRequests(t, tree, testRequests{
		{"/", false, "/", nil},
		{"/doc/", false, "/doc/", nil},
		{"/src/some/file.png", false, "/src/*filepath", Params{Param{"filepath", "/some/file.png"}}},
		{"/search/someth!ng+in+ünìcodé", false, "/search/:query", Params{Param{"query", "someth!ng+in+ünìcodé"}}},
		{"/user_gopher", false, "/user_:name", Params{Param{"name", "gopher"}}},
	})
}

func TestLongestCommonPrefix(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 0},
		{"", "a", 0},
		{"a", "a", 1},
		{"a", "b", 0},
		{"ab", "a", 1},
		{"a", "ab", 1},
		{"abc", "abc", 3},
		{"abc", "abd", 2},
		{"abc", "xyz", 0},
		{"/users/:id", "/users/:name", 8},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s+%s", tt.a, tt.b), func(t *testing.T) {
			got := longestCommonPrefix(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("longestCommonPrefix(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
