// example: ghttp 实时能力示例 —— Server-Sent Events (SSE) 与 WebSocket。
// 演示:用 SSEWriter 在 RawHandle 上推送事件流;用 ServeWS 一行注册 WebSocket 回声端点,
// 以及用 WSUpgrader 在 RawHandle 内手动升级。SSE 是纯 HTTP 长连接(无需 Hijack),
// WebSocket 经 gorilla/websocket 升级(ghttp 只负责让 Response 可 Hijack)。
// 本文件仅用于演示,不作为包的一部分。
//
// example: ghttp real-time features — Server-Sent Events (SSE) and WebSocket.
// Demonstrates pushing an event stream with SSEWriter over a RawHandle; a
// one-liner WebSocket echo endpoint via ServeWS; and a manual upgrade with
// WSUpgrader inside a RawHandle. SSE is a plain-HTTP long connection (no Hijack);
// WebSocket upgrades via gorilla/websocket (ghttp only makes Response hijackable).
// For demonstration only.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	ghttp "github.com/sofiworker/gk/ghttp"
)

func main() {
	m := ghttp.New(ghttp.WithAddr(addrFromEnv()))

	// —— SSE:每秒推送一条 server time 事件,直到客户端断开。——
	// —— SSE: push a server-time event every second until the client disconnects. ——
	if err := m.RawHandle(http.MethodGet, "/sse/time", func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		w := ghttp.NewSSEWriter(resp)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				// 请求上下文取消(客户端断开 / 服务器关闭)→ 结束流。
				// Request context canceled (client gone / server shutdown) → end stream.
				return nil
			case t := <-ticker.C:
				if err := w.SendEvent("time", t.UTC().Format(time.RFC3339)); err != nil {
					// 写失败通常意味着客户端已断开,退出即可。
					// A write failure usually means the client disconnected; just exit.
					return nil
				}
			}
		}
	}); err != nil {
		log.Fatal(err)
	}

	// —— WebSocket(语法糖):一行注册回声端点。——
	// —— WebSocket (sugar): a one-liner echo endpoint. ——
	if err := ghttp.ServeWS(m, "/ws/echo", nil, func(ctx context.Context, req *ghttp.Request, conn *websocket.Conn) error {
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return err // 客户端关闭 → 正常退出。Client close → normal exit.
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return err
			}
		}
	}); err != nil {
		log.Fatal(err)
	}

	// —— WebSocket(手动升级):在 RawHandle 内用自定义 Upgrader 升级,握手后推一条欢迎消息。——
	// —— WebSocket (manual upgrade): upgrade with a custom Upgrader inside a RawHandle. ——
	up := ghttp.NewWSUpgrader(
		ghttp.WithWSReadBufferSize(4096),
		ghttp.WithWSWriteBufferSize(4096),
		ghttp.WithWSCheckOrigin(func(r *http.Request) bool { return true }), // 演示放开跨源;生产请按域名校验。demo only.
	)
	if err := m.RawHandle(http.MethodGet, "/ws/welcome", func(ctx context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		conn, err := up.Upgrade(resp, req, nil)
		if err != nil {
			return err
		}
		defer conn.Close()
		return conn.WriteMessage(websocket.TextMessage, []byte("welcome"))
	}); err != nil {
		log.Fatal(err)
	}

	log.Printf("listening on %s (GET /sse/time, WS /ws/echo, WS /ws/welcome)", addrFromEnv())
	// addr 已由 WithAddr 配置,Run("") 沿用之。
	// addr is configured via WithAddr; Run("") reuses it.
	if err := m.Run(""); err != nil && err != ghttp.ErrServerClosed {
		log.Fatal(err)
	}
}

// addrFromEnv 从 ADDR 环境变量读监听地址,默认 :8080。
// addrFromEnv reads the listen address from ADDR, defaulting to :8080.
func addrFromEnv() string {
	if a := os.Getenv("ADDR"); a != "" {
		return a
	}
	return ":8080"
}
