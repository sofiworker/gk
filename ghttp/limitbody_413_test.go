package ghttp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// ===========================================================================
// LimitBody 413 回归守卫。
//
// 守两个曾经互相掩盖的缺陷:
//  1. Content-Length 分支曾手动 WriteHeader(413) 再返回 ErrInvalidInput,导致客户端
//     收 413 空体、onError 钩子收 400、响应体缺失——三方不一致。
//  2. codec 曾用 fmt.Errorf("%w: %v", ErrInvalidInput, err) 把 *http.MaxBytesError
//     打平成字符串,使 classifyError 里 errors.As(*MaxBytesError)→413 的分支不可达,
//     chunked(无 Content-Length)超限退化成 400。
//
// 因此每个用例都同时断言【客户端 status + 响应体 + hook status + hook 错误语义 +
// 终端是否被调用】:只断言其中一项时,上面任一缺陷都能悄悄溜过。
// LimitBody 413 regression guards.
//
// They cover two defects that used to mask each other: the Content-Length branch
// committing 413 manually while returning a 400-classified error, and the codec
// flattening *http.MaxBytesError so the errors.As 413 branch became dead code.
// Every case therefore asserts the client status, the body, the hook status, the
// hook error semantics, and terminal invocation together — asserting any single one
// alone lets either defect slip through.
// ===========================================================================

// limit413Body 是回归用例的请求体类型。
type limit413Body struct {
	Name string `json:"name"`
}

// limit413Observation 汇总一次请求的客户端侧与观测侧结果。
type limit413Observation struct {
	clientStatus int
	clientBody   string
	hookCalled   bool
	hookStatus   int
	hookErr      error
	terminalHit  bool
}

// limit413Server 组装 LimitBody(maxBytes) + JSON body 终端 + WithErrorHook 的服务,
// 并返回一个执行请求并采集三方结果的闭包。
//
// 观测必须经真实 ServeHTTP 而非直接调中间件:缺陷 A 的本质是"中间件写的响应"与
// "错误链分类的状态码"分裂,只有走完整链路才能同时看到两者。
func limit413Server(t *testing.T, maxBytes int64) func(*http.Request) limit413Observation {
	t.Helper()
	var obs limit413Observation
	s := New(WithErrorHook(func(_ *http.Request, status int, err error) {
		obs.hookCalled, obs.hookStatus, obs.hookErr = true, status, err
	}))
	s.Use(LimitBody(maxBytes))
	if err := PostBody(s, "/echo", JSONBody[limit413Body](), JSON[limit413Body](),
		func(_ context.Context, b limit413Body) (limit413Body, error) {
			obs.terminalHit = true
			return b, nil
		}); err != nil {
		t.Fatalf("PostBody: %v", err)
	}
	return func(req *http.Request) limit413Observation {
		obs = limit413Observation{}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		obs.clientStatus, obs.clientBody = rec.Code, rec.Body.String()
		return obs
	}
}

// limit413JSONPayload 生成一个 JSON 合法但长度为 size 量级的请求体。
func limit413JSONPayload(fill int) string {
	return `{"name":"` + strings.Repeat("x", fill) + `"}`
}

// limit413RequestWithCL 构造带 Content-Length 头的请求。
//
// httptest.NewRequest 只填 req.ContentLength 字段、并不设置 Content-Length 头,而
// LimitBody 的短路分支读的是【头】;不显式设头就根本走不到那条分支,缺陷 A 无法复现。
func limit413RequestWithCL(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return req
}

// limit413ChunkedReader 是不暴露长度的 Body,使 net/http 无从推断 Content-Length。
type limit413ChunkedReader struct{ r io.Reader }

func (c *limit413ChunkedReader) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *limit413ChunkedReader) Close() error               { return nil }

// limit413ChunkedRequest 用 http.ReadRequest 解析真实 chunked 裸报文。
//
// 用裸报文而非手工拼 httptest 请求:这样 ContentLength=-1 与 TransferEncoding 都由
// 标准库解析产生,能证明"没有 Content-Length 头"这个前提是真实报文的自然结果,
// 而不是测试硬塞的字段值。
func limit413ChunkedRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	raw := "POST /echo HTTP/1.1\r\nHost: example.test\r\n" +
		"Content-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n" +
		strconv.FormatInt(int64(len(body)), 16) + "\r\n" + body + "\r\n0\r\n\r\n"
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("http.ReadRequest: %v", err)
	}
	if req.Header.Get("Content-Length") != "" {
		t.Fatalf("precondition failed: chunked request carries Content-Length %q",
			req.Header.Get("Content-Length"))
	}
	if req.ContentLength != -1 {
		t.Fatalf("precondition failed: ContentLength = %d, want -1 (unknown)", req.ContentLength)
	}
	return req
}

