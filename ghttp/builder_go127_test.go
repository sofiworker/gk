//go:build go1.27

package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件只在 Go ≥ 1.27 下编译,验证「非泛型链 + 泛型终结方法」形态。
// This file compiles only under Go >= 1.27 and validates the "non-generic chain +
// generic terminal" shape.

type bldParams struct {
	ID      int    `path:"id"`
	Keyword string `query:"keyword"`
}

type bldBody struct {
	Name string `json:"name"`
}

type bldResp struct {
	Echo string `json:"echo"`
	ID   int    `json:"id"`
}

// TestBuilderTerminals 覆盖四个类型化终结方法的端到端行为:类型全部由 handler 推断,
// 链上不出现类型参数。
func TestBuilderTerminals(t *testing.T) {
	s := New()

	if err := s.Get("/items/{id}").To(JSON[bldResp](),
		func(ctx context.Context, p bldParams) (bldResp, error) {
			return bldResp{Echo: p.Keyword, ID: p.ID}, nil
		}); err != nil {
		t.Fatalf("To: %v", err)
	}
	if err := s.Get("/healthz").ToNone(JSON[bldResp](),
		func(ctx context.Context) (bldResp, error) {
			return bldResp{Echo: "ok"}, nil
		}); err != nil {
		t.Fatalf("ToNone: %v", err)
	}
	if err := s.Post("/items").ToBody(JSONBody[bldBody](), JSON[bldResp]().Status(http.StatusCreated),
		func(ctx context.Context, in bldBody) (bldResp, error) {
			return bldResp{Echo: in.Name}, nil
		}); err != nil {
		t.Fatalf("ToBody: %v", err)
	}
	if err := s.Put("/items/{id}").ToParamsBody(JSONBody[bldBody](), JSON[bldResp](),
		func(ctx context.Context, p bldParams, in bldBody) (bldResp, error) {
			return bldResp{Echo: in.Name, ID: p.ID}, nil
		}); err != nil {
		t.Fatalf("ToParamsBody: %v", err)
	}
	if err := s.Delete("/raw").ToRaw(func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusTeapot)
		_, err := resp.WriteString("raw")
		return err
	}); err != nil {
		t.Fatalf("ToRaw: %v", err)
	}

	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"params", http.MethodGet, "/items/7?keyword=hi", "", http.StatusOK, `{"echo":"hi","id":7}`},
		{"none", http.MethodGet, "/healthz", "", http.StatusOK, `{"echo":"ok","id":0}`},
		{"body", http.MethodPost, "/items", `{"name":"n1"}`, http.StatusCreated, `{"echo":"n1","id":0}`},
		{"params+body", http.MethodPut, "/items/9", `{"name":"n2"}`, http.StatusOK, `{"echo":"n2","id":9}`},
		{"raw", http.MethodDelete, "/raw", "", http.StatusTeapot, "raw"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.body == "" {
				req = httptest.NewRequest(tc.method, tc.target, nil)
			} else {
				req = httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tc.wantBody {
				t.Fatalf("body=%q want %q", got, tc.wantBody)
			}
		})
	}
}

// TestBuilderGroupPrefix 确认分组前缀经链式入口正确生效,且与自由函数入口注册到同一棵树。
func TestBuilderGroupPrefix(t *testing.T) {
	s := New()
	g := s.Group("/api/v1")

	if err := g.Get("/items/{id}").To(JSON[bldResp](),
		func(ctx context.Context, p bldParams) (bldResp, error) {
			return bldResp{Echo: "grp", ID: p.ID}, nil
		}); err != nil {
		t.Fatalf("group To: %v", err)
	}
	// 嵌套分组
	sub := g.Group("/nested")
	if err := sub.Get("/x").ToNone(JSON[bldResp](),
		func(ctx context.Context) (bldResp, error) { return bldResp{Echo: "sub"}, nil }); err != nil {
		t.Fatalf("nested ToNone: %v", err)
	}

	for _, tc := range []struct{ target, want string }{
		{"/api/v1/items/3", `{"echo":"grp","id":3}`},
		{"/api/v1/nested/x", `{"echo":"sub","id":0}`},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", tc.target, rec.Code, rec.Body.String())
		}
		if got := strings.TrimSpace(rec.Body.String()); got != tc.want {
			t.Fatalf("%s: body=%q want %q", tc.target, got, tc.want)
		}
	}

	// 未加前缀的裸路径不应命中
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/3", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unprefixed path should 404, got %d", rec.Code)
	}
}

