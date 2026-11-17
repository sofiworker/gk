package client

import (
	"crypto/tls"
	"net/http/httptrace"
	"time"
)

// 本文件实现 WithTrace 的时序采集：用 net/http/httptrace 记录一次请求的各阶段耗时，
// 结果经 Response.Traces 读取。未开启时零开销（不建 trace、不注入 context）。
// This file implements WithTrace timing collection: net/http/httptrace records each
// phase of a request, readable via Response.Traces. Disabled means zero overhead (no
// trace object, no context injection).

// TraceInfo 是一次请求的分阶段时序。各耗时字段在对应阶段未发生时为零值。
// TraceInfo is a request's phase-by-phase timing. A duration is zero when its phase did
// not occur.
type TraceInfo struct {
	// DNSLookup 是域名解析耗时。
	// DNSLookup is the DNS resolution time.
	DNSLookup time.Duration
	// Connect 是 TCP 建连耗时。
	// Connect is the TCP connect time.
	Connect time.Duration
	// TLSHandshake 是 TLS 握手耗时。
	// TLSHandshake is the TLS handshake time.
	TLSHandshake time.Duration
	// WroteRequest 是从开始发出到请求体写完的耗时。
	// WroteRequest is the time from start until the request body was fully written.
	WroteRequest time.Duration
	// TTFB 是首字节（响应头）到达的耗时，即"服务端处理 + 网络往返"。
	// TTFB is the time to first byte (response headers) — server processing plus a
	// network round trip.
	TTFB time.Duration
	// Total 是整次尝试的墙钟耗时（含读取响应体）。
	// Total is the whole attempt's wall-clock time, including the body read.
	Total time.Duration
	// RemoteAddr 是实际连接的远端地址。
	// RemoteAddr is the peer address actually connected to.
	RemoteAddr string
	// ReusedConn 报告本次是否复用了池中的连接。
	// ReusedConn reports whether a pooled connection was reused.
	ReusedConn bool
}

// Traces 返回本次请求的分阶段时序；未开启 WithTrace 时返回零值。
// Traces returns this request's phase timings, or the zero value when WithTrace is off.
func (r *Response) Traces() TraceInfo { return r.trace }

// traceCollector 按事件顺序记录时间点。它只在请求内单 goroutine 使用（httptrace 的回调
// 由 Transport 在发起请求的 goroutine 上调用），因此无需加锁。
// traceCollector records timestamps in event order. It is used by a single goroutine per
// request (httptrace invokes its callbacks on the goroutine issuing the request), so it
// needs no lock.
type traceCollector struct {
	start time.Time

	dnsStart, dnsDone   time.Time
	connStart, connDone time.Time
	tlsStart, tlsDone   time.Time
	wroteRequest        time.Time
	firstByte           time.Time
	remoteAddr          string
	reusedConn          bool
	hasTLS              bool
}

// newTraceCollector 创建采集器并记下起点。
// newTraceCollector creates a collector and records the start.
func newTraceCollector() *traceCollector {
	return &traceCollector{start: time.Now()}
}

// clientTrace 返回注入 context 的 httptrace 钩子。
// clientTrace returns the httptrace hooks injected into the context.
func (t *traceCollector) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { t.dnsStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { t.dnsDone = time.Now() },
		ConnectStart:         func(_, _ string) { t.connStart = time.Now() },
		ConnectDone:          func(_, _ string, _ error) { t.connDone = time.Now() },
		TLSHandshakeStart:    func() { t.tlsStart = time.Now(); t.hasTLS = true },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { t.tlsDone = time.Now() },
		WroteRequest:         func(httptrace.WroteRequestInfo) { t.wroteRequest = time.Now() },
		GotFirstResponseByte: func() { t.firstByte = time.Now() },
		GotConn: func(info httptrace.GotConnInfo) {
			t.reusedConn = info.Reused
			if info.Conn != nil {
				t.remoteAddr = info.Conn.RemoteAddr().String()
			}
		},
	}
}

// info 把时间点折算成时长。total 由调用方给出（含响应体读取，trace 本身看不到那一段）。
// info converts timestamps into durations. total comes from the caller, since it covers
// the body read that the trace itself cannot observe.
func (t *traceCollector) info(total time.Duration) TraceInfo {
	out := TraceInfo{
		Total:      total,
		RemoteAddr: t.remoteAddr,
		ReusedConn: t.reusedConn,
	}
	if !t.dnsStart.IsZero() && !t.dnsDone.IsZero() {
		out.DNSLookup = t.dnsDone.Sub(t.dnsStart)
	}
	if !t.connStart.IsZero() && !t.connDone.IsZero() {
		out.Connect = t.connDone.Sub(t.connStart)
	}
	if t.hasTLS && !t.tlsStart.IsZero() && !t.tlsDone.IsZero() {
		out.TLSHandshake = t.tlsDone.Sub(t.tlsStart)
	}
	if !t.wroteRequest.IsZero() {
		out.WroteRequest = t.wroteRequest.Sub(t.start)
	}
	if !t.firstByte.IsZero() {
		out.TTFB = t.firstByte.Sub(t.start)
	}
	return out
}
