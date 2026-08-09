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

func registerStreams(server *ghttp.Server, metrics *RuntimeMetrics) {
	ghttp.Route[struct{}, struct{}](server).GET("/sse/once").ToSSE(trackedSSE(metrics, handleSSEOnce))
	ghttp.Route[struct{}, struct{}](server).GET("/sse/multi").ToSSE(trackedSSE(metrics, handleSSEMulti))
	ghttp.Route[struct{}, struct{}](server).GET("/sse/heartbeat").ToSSE(trackedSSE(metrics, handleSSEHeartbeat))
	ghttp.Route[struct{}, struct{}](server).GET("/sse/slow").ToSSE(trackedSSE(metrics, handleSSESlow))

	registerWebSocket(server, metrics, "/ws/echo", 0)
	registerWebSocket(server, metrics, "/ws/binary", 0)
	registerWebSocket(server, metrics, "/ws/multi", 0)
	registerWebSocket(server, metrics, "/ws/slow", 0)
	ghttp.Route[struct{}, struct{}](server).GET("/ws/close").ToWebSocket(trackedWebSocket(metrics, handleWebSocketClose))
}

func trackedSSE(metrics *RuntimeMetrics, handler ghttp.SSEHandler) ghttp.SSEHandler {
	return func(ctx context.Context, params ghttp.Params, stream *ghttp.SSEWriter) error {
		closeStream := metrics.OpenSSE()
		defer closeStream()
		return handler(ctx, params, stream)
	}
}

func handleSSEOnce(_ context.Context, _ ghttp.Params, stream *ghttp.SSEWriter) error {
	if err := stream.Retry(1000); err != nil {
		return err
	}
	return stream.WriteEventWithID("message", "1", "hello\nworld")
}

func handleSSEMulti(ctx context.Context, params ghttp.Params, stream *ghttp.SSEWriter) error {
	count, err := boundedStreamInt(params.Query("count"), 1, maxStreamCount)
	if err != nil {
		return err
	}
	for index := 1; index <= count; index++ {
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

func handleSSEHeartbeat(ctx context.Context, params ghttp.Params, stream *ghttp.SSEWriter) error {
	count, err := boundedStreamInt(params.Query("count"), 1, maxStreamCount)
	if err != nil {
		return err
	}
	for index := 1; index <= count; index++ {
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

func handleSSESlow(ctx context.Context, params ghttp.Params, stream *ghttp.SSEWriter) error {
	count, err := boundedStreamInt(params.Query("count"), 1, maxStreamCount)
	if err != nil {
		return err
	}
	intervalMillis, err := boundedStreamInt(params.Query("interval_ms"), 0, int(maxStreamInterval.Milliseconds()))
	if err != nil {
		return err
	}
	interval := time.Duration(intervalMillis) * time.Millisecond
	for index := 1; index <= count; index++ {
		if err := stream.WriteEventWithID("message", strconv.Itoa(index), fmt.Sprintf("slow-%d", index)); err != nil {
			return err
		}
		if index == count {
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
	ghttp.Route[struct{}, struct{}](server).GET(path).ToWebSocket(trackedWebSocket(metrics, handleWebSocketMessages))
}

func trackedWebSocket(metrics *RuntimeMetrics, handler ghttp.WebSocketHandler) ghttp.WebSocketHandler {
	return func(ctx context.Context, params ghttp.Params, conn *ghttp.WebSocketConn) error {
		closeStream := metrics.OpenWebSocket()
		defer closeStream()
		return handler(ctx, params, conn)
	}
}

func handleWebSocketMessages(ctx context.Context, params ghttp.Params, conn *ghttp.WebSocketConn) error {
	delayMillis, err := boundedStreamInt(params.Query("delay_ms"), 0, int(maxStreamInterval.Milliseconds()))
	if err != nil {
		return err
	}
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if len(data) > maxStreamMessageBytes {
			return conn.WriteMessage(ghttp.WebSocketMessageType(websocket.CloseMessage), websocket.FormatCloseMessage(websocket.CloseMessageTooBig, "message too large"))
		}
		if delayMillis > 0 {
			timer := time.NewTimer(time.Duration(delayMillis) * time.Millisecond)
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

func handleWebSocketClose(_ context.Context, params ghttp.Params, conn *ghttp.WebSocketConn) error {
	code, err := boundedStreamInt(params.Query("code"), websocket.CloseNormalClosure, 4999)
	if err != nil || code < 1000 {
		code = websocket.CloseNormalClosure
	}
	return conn.WriteMessage(ghttp.WebSocketMessageType(websocket.CloseMessage), websocket.FormatCloseMessage(code, "server close"))
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