// TestBuilderMiddlewareOrder 验证折叠顺序为 全局 → 分组 → 路由 → 终端。
func TestBuilderMiddlewareOrder(t *testing.T) {
	var order []string
	mark := func(tag string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				order = append(order, tag)
				return next(ctx, req, resp)
			}
		}
	}

	s := New()
	s.Use(mark("global"))
	g := s.Group("/g", mark("group"))
	if err := g.Get("/x").Use(mark("route1"), mark("route2")).ToNone(JSON[bldResp](),
		func(ctx context.Context) (bldResp, error) {
			order = append(order, "terminal")
			return bldResp{Echo: "ok"}, nil
		}); err != nil {
		t.Fatalf("ToNone: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/g/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	want := []string{"global", "group", "route1", "route2", "terminal"}
	if len(order) != len(want) {
		t.Fatalf("order=%v want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order=%v want %v", order, want)
		}
	}
}

// TestBuilderMiddlewareShortCircuit 确认路由级中间件可短路,终端不被执行。
func TestBuilderMiddlewareShortCircuit(t *testing.T) {
	s := New()
	called := false
	deny := func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			resp.WriteHeader(http.StatusForbidden)
			return nil
		}
	}
	if err := s.Get("/x").Use(deny).ToNone(JSON[bldResp](),
		func(ctx context.Context) (bldResp, error) {
			called = true
			return bldResp{}, nil
		}); err != nil {
		t.Fatalf("ToNone: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403", rec.Code)
	}
	if called {
		t.Fatal("terminal must not run when middleware short-circuits")
	}
}

// TestBuilderErrors 确认链式终结器的错误语义与自由函数入口完全一致。
func TestBuilderErrors(t *testing.T) {
	t.Run("missing output", func(t *testing.T) {
		s := New()
		err := s.Get("/x").To(nil, func(ctx context.Context, p bldParams) (bldResp, error) {
			return bldResp{}, nil
		})
		if !errors.Is(err, ErrMissingOutput) {
			t.Fatalf("got %v want ErrMissingOutput", err)
		}
	})

	t.Run("missing codec nil", func(t *testing.T) {
		s := New()
		err := s.Post("/x").ToBody(nil, JSON[bldResp](),
			func(ctx context.Context, in bldBody) (bldResp, error) { return bldResp{}, nil })
		if !errors.Is(err, ErrMissingCodec) {
			t.Fatalf("got %v want ErrMissingCodec", err)
		}
	})

	// Body[T](nil) 造出非 nil 接口值但内部 decoder 为 nil,必须同样被拒(不得 panic)。
	t.Run("missing codec inside", func(t *testing.T) {
		s := New()
		err := s.Post("/x").ToBody(Body[bldBody](nil), JSON[bldResp](),
			func(ctx context.Context, in bldBody) (bldResp, error) { return bldResp{}, nil })
		if !errors.Is(err, ErrMissingCodec) {
			t.Fatalf("got %v want ErrMissingCodec", err)
		}
	})

	t.Run("bad path", func(t *testing.T) {
		s := New()
		err := s.Get("no-leading-slash").ToNone(JSON[bldResp](),
			func(ctx context.Context) (bldResp, error) { return bldResp{}, nil })
		if !errors.Is(err, ErrEmptyPath) {
			t.Fatalf("got %v want ErrEmptyPath", err)
		}
	})

	// ToRaw 的注册失败分支：重复路由必须原样返回错误,且不得登记 OpenAPI 元数据。
	t.Run("raw duplicate route", func(t *testing.T) {
		s := New()
		fn := func(ctx context.Context, req *Request, resp *Response) error { return nil }
		if err := s.Get("/rawdup").ToRaw(fn); err != nil {
			t.Fatalf("first: %v", err)
		}
		err := s.Get("/rawdup").ToRaw(fn)
		if !errors.Is(err, ErrDuplicateRoute) {
			t.Fatalf("got %v want ErrDuplicateRoute", err)
		}
	})

	t.Run("duplicate route", func(t *testing.T) {
		s := New()
		h := func(ctx context.Context) (bldResp, error) { return bldResp{}, nil }
		if err := s.Get("/dup").ToNone(JSON[bldResp](), h); err != nil {
			t.Fatalf("first: %v", err)
		}
		err := s.Get("/dup").ToNone(JSON[bldResp](), h)
		if !errors.Is(err, ErrDuplicateRoute) {
			t.Fatalf("got %v want ErrDuplicateRoute", err)
		}
	})

	// 小写方法拼写必须在注册期被拒(否则建出永不命中的树)。
	t.Run("lowercase method", func(t *testing.T) {
		s := New()
		err := s.Method("get", "/x").ToNone(JSON[bldResp](),
			func(ctx context.Context) (bldResp, error) { return bldResp{}, nil })
		if !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("got %v want ErrInvalidParam", err)
		}
	})
}

