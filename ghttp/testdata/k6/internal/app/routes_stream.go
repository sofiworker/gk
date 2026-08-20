package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sofiworker/gk/ghttp"
)

const (
	maxStreamCount        = 100
	maxStreamInterval     = time.Second
	maxStreamMessageBytes = 64 * 1024
)

// sseParams 聚合 SSE 端点的查询参数。
// sseParams holds the query params for SSE endpoints.
type sseParams struct {
	Count      int
	IntervalMS int
}

// wsParams 聚合 WebSocket 端点的查询参数。
// wsParams holds the query params for WebSocket endpoints.
type wsParams struct {
	DelayMS int
	Code    int
}

func registerStreams(server *ghttp.Server, metrics *RuntimeMetrics) {
	server.MustMount(ghttp.SSEOperation("/sse/once", ghttp.NoInput(), trackedSSEOnce(metrics, handleSSEOnce)))
	server.MustMount(ghttp.SSEOperation("/sse/multi", ghttp.InputFunc(sseCountInput), trackedSSEMulti(metrics, handleSSEMulti)))
	server.MustMount(ghttp.SSEOperation("/sse/heartbeat", ghttp.InputFunc(sseCountInput), trackedSSEHeartbeat(metrics, handleSSEHeartbeat)))
	server.MustMount(ghttp.SSEOperation("/sse/slow", ghttp.InputFunc(sseSlowInput), trackedSSESlow(metrics, handleSSESlow)))

	registerWebSocket(server, metrics, "/ws/echo", 0)
	registerWebSocket(server, metrics, "/ws/binary", 0)
	registerWebSocket(server, metrics, "/ws/multi", 0)
	registerWebSocket(server, metrics, "/ws/slow", 0)
	server.MustMount(ghttp.WebSocketOperation("/ws/close", ghttp.InputFunc(wsCloseInput), trackedWebSocketClose(metrics, handleWebSocketClose)))
}

func sseCountInput(view ghttp.RequestView) (sseParams, error) {
	count, err := boundedStreamInt(view.Query().Get("count"), 1, maxStreamCount)
	if err != nil {
		return sseParams{}, err
	}
	return sseParams{Count: count}, nil
}

func sseSlowInput(view ghttp.RequestView) (sseParams, error) {
	count, err := boundedStreamInt(view.Query().Get("count"), 1, maxStreamCount)
	if err != nil {
		return sseParams{}, err
	}
	intervalMS, err := boundedStreamInt(view.Query().Get("interval_ms"), 0, int(maxStreamInterval.Milliseconds()))
	if err != nil {
		return sseParams{}, err
	}
	return sseParams{Count: count, IntervalMS: intervalMS}, nil
}

func wsEchoInput(view ghttp.RequestView) (wsParams, error) {
	delayMS, err := boundedStreamInt(view.Query().Get("delay_ms"), 0, int(maxStreamInterval.Milliseconds()))
	if err != nil {
		return wsParams{}, err
	}
	return wsParams{DelayMS: delayMS}, nil
}

func wsCloseInput(view ghttp.RequestView) (wsParams, error) {
	code, err := boundedStreamInt(view.Query().Get("code"), websocket.CloseNormalClosure, 4999)
	if err != nil || code < 1000 {
		code = websocket.CloseNormalClosure
	}
	return wsParams{Code: code}, nil
}

func trackedSSEOnce(metrics *RuntimeMetrics, handler func(context.Context, ghttp.EmptyInput, *ghttp.SSEWriter) error) func(context.Context, ghttp.EmptyInput, *ghttp.SSEWriter) error {
	return func(ctx context.Context, _ ghttp.EmptyInput, stream *ghttp.SSEWriter) error {
		closeStream := metrics.OpenSSE()
		defer closeStream()
		return handler(ctx, ghttp.EmptyInput{}, stream)
	}
}

func trackedSSEMulti(metrics *RuntimeMetrics, handler func(context.Context, sseParams, *ghttp.SSEWriter) error) func(context.Context, sseParams, *ghttp.SSEWriter) error {
	return func(ctx context.Context, in sseParams, stream *ghttp.SSEWriter) error {
		closeStream := metrics.OpenSSE()
		defer closeStream()
		return handler(ctx, in, stream)
	}
}