// limit413ErrorBody 解出统一错误体的 code 与 message。
func limit413ErrorBody(t *testing.T, body string) (code, message string) {
	t.Helper()
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("error body is not the unified JSON shape: %v (body=%q)", err, body)
	}
	return payload.Error.Code, payload.Error.Message
}

// TestLimitBody_ContentLengthOverLimitYields413 守缺陷 A:有 Content-Length 且超限时,
// 客户端 413 + 统一 JSON 错误体、hook 同样看到 413、终端未被调用。
func TestLimitBody_ContentLengthOverLimitYields413(t *testing.T) {
	do := limit413Server(t, 16)
	body := limit413JSONPayload(200)
	obs := do(limit413RequestWithCL(body))

	if obs.clientStatus != http.StatusRequestEntityTooLarge {
		t.Errorf("client status = %d, want 413", obs.clientStatus)
	}
	// 缺陷 A 的直接症状是 413 空体(手动 WriteHeader 绕过了渲染器)。
	if obs.clientBody == "" {
		t.Fatal("client body is empty: the manual WriteHeader bypassed the unified error renderer")
	}
	code, message := limit413ErrorBody(t, obs.clientBody)
	if code != "request_entity_too_large" {
		t.Errorf("error code = %q, want request_entity_too_large", code)
	}
	if message == "" {
		t.Error("error message is empty")
	}
	if !obs.hookCalled {
		t.Fatal("WithErrorHook was never invoked")
	}
	if obs.hookStatus != http.StatusRequestEntityTooLarge {
		t.Errorf("hook status = %d, want 413 (must agree with the client status)", obs.hookStatus)
	}
	if obs.hookStatus != obs.clientStatus {
		t.Errorf("three-way inconsistency: client=%d hook=%d", obs.clientStatus, obs.hookStatus)
	}
	if obs.terminalHit {
		t.Error("terminal handler ran despite the oversized body")
	}
}

// TestLimitBody_ContentLengthHookSeesEntityTooLarge 守 hook 侧错误语义:
// 观测方必须能 errors.Is 出 ErrRequestEntityTooLarge,而不是被当成 ErrInvalidInput。
func TestLimitBody_ContentLengthHookSeesEntityTooLarge(t *testing.T) {
	do := limit413Server(t, 16)
	obs := do(limit413RequestWithCL(limit413JSONPayload(200)))

	if obs.hookErr == nil {
		t.Fatal("hook error is nil")
	}
	if !errors.Is(obs.hookErr, ErrRequestEntityTooLarge) {
		t.Errorf("errors.Is(hookErr, ErrRequestEntityTooLarge) = false, err = %v", obs.hookErr)
	}
	// 不得同时命中 400 哨兵:否则 classifyError 的 switch 顺序一变状态码就会漂回 400。
	if errors.Is(obs.hookErr, ErrInvalidInput) {
		t.Errorf("hookErr must not also match ErrInvalidInput (it classifies to 400), err = %v", obs.hookErr)
	}
}

