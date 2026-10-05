// Copyright 2013 Julien Schmidt. All rights reserved.
// Adapted from httprouter/gin implementation for ghttp v3
// Use of this source code is governed by a BSD-style license.

package ghttp

import (
	"testing"
)

// BenchmarkTreeAddRoute 基准测试路由注册性能
// BenchmarkTreeAddRoute benchmarks route registration performance
func BenchmarkTreeAddRoute(b *testing.B) {
	routes := []string{
		"/",
		"/hi",
		"/contact",
		"/co",
		"/c",
		"/a",
		"/ab",
		"/doc/",
		"/doc/go_faq.html",
		"/doc/go1.html",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tree := &radixNode{}
		for _, route := range routes {
			tree.addRoute(route, fakeHandler(route))
		}
	}
}

// BenchmarkTreeGetValueStatic 基准测试静态路由查找
// BenchmarkTreeGetValueStatic benchmarks static route lookup
func BenchmarkTreeGetValueStatic(b *testing.B) {
	tree := &radixNode{}
	routes := []string{
		"/",
		"/hi",
		"/contact",
		"/doc/",
		"/doc/go_faq.html",
		"/doc/go1.html",
	}
	for _, route := range routes {
		tree.addRoute(route, fakeHandler(route))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tree.getValue("/doc/go1.html", nil, getSkippedNodes(), false)
	}
}

// BenchmarkTreeGetValueParam 基准测试参数路由查找
// BenchmarkTreeGetValueParam benchmarks param route lookup
func BenchmarkTreeGetValueParam(b *testing.B) {
	tree := &radixNode{}
	routes := []string{
		"/",
		"/cmd/:tool/:sub",
		"/cmd/:tool/",
		"/search/:query",
		"/user_:name",
		"/info/:user/public",
		"/info/:user/project/:project",
	}
	for _, route := range routes {
		tree.addRoute(route, fakeHandler(route))
	}

	b.ResetTimer()
	b.ReportAllocs()

	params := getParams()
	skipped := getSkippedNodes()
	for i := 0; i < b.N; i++ {
		*params = (*params)[:0]
		*skipped = (*skipped)[:0]
		tree.getValue("/info/gordon/project/go", params, skipped, false)
	}
}

// BenchmarkTreeGetValueCatchAll 基准测试通配路由查找
// BenchmarkTreeGetValueCatchAll benchmarks catch-all route lookup
func BenchmarkTreeGetValueCatchAll(b *testing.B) {
	tree := &radixNode{}
	routes := []string{
		"/",
		"/src/*filepath",
		"/files/:dir/*filepath",
	}
	for _, route := range routes {
		tree.addRoute(route, fakeHandler(route))
	}

	b.ResetTimer()
	b.ReportAllocs()

	params := getParams()
	skipped := getSkippedNodes()
	for i := 0; i < b.N; i++ {
		*params = (*params)[:0]
		*skipped = (*skipped)[:0]
		tree.getValue("/src/some/file/path.go", params, skipped, false)
	}
}

// BenchmarkLongestCommonPrefix 基准测试最长公共前缀
// BenchmarkLongestCommonPrefix benchmarks longest common prefix
func BenchmarkLongestCommonPrefix(b *testing.B) {
	a := "/users/:id/posts"
	c := "/users/:id/comments"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		longestCommonPrefix(a, c)
	}
}