func trackedSSEHeartbeat(metrics *RuntimeMetrics, handler func(context.Context, sseParams, *ghttp.SSEWriter) error) func(context.Context, sseParams, *ghttp.SSEWriter) error {
	return func(ctx context.Context, in sseParams, stream *ghttp.SSEWriter) error {
		closeStream := metrics.OpenSSE()
		defer closeStream()
		return handler(ctx, in, stream)
	}
}

func trackedSSESlow(metrics *RuntimeMetrics, handler func(context.Context, sseParams, *ghttp.SSEWriter) error) func(context.Context, sseParams, *ghttp.SSEWriter) error {
	return func(ctx context.Context, in sseParams, stream *ghttp.SSEWriter) error {
		closeStream := metrics.OpenSSE()
		defer closeStream()
		return handler(ctx, in, stream)
	}
}

func handleSSEOnce(_ context.Context, _ ghttp.EmptyInput, stream *ghttp.SSEWriter) error {
	if err := stream.Retry(1000); err != nil {
		return err
	}
	return stream.WriteEventWithID("message", "1", "hello\nworld")
}

func handleSSEMulti(ctx context.Context, in sseParams, stream *ghttp.SSEWriter) error {
	for index := 1; index <= in.Count; index++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := stream.WriteEventWithID("message", strconv.Itoa(index), fmt.Sprintf("message-%d", index)); err != nil {
			return err
		}
	}
	return nil
}

func handleSSEHeartbeat(ctx context.Context, in sseParams, stream *ghttp.SSEWriter) error {
	for index := 1; index <= in.Count; index++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := stream.WriteComment(fmt.Sprintf("heartbeat-%d", index)); err != nil {
			return err
		}
		if err := stream.WriteEvent("message", fmt.Sprintf("heartbeat-%d", index)); err != nil {
			return err
		}
	}
	return nil
}

func handleSSESlow(ctx context.Context, in sseParams, stream *ghttp.SSEWriter) error {
	interval := time.Duration(in.IntervalMS) * time.Millisecond
	for index := 1; index <= in.Count; index++ {
		if err := stream.WriteEventWithID("message", strconv.Itoa(index), fmt.Sprintf("slow-%d", index)); err != nil {
			return err
		}
		if index == in.Count {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func registerWebSocket(server *ghttp.Server, metrics *RuntimeMetrics, path string, _ time.Duration) {
	server.MustMount(ghttp.WebSocketOperation(path, ghttp.InputFunc(wsEchoInput), trackedWebSocketEcho(metrics, handleWebSocketMessages)))
}

func trackedWebSocketEcho(metrics *RuntimeMetrics, handler func(context.Context, wsParams, *ghttp.WebSocketConn) error) func(context.Context, wsParams, *ghttp.WebSocketConn) error {
	return func(ctx context.Context, in wsParams, conn *ghttp.WebSocketConn) error {
		closeStream := metrics.OpenWebSocket()
		defer closeStream()
		return handler(ctx, in, conn)
	}
}

func trackedWebSocketClose(metrics *RuntimeMetrics, handler func(context.Context, wsParams, *ghttp.WebSocketConn) error) func(context.Context, wsParams, *ghttp.WebSocketConn) error {
	return func(ctx context.Context, in wsParams, conn *ghttp.WebSocketConn) error {
		closeStream := metrics.OpenWebSocket()
		defer closeStream()
		return handler(ctx, in, conn)
	}
}

func handleWebSocketMessages(ctx context.Context, in wsParams, conn *ghttp.WebSocketConn) error {
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if len(data) > maxStreamMessageBytes {
			return conn.WriteMessage(ghttp.WebSocketMessageType(websocket.CloseMessage), websocket.FormatCloseMessage(websocket.CloseMessageTooBig, "message too large"))
		}
		if in.DelayMS > 0 {
			timer := time.NewTimer(time.Duration(in.DelayMS) * time.Millisecond)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
		if err := conn.WriteMessage(messageType, data); err != nil {
			return err
		}
	}
}

func handleWebSocketClose(_ context.Context, in wsParams, conn *ghttp.WebSocketConn) error {
	return conn.WriteMessage(ghttp.WebSocketMessageType(websocket.CloseMessage), websocket.FormatCloseMessage(in.Code, "server close"))
}

func boundedStreamInt(raw string, fallback, maximum int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid stream parameter")
	}
	if value > maximum {
		value = maximum
	}
	return value, nil
}
