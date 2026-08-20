package ghttp

import (
	"context"
	"net/http/httptest"
	"testing"
)

// 新执行模型冒烟测试:静态 + 参数 + 中间件 + 404 四条链。
// smoke test for the new execution model: static, param, middleware, and 404.

type smokePong struct {
	Message string `json:"message"`
}

func TestRewriteSmoke(t *testing.T) {
	s := New()
	s.Use(func(c *Ctx) { c.W.Header().Set("X-MW", "yes"); c.Next() })
	s.MustMount(
		Handle(Get("/ping"), NoInput(), JSONOutput[smokePong](), func(ctx context.Context, _ EmptyInput) (smokePong, error) {
			return smokePong{Message: "pong"}, nil
		}),
		Handle(Get("/users/{id}"), PathString("id"), JSONOutput[smokePong](), func(ctx context.Context, id string) (smokePong, error) {
			return smokePong{Message: "id=" + id}, nil
		}),
	)

	// static
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/ping", nil))
	if rec.Code != 200 || rec.Header().Get("X-MW") != "yes" {
		t.Fatalf("static: code=%d xmw=%q body=%q", rec.Code, rec.Header().Get("X-MW"), rec.Body.String())
	}

	// param
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/users/42", nil))
	if rec.Code != 200 || !contains(rec.Body.String(), "id=42") {
		t.Fatalf("param: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 404
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 {
		t.Fatalf("404: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