// TestBuilderStrictContentType 确认链式 body 入口同样受严格 Content-Type 校验约束。
func TestBuilderStrictContentType(t *testing.T) {
	s := New()
	if err := s.Post("/items").ToBody(JSONBody[bldBody](), JSON[bldResp](),
		func(ctx context.Context, in bldBody) (bldResp, error) {
			return bldResp{Echo: in.Name}, nil
		}); err != nil {
		t.Fatalf("ToBody: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d want 415 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestBuilderAllVerbs 覆盖 Server 与 Group 上的每个动词起点,确认 method 与前缀都正确
// 传递(HEAD 用 NoContent,因 HEAD 语义上无响应体)。
func TestBuilderAllVerbs(t *testing.T) {
	// verbs 把动词起点与其期望 method 配对;每个 fn 从 router 取得 builder。
	verbs := []struct {
		name   string
		method string
		start  func(r any, path string) *RouteBuilder
	}{
		{"Get", http.MethodGet, func(r any, p string) *RouteBuilder { return verbGet(r, p) }},
		{"Post", http.MethodPost, func(r any, p string) *RouteBuilder { return verbPost(r, p) }},
		{"Put", http.MethodPut, func(r any, p string) *RouteBuilder { return verbPut(r, p) }},
		{"Patch", http.MethodPatch, func(r any, p string) *RouteBuilder { return verbPatch(r, p) }},
		{"Delete", http.MethodDelete, func(r any, p string) *RouteBuilder { return verbDelete(r, p) }},
		{"Head", http.MethodHead, func(r any, p string) *RouteBuilder { return verbHead(r, p) }},
		{"Options", http.MethodOptions, func(r any, p string) *RouteBuilder { return verbOptions(r, p) }},
		{"Method", "WEBHOOK", func(r any, p string) *RouteBuilder { return verbMethod(r, "WEBHOOK", p) }},
	}

	for _, target := range []string{"server", "group"} {
		for _, v := range verbs {
			t.Run(target+"/"+v.name, func(t *testing.T) {
				s := New()
				var start any = s
				wantPath := "/x"
				if target == "group" {
					start = s.Group("/api")
					wantPath = "/api/x"
				}
				// HEAD 无响应体,用 NoContent 声明输出契约。
				if v.method == http.MethodHead {
					if err := v.start(start, "/x").ToNone(NoContent[struct{}](),
						func(ctx context.Context) (struct{}, error) { return struct{}{}, nil }); err != nil {
						t.Fatalf("%s: %v", v.name, err)
					}
					rec := httptest.NewRecorder()
					s.ServeHTTP(rec, httptest.NewRequest(v.method, wantPath, nil))
					if rec.Code != http.StatusNoContent {
						t.Fatalf("status=%d want 204", rec.Code)
					}
					return
				}
				if err := v.start(start, "/x").ToNone(JSON[bldResp](),
					func(ctx context.Context) (bldResp, error) { return bldResp{Echo: v.name}, nil }); err != nil {
					t.Fatalf("%s: %v", v.name, err)
				}
				rec := httptest.NewRecorder()
				s.ServeHTTP(rec, httptest.NewRequest(v.method, wantPath, nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), v.name) {
					t.Fatalf("body=%q want to contain %q", rec.Body.String(), v.name)
				}
			})
		}
	}
}

// 下面这组小 helper 把 *Server 与 *Group 的同名动词起点统一成一个签名,使表驱动测试
// 能同时覆盖两者(Go 不允许对方法做跨类型的直接抽象,故显式分派)。
func verbGet(r any, p string) *RouteBuilder {
	if g, ok := r.(*Group); ok {
		return g.Get(p)
	}
	return r.(*Server).Get(p)
}

func verbPost(r any, p string) *RouteBuilder {
	if g, ok := r.(*Group); ok {
		return g.Post(p)
	}
	return r.(*Server).Post(p)
}

func verbPut(r any, p string) *RouteBuilder {
	if g, ok := r.(*Group); ok {
		return g.Put(p)
	}
	return r.(*Server).Put(p)
}

func verbPatch(r any, p string) *RouteBuilder {
	if g, ok := r.(*Group); ok {
		return g.Patch(p)
	}
	return r.(*Server).Patch(p)
}

func verbDelete(r any, p string) *RouteBuilder {
	if g, ok := r.(*Group); ok {
		return g.Delete(p)
	}
	return r.(*Server).Delete(p)
}

func verbHead(r any, p string) *RouteBuilder {
	if g, ok := r.(*Group); ok {
		return g.Head(p)
	}
	return r.(*Server).Head(p)
}

func verbOptions(r any, p string) *RouteBuilder {
	if g, ok := r.(*Group); ok {
		return g.Options(p)
	}
	return r.(*Server).Options(p)
}

func verbMethod(r any, method, p string) *RouteBuilder {
	if g, ok := r.(*Group); ok {
		return g.Method(method, p)
	}
	return r.(*Server).Method(method, p)
}

// TestBuilderCustomMethod 确认自定义扩展方法可用。
func TestBuilderCustomMethod(t *testing.T) {
	s := New()
	if err := s.Method("WEBHOOK", "/hook").ToNone(JSON[bldResp](),
		func(ctx context.Context) (bldResp, error) { return bldResp{Echo: "hook"}, nil }); err != nil {
		t.Fatalf("Method: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("WEBHOOK", "/hook", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestBuilderInteropWithFreeFunctions 确认两种形态注册到同一棵路由树、可自由混用。
func TestBuilderInteropWithFreeFunctions(t *testing.T) {
	s := New()
	// 链式形态
	if err := s.Get("/chained").ToNone(JSON[bldResp](),
		func(ctx context.Context) (bldResp, error) { return bldResp{Echo: "chained"}, nil }); err != nil {
		t.Fatalf("chained: %v", err)
	}
	// 自由函数形态(Go 1.27 下必须仍然可用)
	if err := GetNone(s, "/free", JSON[bldResp](),
		func(ctx context.Context) (bldResp, error) { return bldResp{Echo: "free"}, nil }); err != nil {
		t.Fatalf("free: %v", err)
	}

	for _, tc := range []struct{ target, want string }{
		{"/chained", "chained"},
		{"/free", "free"},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status=%d", tc.target, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: body=%q want to contain %q", tc.target, rec.Body.String(), tc.want)
		}
	}
}

// TestBuilderOpenAPIRecordsGroupPrefix 是回归测试:builder 必须把【原始】 router 透传给
// register*,否则 noteRoute 的 r.(*Group) 断言失败,spec 里会静默丢掉分组前缀与 tag。
func TestBuilderOpenAPIRecordsGroupPrefix(t *testing.T) {
	s := New(WithOpenAPI(OpenAPIInfo{Title: "t", Version: "1"}))
	g := s.Group("/api/v1/widgets")
	if err := g.Get("/{id}").To(JSON[bldResp](),
		func(ctx context.Context, p bldParams) (bldResp, error) { return bldResp{}, nil }); err != nil {
		t.Fatalf("group To: %v", err)
	}

	spec := s.SpecJSON()
	if spec == nil {
		t.Fatal("SpecJSON returned nil")
	}
	got := string(spec)
	if !strings.Contains(got, `/api/v1/widgets/{id}`) {
		t.Fatalf("spec must contain the group-prefixed path, got: %s", got)
	}
	if !strings.Contains(got, `"widgets"`) {
		t.Fatalf("spec must contain the derived tag %q, got: %s", "widgets", got)
	}
}