// TestLimitBody_ChunkedOverLimitYields413 守缺陷 B:无 Content-Length(chunked)时
// 超限体经 MaxBytesReader 在解码期失败,必须仍是 413 而非退化的 400。
func TestLimitBody_ChunkedOverLimitYields413(t *testing.T) {
	do := limit413Server(t, 16)
	obs := do(limit413ChunkedRequest(t, limit413JSONPayload(200)))

	if obs.clientStatus != http.StatusRequestEntityTooLarge {
		t.Errorf("client status = %d, want 413 (400 means *http.MaxBytesError was flattened by the codec)",
			obs.clientStatus)
	}
	code, _ := limit413ErrorBody(t, obs.clientBody)
	if code != "request_entity_too_large" {
		t.Errorf("error code = %q, want request_entity_too_large", code)
	}
	if obs.hookStatus != http.StatusRequestEntityTooLarge {
		t.Errorf("hook status = %d, want 413", obs.hookStatus)
	}
	if !errors.Is(obs.hookErr, ErrRequestEntityTooLarge) {
		t.Errorf("errors.Is(hookErr, ErrRequestEntityTooLarge) = false, err = %v", obs.hookErr)
	}
	// 底层 *http.MaxBytesError 必须仍在链上:它是 classifyError 兜底分支的输入,
	// 也是调用方读取 Limit 的唯一途径。
	var maxBytes *http.MaxBytesError
	if !errors.As(obs.hookErr, &maxBytes) {
		t.Errorf("errors.As(hookErr, **http.MaxBytesError) = false: the cause chain was severed, err = %v",
			obs.hookErr)
	} else if maxBytes.Limit != 16 {
		t.Errorf("MaxBytesError.Limit = %d, want 16", maxBytes.Limit)
	}
	if obs.terminalHit {
		t.Error("terminal handler ran despite the oversized chunked body")
	}
}

// TestLimitBody_UnderLimitUnaffected 守回归面:未超限的请求必须完全不受影响,
// 两种传输方式(带 CL 与 chunked)都要跑通,防止修复把正常请求也拦掉。
func TestLimitBody_UnderLimitUnaffected(t *testing.T) {
	const limit = 1 << 12
	body := `{"name":"alice"}`

	tests := []struct {
		name string
		make func(*testing.T) *http.Request
	}{
		{"with Content-Length", func(*testing.T) *http.Request { return limit413RequestWithCL(body) }},
		{"chunked", func(t *testing.T) *http.Request { return limit413ChunkedRequest(t, body) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			do := limit413Server(t, limit)
			obs := do(tt.make(t))

			if obs.clientStatus != http.StatusOK {
				t.Errorf("client status = %d, want 200 (body=%q)", obs.clientStatus, obs.clientBody)
			}
			if !obs.terminalHit {
				t.Error("terminal handler was not reached for an in-limit body")
			}
			if obs.hookCalled {
				t.Errorf("error hook fired for a valid request: status=%d err=%v", obs.hookStatus, obs.hookErr)
			}
			var echoed limit413Body
			if err := json.Unmarshal([]byte(obs.clientBody), &echoed); err != nil {
				t.Fatalf("response is not valid JSON: %v (body=%q)", err, obs.clientBody)
			}
			if echoed.Name != "alice" {
				t.Errorf("echoed Name = %q, want alice", echoed.Name)
			}
		})
	}
}

