// example 1: ghttp 最小使用示例
// 本文件仅用于演示，不作为包的一部分。
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	ghttp "github.com/sofiworker/gk/ghttp"
)

// 查询结构体：多字段用 Query(...) 组合
type listQuery struct {
	Page    int
	Size    int
	Keyword string
}

// Header 结构体：多字段用 Header(...) 组合，.Required() 标记必填
type authHeaders struct {
	Token   string `json:"token"`
	Version int    `json:"version"`
}

func main() {
	m := ghttp.New()

	// 全局中间件：RequestID 注入请求 ID 到 context（下游可读，并回显响应头），
	// Logger 记录方法/路径/状态码/耗时。二者演示"前处理 + context 传值"与"后处理 + 读 status"。
	m.Use(ghttp.RequestID(), ghttp.Logger())

	// ListItems: /items?keyword=foo&page=1&size=20
	if err := ghttp.Get(m, "/items",
		ghttp.NoInput[ghttp.NoPath](),
		ghttp.Query(
			ghttp.QStr("keyword", func(q *listQuery, v string) { q.Keyword = v }),
			ghttp.QInt("page", func(q *listQuery, v int) { q.Page = v }),
			ghttp.QInt("size", func(q *listQuery, v int) { q.Size = v }),
		),
		ghttp.NoInput[ghttp.NoBody](),
		ghttp.JSON[map[string]any]().Status(http.StatusOK),
		func(ctx context.Context, in ghttp.RequestInput[ghttp.NoPath, listQuery, ghttp.NoBody]) (map[string]any, error) {
			return map[string]any{
				"endpoint": "/items",
				"query":    in.Query,
				"req_id":   ghttp.RequestIDFromContext(ctx),
			}, nil
		}); err != nil {
		panic(err)
	}

	// SecureItem: /secure 需要 X-Token（必填） + X-Api-Version
	if err := ghttp.Get(m, "/secure",
		ghttp.NoInput[ghttp.NoPath](),
		ghttp.Header(
			ghttp.HStr("X-Token", func(h *authHeaders, v string) { h.Token = v }).Required(), // .Required() 标记
			ghttp.HInt("X-Api-Version", func(h *authHeaders, v int) { h.Version = v }),
		),
		ghttp.NoInput[ghttp.NoBody](),
		ghttp.JSON[authHeaders]().Status(http.StatusOK),
		func(ctx context.Context, in ghttp.RequestInput[ghttp.NoPath, authHeaders, ghttp.NoBody]) (authHeaders, error) {
			return in.Query, nil
		}); err != nil {
		panic(err)
	}

	// 可选场景：单值源无需包结构体（如 /echo?id=42）
	if err := ghttp.Get(m, "/echo",
		ghttp.NoInput[ghttp.NoPath](),
		ghttp.QueryString("id"), // 直接 InputSource[string]
		ghttp.NoInput[ghttp.NoBody](),
		ghttp.JSON[string](),
		func(ctx context.Context, in ghttp.RequestInput[ghttp.NoPath, string, ghttp.NoBody]) (string, error) {
			return in.Query, nil
		}); err != nil {
		panic(err)
	}

	addr := ":8080"
	if a := os.Getenv("PORT"); a != "" {
		addr = ":" + a
	}
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, m); err != nil {
		log.Fatal(err)
	}
}
