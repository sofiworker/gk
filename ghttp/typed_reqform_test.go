package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 扩展 handler 形态 HandleReq:业务函数在 typed 输入之外拿到 *Request,可读原始头,
// 输出仍走 OutputSpec。验证 typed 契约与原始请求并存。
func TestHandleReqExtendedForm(t *testing.T) {
	m := New()
	err := HandleReq(m, http.MethodPost, "/users/{id}",
		PathInt64("id"),
		NoInput[NoQuery](),
		Body[createUser](JSONBody()),
		JSON[userOut]().Status(http.StatusCreated),
		func(ctx context.Context, req *Request, in RequestInput[int64, NoQuery, createUser]) (userOut, error) {
			// 扩展形态的关键能力:直接访问原始 *Request(此处读一个自定义头)。
			trace := req.Header.Get("X-Trace")
			return userOut{ID: in.Path, Name: in.Body.Name + ":" + trace, Age: in.Body.Age}, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/5", strings.NewReader(`{"name":"Bob","age":20}`))
	req.Header.Set("X-Trace", "t7")
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"Bob:t7"`) {
		t.Errorf("body = %s, want name to embed raw header trace \"Bob:t7\"", rec.Body.String())
	}
}

// 扩展形态同样贯通中间件:RequestID 注入的 ID 经 context 在扩展 handler 内可读,
// 且扩展形态能同时从 *Request 与 ctx 取值。
func TestHandleReqWithMiddleware(t *testing.T) {
	m := New()
	m.Use(RequestID())
	var fromCtx, fromReqPath string
	err := HandleReq(m, http.MethodGet, "/ping/{tag}",
		PathString("tag"), NoInput[NoQuery](), NoInput[NoBody](),
		JSON[string](),
		func(ctx context.Context, req *Request, in RequestInput[string, NoQuery, NoBody]) (string, error) {
			fromCtx = RequestIDFromContext(ctx)
			fromReqPath = req.URL.Path
			return in.Path, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping/xyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fromCtx == "" {
		t.Error("extended handler saw empty request ID from context")
	}
	if fromReqPath != "/ping/xyz" {
		t.Errorf("extended handler *Request.URL.Path = %q, want /ping/xyz", fromReqPath)
	}
}

// HandleReq 缺 Output 同样返回 ErrMissingOutput(与 Handle 一致)。
func TestHandleReqMissingOutput(t *testing.T) {
	m := New()
	err := HandleReq[NoPath, NoQuery, NoBody, userOut](m, http.MethodGet, "/x",
		NoInput[NoPath](), NoInput[NoQuery](), NoInput[NoBody](),
		nil,
		func(ctx context.Context, req *Request, in RequestInput[NoPath, NoQuery, NoBody]) (userOut, error) {
			return userOut{}, nil
		})
	if err != ErrMissingOutput {
		t.Errorf("err = %v, want ErrMissingOutput", err)
	}
}

// HandleReq 输入解码失败时业务函数不执行(与 Handle 一致)。
func TestHandleReqInputDecodeError(t *testing.T) {
	m := New()
	ran := false
	err := HandleReq(m, http.MethodGet, "/users/{id}",
		PathInt64("id"), NoInput[NoQuery](), NoInput[NoBody](),
		JSON[int64](),
		func(ctx context.Context, req *Request, in RequestInput[int64, NoQuery, NoBody]) (int64, error) {
			ran = true
			return in.Path, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/nan", nil))
	if ran {
		t.Error("extended business handler ran despite input decode error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (pre-stage-4 fallback)", rec.Code)
	}
}