// TestLimitBody_SyntaxErrorStill400 守缺陷 B 修复的精确性:普通 JSON 语法错误必须
// 仍是 400。缺陷 B 若被"把所有解码错误都当 413"的粗暴改法修掉,本用例即失败。
func TestLimitBody_SyntaxErrorStill400(t *testing.T) {
	// 注意:不测"尾部多余字符"(如 `{"name":"a"} }}`)。json.Decoder 是流式的,
	// Decode 只读第一个完整 JSON 值就返回,尾部残余不算错误——那是标准库既有语义,
	// 不是本次修复的守卫对象。
	tests := []struct {
		name string
		body string
	}{
		{"truncated object", `{"name":`},
		{"unterminated string", `{"name":"abc`},
		{"unquoted key", `{name:1}`},
		{"type mismatch", `{"name":123}`},
		{"not json at all", `<xml/>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			do := limit413Server(t, 1<<20) // 上限远大于请求体,绝不该触发 413
			req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			obs := do(req)

			if obs.clientStatus != http.StatusBadRequest {
				t.Errorf("client status = %d, want 400", obs.clientStatus)
			}
			code, _ := limit413ErrorBody(t, obs.clientBody)
			if code != "invalid_input" {
				t.Errorf("error code = %q, want invalid_input", code)
			}
			if obs.hookStatus != http.StatusBadRequest {
				t.Errorf("hook status = %d, want 400", obs.hookStatus)
			}
			// 语义未被破坏:普通解码错误仍须 errors.Is 到 ErrInvalidInput。
			if !errors.Is(obs.hookErr, ErrInvalidInput) {
				t.Errorf("errors.Is(hookErr, ErrInvalidInput) = false, err = %v", obs.hookErr)
			}
			if errors.Is(obs.hookErr, ErrRequestEntityTooLarge) {
				t.Errorf("a plain syntax error must not match ErrRequestEntityTooLarge, err = %v", obs.hookErr)
			}
		})
	}
}

// TestLimitBody_DecodeErrorPreservesCause 守 %w 包装:解码错误必须保留底层原因,
// 使调用方能 errors.As 出具体类型(*json.SyntaxError / *http.MaxBytesError)。
// 这是缺陷 B 的根因守卫——一旦有人把 %w 改回 %v,链断裂即在此暴露。
func TestLimitBody_DecodeErrorPreservesCause(t *testing.T) {
	t.Run("json syntax error", func(t *testing.T) {
		req := &Request{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))}
		var v limit413Body
		err := jsonCodec{}.Decode(req, &v)
		if err == nil {
			t.Fatal("expected a decode error")
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("errors.Is(err, ErrInvalidInput) = false, err = %v", err)
		}
		var syntax *json.SyntaxError
		if !errors.As(err, &syntax) && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("cause chain lost: want *json.SyntaxError or io.ErrUnexpectedEOF, err = %v (%T)", err, err)
		}
	})

	t.Run("max bytes error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		inner := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(limit413JSONPayload(200)))
		inner.Body = http.MaxBytesReader(rec, inner.Body, 16)
		err := jsonCodec{}.Decode(&Request{Request: inner}, &limit413Body{})
		if err == nil {
			t.Fatal("expected a decode error")
		}
		var maxBytes *http.MaxBytesError
		if !errors.As(err, &maxBytes) {
			t.Fatalf("errors.As(err, **http.MaxBytesError) = false, err = %v (%T)", err, err)
		}
		if !errors.Is(err, ErrRequestEntityTooLarge) {
			t.Errorf("errors.Is(err, ErrRequestEntityTooLarge) = false, err = %v", err)
		}
		if status, code := classifyError(err); status != http.StatusRequestEntityTooLarge {
			t.Errorf("classifyError = (%d,%q), want 413", status, code)
		}
	})

	t.Run("xml max bytes error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		inner := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("<item><name>"+strings.Repeat("x", 200)+"</name></item>"))
		inner.Body = http.MaxBytesReader(rec, inner.Body, 16)
		var v struct {
			Name string `xml:"name"`
		}
		err := xmlCodec{}.Decode(&Request{Request: inner}, &v)
		if err == nil {
			t.Fatal("expected a decode error")
		}
		if !errors.Is(err, ErrRequestEntityTooLarge) {
			t.Errorf("errors.Is(err, ErrRequestEntityTooLarge) = false, err = %v", err)
		}
		if status, _ := classifyError(err); status != http.StatusRequestEntityTooLarge {
			t.Errorf("classifyError status = %d, want 413", status)
		}
	})
}

// TestLimitBody_DecodeErrorClassification 表驱动覆盖 decodeError 的分类矩阵:
// 只有 *http.MaxBytesError(含被包装的)映射 413,其余一律 400。
func TestLimitBody_DecodeErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		cause      error
		wantStatus int
		wantCode   string
	}{
		{"max bytes", &http.MaxBytesError{Limit: 16}, http.StatusRequestEntityTooLarge, "request_entity_too_large"},
		{"wrapped max bytes", errors.Join(errors.New("read"), &http.MaxBytesError{Limit: 8}),
			http.StatusRequestEntityTooLarge, "request_entity_too_large"},
		{"syntax", &json.SyntaxError{Offset: 3}, http.StatusBadRequest, "invalid_input"},
		{"unexpected eof", io.ErrUnexpectedEOF, http.StatusBadRequest, "invalid_input"},
		{"plain", errors.New("boom"), http.StatusBadRequest, "invalid_input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := decodeError(tt.cause)
			status, code := classifyError(err)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Errorf("classifyError(decodeError(%v)) = (%d,%q), want (%d,%q)",
					tt.cause, status, code, tt.wantStatus, tt.wantCode)
			}
			// 无论走哪条分支,底层原因都必须留在链上。
			if !errors.Is(err, tt.cause) {
				t.Errorf("errors.Is(err, cause) = false: decodeError dropped the cause, err = %v", err)
			}
		})
	}
}
