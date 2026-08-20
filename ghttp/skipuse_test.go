package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// authHeader 模拟鉴权中间件：无 X-Auth 则 401。
// authHeader simulates an auth middleware: rejects requests without X-Auth.
func authHeader(c *Ctx) {
	if c.R.Header.Get("X-Auth") == "" {
		http.Error(c.W, "unauthorized", http.StatusUnauthorized)
		return
	}
	c.Next()
}

func skipPingHandler(ctx context.Context) (map[string]string, error) {
	return map[string]string{"ok": "true"}, nil
}

func TestSkipUseExemptsExactRoute(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(authHeader)
	app.SkipUse(authHeader, http.MethodGet, "/ping")

	app.MustMount(HandleNoInput(Get("/ping"), JSONOutput[map[string]string](), skipPingHandler))
	app.MustMount(HandleNoInput(Get("/secure"), JSONOutput[map[string]string](), skipPingHandler))

	// /ping 应放行（豁免生效）
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/ping status = %d, want 200 (should be exempted)", w.Code)
	}

	// /secure 仍受鉴权保护
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/secure", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("/secure status = %d, want 401", w.Code)
	}

	// 带 header 的 /secure 应通过
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/secure", nil)
	r.Header.Set("X-Auth", "token")
	app.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("/secure with auth status = %d, want 200", w.Code)
	}
}

func TestSkipUseMethodSpecific(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(authHeader)
	// 只豁免 GET /ping，POST /ping 仍受鉴权
	app.SkipUse(authHeader, http.MethodGet, "/ping")

	app.MustMount(HandleNoInput(Get("/ping"), JSONOutput[map[string]string](), skipPingHandler))
	app.MustMount(HandleNoInput(Post("/ping"), JSONOutput[map[string]string](), skipPingHandler))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /ping status = %d, want 200", w.Code)
	}

	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ping", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("POST /ping status = %d, want 401 (only GET exempted)", w.Code)
	}
}

func TestSkipUseGroupScoped(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	public := app.Group("/public")
	public.Use(authHeader)
	// 豁免匹配最终注册路径（含组前缀）
	public.SkipUse(authHeader, http.MethodGet, "/public/ping")
	public.MustMount(HandleNoInput(Get("/ping"), JSONOutput[map[string]string](), skipPingHandler))
	public.MustMount(HandleNoInput(Get("/secure"), JSONOutput[map[string]string](), skipPingHandler))

	app.MustMount(HandleNoInput(Get("/outside"), JSONOutput[map[string]string](), skipPingHandler))

	// 组内豁免生效
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/public/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/public/ping status = %d, want 200", w.Code)
	}

	// 组内其他路由仍受保护
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/public/secure", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("/public/secure status = %d, want 401", w.Code)
	}
}

func TestSkipUseNonMatchingMiddlewareKept(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(authHeader, middlewareSetHeader("X-Extra", "on"))
	// 豁免 authHeader，但保留 middlewareSetHeader
	app.SkipUse(authHeader, "*", "/ping")

	app.MustMount(HandleNoInput(Get("/ping"), JSONOutput[map[string]string](), skipPingHandler))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/ping status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Extra"); got != "on" {
		t.Fatalf("X-Extra header = %q, want on (non-matching middleware kept)", got)
	}
}
