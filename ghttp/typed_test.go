package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type createUser struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type userOut struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// typed 端到端:PathInt64 + Body[JSONBody] 输入,JSON 输出,状态码覆盖为 201。
func TestTypedEndToEnd(t *testing.T) {
	m := New()
	err := Post(m, "/users/{id}",
		PathInt64("id"),
		NoInput[NoQuery](),
		Body[createUser](JSONBody()),
		JSON[userOut]().Status(http.StatusCreated),
		func(ctx context.Context, in RequestInput[int64, NoQuery, createUser]) (userOut, error) {
			return userOut{ID: in.Path, Name: in.Body.Name, Age: in.Body.Age}, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/7", strings.NewReader(`{"name":"Alice","age":30}`))
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got userOut
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := userOut{ID: 7, Name: "Alice", Age: 30}
	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}

// typed 与 middleware 贯通:RequestID 注入的 ID 在 typed handler 内可读。
func TestTypedWithMiddleware(t *testing.T) {
	m := New()
	m.Use(RequestID())
	var seen string
	err := Get(m, "/ping",
		NoInput[NoPath](), NoInput[NoQuery](), NoInput[NoBody](),
		JSON[map[string]string](),
		func(ctx context.Context, in RequestInput[NoPath, NoQuery, NoBody]) (map[string]string, error) {
			seen = RequestIDFromContext(ctx)
			return map[string]string{"ok": "1"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if seen == "" {
		t.Error("typed handler saw empty request ID; middleware/typed not wired")
	}
}

// NoContent 输出:写状态码(默认 204),无响应体。
func TestTypedNoContent(t *testing.T) {
	m := New()
	err := Delete(m, "/users/{id}",
		PathInt64("id"), NoInput[NoQuery](), NoInput[NoBody](),
		NoContent[NoBody](),
		func(ctx context.Context, in RequestInput[int64, NoQuery, NoBody]) (NoBody, error) {
			return NoBody{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/users/9", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body len = %d, want 0", rec.Body.Len())
	}
}

// 缺 Output(out 为 nil)注册期返回 ErrMissingOutput。
func TestTypedMissingOutput(t *testing.T) {
	m := New()
	err := Get[NoPath, NoQuery, NoBody, userOut](m, "/x",
		NoInput[NoPath](), NoInput[NoQuery](), NoInput[NoBody](),
		nil, // 未声明输出
		func(ctx context.Context, in RequestInput[NoPath, NoQuery, NoBody]) (userOut, error) {
			return userOut{}, nil
		})
	if err != ErrMissingOutput {
		t.Errorf("err = %v, want ErrMissingOutput", err)
	}
}

// 输入解码失败(路径 int64 非法)时,业务函数不应执行(阶段 4 前:终端返回 error,
// 未写响应,顶层暂以 500 兜底)。本测试验证"业务函数未被调用"这一契约。
func TestTypedInputDecodeError(t *testing.T) {
	m := New()
	handlerRan := false
	// 用 {id} 但请求 /users/abc(非 int64)。为触达终端解码,需先匹配到该路由:
	// 直接用 PathString 无法制造错误,故用 PathInt64 + 字面非法段。
	err := Get(m, "/users/{id}",
		PathInt64("id"), NoInput[NoQuery](), NoInput[NoBody](),
		JSON[userOut](),
		func(ctx context.Context, in RequestInput[int64, NoQuery, NoBody]) (userOut, error) {
			handlerRan = true
			return userOut{ID: in.Path}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/not-an-int", nil))
	if handlerRan {
		t.Error("business handler ran despite input decode error")
	}
	// 阶段 4 前:终端返回 error 而未写响应,顶层 serve 以 500 兜底。
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (pre-stage-4 fallback)", rec.Code)
	}
}
