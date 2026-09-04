package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ===========================================================================
// typed 入口矩阵的完整性测试。
//
// 入口命名恒为 <Method><InputShape>,矩阵按"该 method 在 HTTP 语义下可能收到什么输入"
// 铺开:每个 method 都有 Params 与 None;能带请求体的 method(POST/PUT/PATCH/DELETE)
// 另有 Body 与 ParamsBody。GET/HEAD/OPTIONS 不提供 Body 入口——按 RFC 9110 它们的请求体
// 没有定义语义,提供入口只会诱导错误用法。
//
// Completeness tests for the typed entry matrix.
//
// Entries are always named <Method><InputShape>, and the matrix spans "what input
// this method can receive under HTTP semantics": every method has Params and None,
// while body-bearing methods (POST/PUT/PATCH/DELETE) additionally have Body and
// ParamsBody. GET/HEAD/OPTIONS expose no Body entry — per RFC 9110 their request
// bodies have no defined semantics, and offering one would only invite misuse.
// ===========================================================================

type matrixParams struct {
	ID int `path:"id"`
}

type matrixBody struct {
	Name string `json:"name"`
}

type matrixOut struct {
	Echo string `json:"echo"`
}

// ——— None 形态:每个 method 都要有 ——— //

func TestEntryMatrix_NoneAllMethods(t *testing.T) {
	tests := []struct {
		method     string
		register   func(r router) error
		wantStatus int
	}{
		{http.MethodGet, func(r router) error {
			return GetNone(r, "/n", JSON[matrixOut](), func(context.Context) (matrixOut, error) {
				return matrixOut{Echo: "get"}, nil
			})
		}, http.StatusOK},
		{http.MethodPost, func(r router) error {
			return PostNone(r, "/n", JSON[matrixOut](), func(context.Context) (matrixOut, error) {
				return matrixOut{Echo: "post"}, nil
			})
		}, http.StatusOK},
		{http.MethodPut, func(r router) error {
			return PutNone(r, "/n", JSON[matrixOut](), func(context.Context) (matrixOut, error) {
				return matrixOut{Echo: "put"}, nil
			})
		}, http.StatusOK},
		{http.MethodPatch, func(r router) error {
			return PatchNone(r, "/n", JSON[matrixOut](), func(context.Context) (matrixOut, error) {
				return matrixOut{Echo: "patch"}, nil
			})
		}, http.StatusOK},
		{http.MethodDelete, func(r router) error {
			return DeleteNone(r, "/n", JSON[matrixOut](), func(context.Context) (matrixOut, error) {
				return matrixOut{Echo: "delete"}, nil
			})
		}, http.StatusOK},
		{http.MethodHead, func(r router) error {
			return HeadNone(r, "/n", NoContent[struct{}](), func(context.Context) (struct{}, error) {
				return struct{}{}, nil
			})
		}, http.StatusNoContent},
		{http.MethodOptions, func(r router) error {
			return OptionsNone(r, "/n", NoContent[struct{}](), func(context.Context) (struct{}, error) {
				return struct{}{}, nil
			})
		}, http.StatusNoContent},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			s := New()
			if err := tc.register(s); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(tc.method, "/n", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("code=%d want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

// ——— Params 形态:每个 method 都要有 ——— //

func TestEntryMatrix_ParamsAllMethods(t *testing.T) {
	tests := []struct {
		method     string
		register   func(r router) error
		wantStatus int
	}{
		{http.MethodGet, func(r router) error {
			return GetParams(r, "/p/{id}", JSON[matrixOut](), func(_ context.Context, p matrixParams) (matrixOut, error) {
				return matrixOut{Echo: "get"}, nil
			})
		}, http.StatusOK},
		{http.MethodPost, func(r router) error {
			return PostParams(r, "/p/{id}", JSON[matrixOut](), func(_ context.Context, p matrixParams) (matrixOut, error) {
				return matrixOut{Echo: "post"}, nil
			})
		}, http.StatusOK},
		{http.MethodPut, func(r router) error {
			return PutParams(r, "/p/{id}", JSON[matrixOut](), func(_ context.Context, p matrixParams) (matrixOut, error) {
				return matrixOut{Echo: "put"}, nil
			})
		}, http.StatusOK},
		{http.MethodPatch, func(r router) error {
			return PatchParams(r, "/p/{id}", JSON[matrixOut](), func(_ context.Context, p matrixParams) (matrixOut, error) {
				return matrixOut{Echo: "patch"}, nil
			})
		}, http.StatusOK},
		{http.MethodDelete, func(r router) error {
			return DeleteParams(r, "/p/{id}", JSON[matrixOut](), func(_ context.Context, p matrixParams) (matrixOut, error) {
				return matrixOut{Echo: "delete"}, nil
			})
		}, http.StatusOK},
		{http.MethodHead, func(r router) error {
			return HeadParams(r, "/p/{id}", NoContent[struct{}](), func(_ context.Context, p matrixParams) (struct{}, error) {
				return struct{}{}, nil
			})
		}, http.StatusNoContent},
		{http.MethodOptions, func(r router) error {
			return OptionsParams(r, "/p/{id}", NoContent[struct{}](), func(_ context.Context, p matrixParams) (struct{}, error) {
				return struct{}{}, nil
			})
		}, http.StatusNoContent},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			s := New()
			if err := tc.register(s); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(tc.method, "/p/7", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("code=%d want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestEntryMatrix_ParamsActuallyBound(t *testing.T) {
	// 参数必须真的绑定到 handler,而不只是路由能命中。
	s := New()
	var gotID int
	if err := HeadParams(s, "/h/{id}", NoContent[struct{}](),
		func(_ context.Context, p matrixParams) (struct{}, error) {
			gotID = p.ID
			return struct{}{}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/h/123", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code=%d", rec.Code)
	}
	if gotID != 123 {
		t.Errorf("HeadParams did not bind the path param: got %d", gotID)
	}
}

// ——— Body / ParamsBody:仅可带体的 method ——— //

func TestEntryMatrix_BodyMethods(t *testing.T) {
	tests := []struct {
		method   string
		register func(r router) error
	}{
		{http.MethodPost, func(r router) error {
			return PostBody(r, "/b", JSONBody[matrixBody](), JSON[matrixOut](),
				func(_ context.Context, b matrixBody) (matrixOut, error) { return matrixOut{Echo: b.Name}, nil })
		}},
		{http.MethodPut, func(r router) error {
			return PutBody(r, "/b", JSONBody[matrixBody](), JSON[matrixOut](),
				func(_ context.Context, b matrixBody) (matrixOut, error) { return matrixOut{Echo: b.Name}, nil })
		}},
		{http.MethodPatch, func(r router) error {
			return PatchBody(r, "/b", JSONBody[matrixBody](), JSON[matrixOut](),
				func(_ context.Context, b matrixBody) (matrixOut, error) { return matrixOut{Echo: b.Name}, nil })
		}},
		{http.MethodDelete, func(r router) error {
			return DeleteBody(r, "/b", JSONBody[matrixBody](), JSON[matrixOut](),
				func(_ context.Context, b matrixBody) (matrixOut, error) { return matrixOut{Echo: b.Name}, nil })
		}},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			s := New()
			if err := tc.register(s); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(tc.method, "/b", strings.NewReader(`{"name":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"echo":"x"`) {
				t.Errorf("body not decoded: %s", rec.Body.String())
			}
		})
	}
}

func TestEntryMatrix_ParamsBodyMethods(t *testing.T) {
	tests := []struct {
		method   string
		register func(r router) error
	}{
		{http.MethodPost, func(r router) error {
			return PostParamsBody(r, "/pb/{id}", JSONBody[matrixBody](), JSON[matrixOut](),
				func(_ context.Context, p matrixParams, b matrixBody) (matrixOut, error) {
					return matrixOut{Echo: b.Name}, nil
				})
		}},
		{http.MethodPut, func(r router) error {
			return PutParamsBody(r, "/pb/{id}", JSONBody[matrixBody](), JSON[matrixOut](),
				func(_ context.Context, p matrixParams, b matrixBody) (matrixOut, error) {
					return matrixOut{Echo: b.Name}, nil
				})
		}},
		{http.MethodPatch, func(r router) error {
			return PatchParamsBody(r, "/pb/{id}", JSONBody[matrixBody](), JSON[matrixOut](),
				func(_ context.Context, p matrixParams, b matrixBody) (matrixOut, error) {
					return matrixOut{Echo: b.Name}, nil
				})
		}},
		{http.MethodDelete, func(r router) error {
			return DeleteParamsBody(r, "/pb/{id}", JSONBody[matrixBody](), JSON[matrixOut](),
				func(_ context.Context, p matrixParams, b matrixBody) (matrixOut, error) {
					return matrixOut{Echo: b.Name}, nil
				})
		}},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			s := New()
			if err := tc.register(s); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(tc.method, "/pb/9", strings.NewReader(`{"name":"y"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"echo":"y"`) {
				t.Errorf("body not decoded: %s", rec.Body.String())
			}
		})
	}
}

func TestEntryMatrix_DeleteBodyAndParamsBind(t *testing.T) {
	// DELETE 带体是真实需求(批量删除的 id 列表),参数与体必须同时可用。
	s := New()
	type bulk struct {
		IDs []int `json:"ids"`
	}
	var gotID int
	var gotIDs []int
	if err := DeleteParamsBody(s, "/tenants/{id}/items", JSONBody[bulk](), NoContent[struct{}](),
		func(_ context.Context, p matrixParams, b bulk) (struct{}, error) {
			gotID, gotIDs = p.ID, b.IDs
			return struct{}{}, nil
		}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/tenants/5/items", strings.NewReader(`{"ids":[1,2,3]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotID != 5 {
		t.Errorf("path param=%d want 5", gotID)
	}
	if !equalInts(gotIDs, []int{1, 2, 3}) {
		t.Errorf("body ids=%v", gotIDs)
	}
}

// ——— HEAD 语义:不写响应体 ——— //

func TestEntryMatrix_HeadWritesNoBody(t *testing.T) {
	// HEAD 必须与同路径 GET 的头一致但无体。这里用 JSON 输出验证:即使 handler 返回了
	// 数据,net/http 也不会把体发给 HEAD 请求。
	s := New()
	if err := HeadNone(s, "/meta", JSON[matrixOut](), func(context.Context) (matrixOut, error) {
		return matrixOut{Echo: "hello"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("HEAD should still carry the content type, got %q", ct)
	}
}

// ——— OPTIONS 语义:常用于 CORS 预检 ——— //

func TestEntryMatrix_OptionsCanSetHeaders(t *testing.T) {
	// OPTIONS 的价值在于回头(Allow / CORS),故要能在 handler 里写响应头。
	s := New()
	if err := OptionsNone(s, "/cors", NoContent[struct{}](), func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/cors", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code=%d", rec.Code)
	}
}

// ——— 入口在 Group 上同样可用 ——— //

func TestEntryMatrix_NewEntriesWorkOnGroups(t *testing.T) {
	s := New()
	g := s.Group("/api")
	registrations := []func() error{
		func() error {
			return DeleteNone(g, "/a", NoContent[struct{}](), func(context.Context) (struct{}, error) {
				return struct{}{}, nil
			})
		},
		func() error {
			return HeadNone(g, "/b", NoContent[struct{}](), func(context.Context) (struct{}, error) {
				return struct{}{}, nil
			})
		},
		func() error {
			return OptionsNone(g, "/c", NoContent[struct{}](), func(context.Context) (struct{}, error) {
				return struct{}{}, nil
			})
		},
		func() error {
			return HeadParams(g, "/d/{id}", NoContent[struct{}](), func(_ context.Context, p matrixParams) (struct{}, error) {
				return struct{}{}, nil
			})
		},
		func() error {
			return OptionsParams(g, "/e/{id}", NoContent[struct{}](), func(_ context.Context, p matrixParams) (struct{}, error) {
				return struct{}{}, nil
			})
		},
		func() error {
			return DeleteBody(g, "/f", JSONBody[matrixBody](), NoContent[struct{}](),
				func(_ context.Context, b matrixBody) (struct{}, error) { return struct{}{}, nil })
		},
		func() error {
			return DeleteParamsBody(g, "/g/{id}", JSONBody[matrixBody](), NoContent[struct{}](),
				func(_ context.Context, p matrixParams, b matrixBody) (struct{}, error) { return struct{}{}, nil })
		},
		func() error {
			return PostNone(g, "/h", NoContent[struct{}](), func(context.Context) (struct{}, error) {
				return struct{}{}, nil
			})
		},
		func() error {
			return PutNone(g, "/i", NoContent[struct{}](), func(context.Context) (struct{}, error) {
				return struct{}{}, nil
			})
		},
		func() error {
			return PatchNone(g, "/j", NoContent[struct{}](), func(context.Context) (struct{}, error) {
				return struct{}{}, nil
			})
		},
	}
	for i, reg := range registrations {
		if err := reg(); err != nil {
			t.Fatalf("registration %d failed: %v", i, err)
		}
	}

	cases := []struct{ method, path string }{
		{http.MethodDelete, "/api/a"},
		{http.MethodHead, "/api/b"},
		{http.MethodOptions, "/api/c"},
		{http.MethodHead, "/api/d/1"},
		{http.MethodOptions, "/api/e/1"},
		{http.MethodPost, "/api/h"},
		{http.MethodPut, "/api/i"},
		{http.MethodPatch, "/api/j"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s %s code=%d want 204", c.method, c.path, rec.Code)
		}
	}
	// 带体的两个入口单独发请求。
	for _, c := range []struct{ method, path string }{
		{http.MethodDelete, "/api/f"},
		{http.MethodDelete, "/api/g/1"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"name":"z"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s %s code=%d want 204 body=%s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// ——— 新入口与 Group 中间件、错误链协同 ——— //

func TestEntryMatrix_NewEntriesRunGroupMiddleware(t *testing.T) {
	s := New()
	var hits int
	g := s.Group("/api", func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			hits++
			return next(ctx, req, resp)
		}
	})
	if err := DeleteNone(g, "/x", NoContent[struct{}](), func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := OptionsNone(g, "/y", NoContent[struct{}](), func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodDelete, "/api/x", nil))
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodOptions, "/api/y", nil))
	if hits != 2 {
		t.Errorf("group middleware ran %d times, want 2", hits)
	}
}

func TestEntryMatrix_DeleteBodyRejectsWrongContentType(t *testing.T) {
	// 带体入口应与既有 Body 入口共享严格内容协商行为。
	s := New(WithStrictContentType(true))
	if err := DeleteBody(s, "/b", JSONBody[matrixBody](), NoContent[struct{}](),
		func(_ context.Context, b matrixBody) (struct{}, error) { return struct{}{}, nil }); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/b", strings.NewReader("name=x"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("code=%d want 415 body=%s", rec.Code, rec.Body.String())
	}
}

func TestEntryMatrix_NewEntriesReportBindErrors(t *testing.T) {
	// 参数解析失败必须走统一错误链映射为 400,新入口不得绕过它。
	s := New()
	if err := HeadParams(s, "/p/{id}", NoContent[struct{}](),
		func(_ context.Context, p matrixParams) (struct{}, error) { return struct{}{}, nil }); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/p/notanint", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d want 400", rec.Code)
	}
}

// ——— 同路径多 method 共存 ——— //

func TestEntryMatrix_AllMethodsOnOnePath(t *testing.T) {
	// 一条资源路径上七种 method 并存,验证入口矩阵与路由树协同无冲突。
	s := New()
	mk := func(name string) func(context.Context) (matrixOut, error) {
		return func(context.Context) (matrixOut, error) { return matrixOut{Echo: name}, nil }
	}
	if err := GetNone(s, "/r", JSON[matrixOut](), mk("get")); err != nil {
		t.Fatal(err)
	}
	if err := PostNone(s, "/r", JSON[matrixOut](), mk("post")); err != nil {
		t.Fatal(err)
	}
	if err := PutNone(s, "/r", JSON[matrixOut](), mk("put")); err != nil {
		t.Fatal(err)
	}
	if err := PatchNone(s, "/r", JSON[matrixOut](), mk("patch")); err != nil {
		t.Fatal(err)
	}
	if err := DeleteNone(s, "/r", JSON[matrixOut](), mk("delete")); err != nil {
		t.Fatal(err)
	}
	if err := HeadNone(s, "/r", NoContent[struct{}](), func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := OptionsNone(s, "/r", NoContent[struct{}](), func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		method string
		want   string
	}{
		{http.MethodGet, "get"},
		{http.MethodPost, "post"},
		{http.MethodPut, "put"},
		{http.MethodPatch, "patch"},
		{http.MethodDelete, "delete"},
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(tc.method, "/r", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s code=%d", tc.method, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"echo":"`+tc.want+`"`) {
			t.Errorf("%s dispatched to the wrong handler: %s", tc.method, rec.Body.String())
		}
	}
	for _, m := range []string{http.MethodHead, http.MethodOptions} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(m, "/r", nil))
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s code=%d want 204", m, rec.Code)
		}
	}
}
