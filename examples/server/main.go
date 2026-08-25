// example: ghttp 生产级 HTTP Server 完整示例。
// 演示：Engine 一步式 Run + 超时配置、生产中间件栈（Recovery/RequestID/Logger/CORS/
// Timeout，CORS 用普通 Use 即可处理预检）、typed 入口、静态资源（embed.FS）、健康/
// 就绪探针、信号驱动的优雅关闭。本文件仅用于演示，不作为包的一部分。
//
// example: a complete production-grade ghttp HTTP server, demonstrating Engine's
// one-liner Run with timeouts, the production middleware stack (Recovery/RequestID/
// Logger/CORS/Timeout — CORS handles preflight via a plain Use), typed entries,
// static assets from embed.FS, health/readiness probes, and signal-driven graceful
// shutdown. For demonstration only.
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	ghttp "github.com/sofiworker/gk/ghttp"
)

//go:embed web
var webFS embed.FS

// listQuery 列表查询参数，经 query: tag 绑定。
// listQuery holds list query params bound via query: tags.
type listQuery struct {
	Keyword string `query:"keyword"`
	Page    int    `query:"page"`
	Size    int    `query:"size"`
}

// createReq 创建请求体，经 JSON 解码。
// createReq is a create request body decoded from JSON.
type createReq struct {
	Name  string `json:"name"`
	Price int    `json:"price"`
}

func main() {
	// 一个 Server 就是一个 HTTP server：New 同时配置路由行为与监听参数（超时等），
	// 随后在它上面挂中间件、注册路由，最后 Run / Shutdown。
	// One Server IS one HTTP server: New configures both routing behavior and
	// listener parameters (timeouts, ...); then attach middleware, register routes,
	// and finally Run / Shutdown on it.
	m := ghttp.New(
		ghttp.WithAddr(addrFromEnv()),
		ghttp.WithReadHeaderTimeout(5*time.Second),
		ghttp.WithReadTimeout(15*time.Second),
		ghttp.WithWriteTimeout(15*time.Second),
		ghttp.WithIdleTimeout(60*time.Second),
	)

	// 生产中间件栈（顺序即洋葱外→内）：
	//   Recovery 最外层，兜住其余中间件与 handler 的 panic；
	//   RequestID 注入请求 ID；Logger 记录状态与耗时；
	//   CORS 处理跨域与预检；Timeout 为每个请求设协作式超时。
	// CORS 作为普通中间件即可正确应答未注册 OPTIONS 路由的预检——因为 ghttp 的全局
	// 中间件在 ServeHTTP 期组装、对未命中路由亦生效（gin 语义），无需任何路由前变通。
	// Production middleware stack (outermost→innermost): Recovery wraps everything
	// so it catches panics from the rest; RequestID injects an id; Logger records
	// status/latency; CORS handles cross-origin + preflight; Timeout sets a
	// cooperative per-request deadline.
	// CORS as a plain middleware correctly answers preflight for unregistered
	// OPTIONS routes — ghttp's global middleware is assembled at ServeHTTP time and
	// applies to route misses too (gin semantics), needing no pre-routing workaround.
	m.Use(
		ghttp.Recovery(),
		ghttp.RequestID(),
		ghttp.Logger(),
		ghttp.CORS(ghttp.CORSDefault()),
		ghttp.Timeout(5*time.Second),
	)

	registerAPI(m)
	registerStatic(m)
	registerProbes(m)

	runWithGracefulShutdown(m)
}

// registerAPI 注册 typed 业务端点。
// registerAPI registers typed business endpoints.
func registerAPI(m *ghttp.Server) {
	// GET /api/items?keyword=&page=&size= — params 经 tag 绑定，输出 JSON。
	must(ghttp.GetParams(m, "/api/items",
		ghttp.JSON[map[string]any]().Status(http.StatusOK),
		func(ctx context.Context, q listQuery) (map[string]any, error) {
			return map[string]any{
				"keyword": q.Keyword,
				"page":    q.Page,
				"size":    q.Size,
				"req_id":  ghttp.RequestIDFromContext(ctx),
			}, nil
		}))

	// POST /api/items — 请求体经 JSONBody[createReq] 解码，输出 201。
	must(ghttp.PostBody(m, "/api/items",
		ghttp.JSONBody[createReq](),
		ghttp.JSON[map[string]any]().Status(http.StatusCreated),
		func(ctx context.Context, in createReq) (map[string]any, error) {
			if in.Name == "" {
				// 返回错误：交由核心错误路径（当前映射 500，错误链落地后细化）。
				// Return an error: handled by the core error path (currently 500;
				// refined once the error chain lands).
				return nil, errors.New("name is required")
			}
			return map[string]any{"created": in.Name, "price": in.Price}, nil
		}))
}

// registerStatic 从 embed.FS 提供静态资源（SPA 回退演示）。
// registerStatic serves static assets from embed.FS (with SPA fallback demo).
func registerStatic(m *ghttp.Server) {
	// embed 的 FS 带有顶层 "web" 目录，用 fs.Sub 剥掉，使 /app/ 映射到其内容。
	// The embedded FS has a top-level "web" dir; strip it with fs.Sub so /app/
	// maps to its contents.
	sub, err := fs.Sub(webFS, "web")
	must(err)
	must(ghttp.StaticFS(m, "/app/", sub, ghttp.WithSPAFallback()))
}

// registerProbes 注册健康与就绪探针，并演示运行时就绪门闸。
// registerProbes registers health/readiness probes and a runtime readiness gate.
func registerProbes(m *ghttp.Server) {
	must(ghttp.Health(m, "/healthz"))

	// 就绪门闸：进程刚起时未就绪，模拟初始化后置为就绪。
	// Readiness gate: not ready at startup; marked ready after a simulated init.
	gate, checker := ghttp.NewReadinessGate("startup")
	must(ghttp.Ready(m, "/readyz", 2*time.Second, checker))
	go func() {
		time.Sleep(500 * time.Millisecond) // 模拟初始化 / simulate init
		gate.Set(true, nil)
		log.Printf("readiness: gate opened")
	}()
}

// runWithGracefulShutdown 启动 Server 并在收到 SIGINT/SIGTERM 时优雅关闭。监听地址与
// 超时已在 New 处配置，故 Run 传空串沿用配置。
// runWithGracefulShutdown starts the Server and gracefully stops it on
// SIGINT/SIGTERM. The address and timeouts were configured at New, so Run takes an
// empty string to reuse them.
func runWithGracefulShutdown(m *ghttp.Server) {
	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", addrFromEnv())
		errCh <- m.Run("")
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, ghttp.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	case sig := <-stop:
		log.Printf("received %s, shutting down gracefully...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			log.Fatalf("graceful shutdown failed: %v", err)
		}
		log.Printf("shutdown complete")
	}
}

// addrFromEnv 从 PORT 环境变量取监听地址，默认 :8080。
// addrFromEnv reads the listen address from PORT, defaulting to :8080.
func addrFromEnv() string {
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

// must 在初始化期遇错即崩，示例代码从简。
// must panics on init-time errors; kept simple for an example.
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
