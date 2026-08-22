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

// listQuery 查询参数：字段用 query: tag 标注来源，无 tag 字段静默跳过。
// listQuery holds query params; fields are bound via query: tags, untagged
// fields are skipped.
type listQuery struct {
	Keyword string `query:"keyword"`
	Page    int    `query:"page"`
	Size    int    `query:"size"`
}

// authHeaders 鉴权头：字段用 header: tag 标注来源。
// authHeaders holds auth headers bound via header: tags.
type authHeaders struct {
	Token   string `header:"X-Token"`
	Version int    `header:"X-Api-Version"`
}

// echoQuery 单值查询参数示例。
// echoQuery is a single-value query param example.
type echoQuery struct {
	ID string `query:"id"`
}

func main() {
	m := ghttp.New()

	// 全局中间件：RequestID 注入请求 ID 到 context（下游可读，并回显响应头），
	// Logger 记录方法/路径/状态码/耗时。二者演示“前处理 + context 传值”与“后处理 + 读 status”。
	// Global middleware: RequestID injects a request id into context (readable
	// downstream, echoed in the response header); Logger records method/path/
	// status/latency.
	m.Use(ghttp.RequestID(), ghttp.Logger())

	// ListItems: /items?keyword=foo&page=1&size=20
	if err := ghttp.GetParams(m, "/items",
		ghttp.JSON[map[string]any]().Status(http.StatusOK),
		func(ctx context.Context, q listQuery) (map[string]any, error) {
			return map[string]any{
				"endpoint": "/items",
				"query":    q,
				"req_id":   ghttp.RequestIDFromContext(ctx),
			}, nil
		}); err != nil {
		panic(err)
	}

	// SecureItem: /secure 读取 X-Token / X-Api-Version 并回显；缺 token 时用 authorized=false 表达。
	// 注：tag 绑定不做必填校验（缺失即零值）；正式的必填/校验应由中间件或统一错误链承担，
	// 此处示例仅演示 header 绑定，故用响应字段表达鉴权结果而非返回裸 error。
	// SecureItem reads X-Token / X-Api-Version and echoes them; a missing token
	// is expressed via authorized=false. Note: tag binding does not enforce
	// presence (absent means zero value); real presence/validation belongs to
	// middleware or the unified error chain. This example only demonstrates
	// header binding, so it reports auth via a field rather than a bare error.
	if err := ghttp.GetParams(m, "/secure",
		ghttp.JSON[map[string]any]().Status(http.StatusOK),
		func(ctx context.Context, h authHeaders) (map[string]any, error) {
			return map[string]any{
				"authorized": h.Token != "",
				"token":      h.Token,
				"version":    h.Version,
			}, nil
		}); err != nil {
		panic(err)
	}

	// Echo: /echo?id=42 单值查询参数（用单字段结构体承接）。
	// Echo: /echo?id=42 single-value query param (received via a one-field struct).
	if err := ghttp.GetParams(m, "/echo",
		ghttp.JSON[string](),
		func(ctx context.Context, q echoQuery) (string, error) {
			return q.ID, nil
		}); err != nil {
		panic(err)
	}

	addr := ":8080"
	if a := os.Getenv("PORT"); a != "" {
		addr = ":" + a
	}
	log.Printf("listening on %s", addr)
	// New 返回的就是一个 HTTP server，直接 Run 即可，无需再包一层 http.Server。
	// New returns an HTTP server outright — just Run it, no extra http.Server layer.
	if err := m.Run(addr); err != nil {
		log.Fatal(err)
	}
}
