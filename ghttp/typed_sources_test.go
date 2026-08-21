package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// PathString 端到端 + Put/Patch 薄封装 + NoContent.Status 覆盖。
func TestTypedSourcesAndVerbs(t *testing.T) {
	m := New()

	// Put + PathString + JSON 输出。
	if err := Put(m, "/items/{name}",
		PathString("name"), NoInput[NoQuery](), Body[createUser](JSONBody()),
		JSON[string]().Status(http.StatusAccepted),
		func(ctx context.Context, in RequestInput[string, NoQuery, createUser]) (string, error) {
			return in.Path + ":" + in.Body.Name, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/items/box", strings.NewReader(`{"name":"lid"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("PUT status = %d, want 202", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `"box:lid"` {
		t.Errorf("PUT body = %s, want \"box:lid\"", got)
	}

	// Patch + NoContent.Status 覆盖(自定义 205)。
	if err := Patch(m, "/items/{name}",
		PathString("name"), NoInput[NoQuery](), NoInput[NoBody](),
		NoContent[NoBody]().Status(http.StatusResetContent),
		func(ctx context.Context, in RequestInput[string, NoQuery, NoBody]) (NoBody, error) {
			return NoBody{}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/items/box", nil))
	if rec.Code != http.StatusResetContent {
		t.Errorf("PATCH status = %d, want 205", rec.Code)
	}
}

// 注册期错误:空路径参数名 → ErrInvalidParam;Body 无 codec → ErrMissingCodec。
func TestRegistrationErrors(t *testing.T) {
	m := New()

	err := Get(m, "/x",
		PathString(""), NoInput[NoQuery](), NoInput[NoBody](),
		JSON[string](),
		func(ctx context.Context, in RequestInput[string, NoQuery, NoBody]) (string, error) { return "", nil })
	if err != ErrInvalidParam {
		t.Errorf("empty path name: err = %v, want ErrInvalidParam", err)
	}

	err = Post(m, "/y",
		NoInput[NoPath](), NoInput[NoQuery](), Body[createUser](nil),
		JSON[string](),
		func(ctx context.Context, in RequestInput[NoPath, NoQuery, createUser]) (string, error) {
			return "", nil
		})
	if err != ErrMissingCodec {
		t.Errorf("nil codec: err = %v, want ErrMissingCodec", err)
	}
}

// Logger 默认 sink 不 panic(走 log.Printf 分支)。
func TestLoggerDefaultSink(t *testing.T) {
	m := New()
	m.Use(Logger())
	if err := m.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	doRequest(t, m, http.MethodGet, "/x") // 仅确保不 panic
}

// Logger 在下游未写响应(仅返回 error)时记录 200 占位。
func TestLoggerUnwrittenPlaceholder(t *testing.T) {
	var seen int = -1
	m := New()
	m.Use(LoggerWith(func(_, _ string, status int, _ time.Duration) { seen = status }))
	if err := m.RawHandle(http.MethodGet, "/x", func(ctx context.Context, req *Request, resp *Response) error {
		return context.Canceled // 不写响应,返回 error
	}); err != nil {
		t.Fatal(err)
	}
	doRequest(t, m, http.MethodGet, "/x")
	if seen != 200 {
		t.Errorf("logger placeholder status = %d, want 200", seen)
	}
}

// JSONBody codec 的 contentType 声明正确(供阶段 3/OpenAPI)。
func TestJSONBodyContentType(t *testing.T) {
	if ct := JSONBody().ContentType(); ct != "application/json" {
		t.Errorf("JSONBody ContentType = %q, want application/json", ct)
	}
}

// doRequest 辅助函数：发送请求并返回记录器供断言。
func doRequest(t *testing.T, m http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	m.ServeHTTP(rec, req)
	return rec
}
