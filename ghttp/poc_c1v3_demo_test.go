package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// C1 POC v3 演示 — 验证风格甲(专用入口):零 In/None 包裹、类型全推断、
// group 分组 + middleware 复用、三层逃生。
// 运行: go test ./ghttp/ -run TestC1v3 -v
// ============================================================================

// 传输层参数(带 tag,不复用)
type v3GetParams struct {
	ID     int64  `path:"id"`
	Fields string `query:"fields"`
	Trace  bool   `header:"X-Trace"`
}

// 业务 body(纯净,零传输 tag,可复用)
type v3CreateUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type v3UserOut struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// 场景 1: 无输入 —— func(ctx) (O, error),零包裹零类型参数。
func TestC1v3_NoInput(t *testing.T) {
	m := New()
	// 注意:调用点没有任何 [类型参数],全靠 handler 字面量推断。
	if err := CGet(m, "/health", CJSON[map[string]string](),
		func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"status": "ok"}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("no-input failed: %d %s", rec.Code, rec.Body.String())
	}
}

// 场景 2: 仅 params —— func(ctx, P) (O, error),P 靠 tag 绑 path/query/header。
func TestC1v3_ParamsOnly(t *testing.T) {
	m := New()
	if err := CGetP(m, "/users/{id}", CJSON[v3UserOut](),
		func(ctx context.Context, p v3GetParams) (v3UserOut, error) {
			// p.ID / p.Fields / p.Trace 直接可用,无 in.Params. 前缀
			return v3UserOut{ID: p.ID, Name: p.Fields}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42?fields=alice", nil)
	req.Header.Set("X-Trace", "true")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":42`) {
		t.Errorf("params-only failed: %d %s", rec.Code, rec.Body.String())
	}
}

// 场景 3: 仅 body —— func(ctx, B) (O, error),B 纯净可复用。
func TestC1v3_BodyOnly(t *testing.T) {
	m := New()
	if err := CPostB(m, "/register", CJSONBody(), CJSON[v3UserOut]().Status(http.StatusCreated),
		func(ctx context.Context, b v3CreateUser) (v3UserOut, error) {
			return v3UserOut{ID: 0, Name: b.Name, Email: b.Email}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"name":"bob","email":"b@x.com"}`))
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"name":"bob"`) {
		t.Errorf("body-only failed: %d %s", rec.Code, rec.Body.String())
	}
}

// 场景 4: params+body —— func(ctx, P, B) (O, error),两个裸参数,body 纯净不污染。
func TestC1v3_ParamsAndBody(t *testing.T) {
	m := New()
	if err := CPostPB(m, "/users/{id}", CJSONBody(), CJSON[v3UserOut]().Status(http.StatusCreated),
		func(ctx context.Context, p v3GetParams, b v3CreateUser) (v3UserOut, error) {
			// p 是传输层(带 tag),b 是纯净业务体 —— 分离清晰
			return v3UserOut{ID: p.ID, Name: b.Name, Email: b.Email}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/7", strings.NewReader(`{"name":"carol","email":"c@x.com"}`))
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"id":7`) || !strings.Contains(body, `"name":"carol"`) {
		t.Errorf("params+body failed: %s", body)
	}
}

// 场景 5: group 分组 + middleware 复用 —— 前缀继承、中间件继承、嵌套。
func TestC1v3_GroupAndMiddleware(t *testing.T) {
	m := New()
	var trail []string
	mwTag := func(tag string) Middleware {
		return func(next Handler) Handler {
			return func(ctx context.Context, req *Request, resp *Response) error {
				trail = append(trail, tag)
				return next(ctx, req, resp)
			}
		}
	}

	// 组级中间件:/api 上挂 mwA;嵌套 /api/v1 复用 mwA 并追加 mwB。
	api := m.Group("/api", mwTag("A"))
	v1 := api.Group("/v1", mwTag("B"))

	// 专用入口直接作用于 group(router 接口统一)。
	if err := CGetP(v1, "/users/{id}", CJSON[v3UserOut](),
		func(ctx context.Context, p v3GetParams) (v3UserOut, error) {
			return v3UserOut{ID: p.ID}, nil
		}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/99", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("group route status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":99`) {
		t.Errorf("group route bind failed: %s", rec.Body.String())
	}
	// 中间件复用+继承:mwA(组)→ mwB(嵌套组)按序执行。
	if strings.Join(trail, ",") != "A,B" {
		t.Errorf("middleware order/inherit failed: got %v want [A B]", trail)
	}
}

// 场景 6: 半逃生 —— 额外拿 *Request;全逃生 —— 现有 RawHandle 自己写 SSE。
func TestC1v3_Escapes(t *testing.T) {
	m := New()
	// 半逃生
	if err := CHandleRawP[v3GetParams, map[string]any](m, http.MethodGet, "/inspect/{id}", CJSON[map[string]any](),
		func(ctx context.Context, req *Request, p v3GetParams) (map[string]any, error) {
			return map[string]any{"id": p.ID, "ua": req.Header.Get("User-Agent")}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/inspect/5", nil)
	req.Header.Set("User-Agent", "poc")
	m.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"ua":"poc"`) {
		t.Errorf("half-escape failed: %s", rec.Body.String())
	}

	// 全逃生:SSE 流式写出(现有 RawHandle,框架不介入编码)。
	if err := m.RawHandle(http.MethodGet, "/events", func(ctx context.Context, req *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/event-stream")
		resp.WriteHeader(http.StatusOK)
		resp.WriteString("data: hello\n\n")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))
	if !strings.Contains(rec.Body.String(), "data: hello") {
		t.Errorf("full-escape SSE failed: %s", rec.Body.String())
	}
}

// 场景 7: 动态码 + 下载(仅 params)。
func TestC1v3_DynamicAndDownload(t *testing.T) {
	m := New()
	type idParam struct {
		ID int64 `path:"id"`
	}
	if err := CResultP[idParam, v3UserOut](m, http.MethodGet, "/u/{id}",
		func(ctx context.Context, p idParam) (CResult[v3UserOut], error) {
			if p.ID == 1 {
				return COK(v3UserOut{ID: 1, Name: "alice"}), nil
			}
			return CStatus(http.StatusNotFound, v3UserOut{}), nil
		}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path string
		want int
	}{{"/u/1", http.StatusOK}, {"/u/9", http.StatusNotFound}} {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s: %d want %d", c.path, rec.Code, c.want)
		}
	}

	type nameParam struct {
		Name string `path:"name"`
	}
	if err := CDownloadP[nameParam](m, http.MethodGet, "/dl/{name}",
		func(ctx context.Context, p nameParam) (CFileDownload, error) {
			return CFileDownload{Filename: p.Name + ".txt", ContentType: "text/plain",
				Content: strings.NewReader("body of " + p.Name), Size: int64(len("body of " + p.Name))}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dl/report", nil))
	if !strings.Contains(rec.Body.String(), "body of report") ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "report.txt") {
		t.Errorf("download failed: %s / %s", rec.Body.String(), rec.Header().Get("Content-Disposition"))
	}
}

// 端到端性能:仅 params(3字段 path+query+header),固定码 JSON。
func BenchmarkC1v3EndToEnd(b *testing.B) {
	m := New()
	_ = CGetP(m, "/users/{id}", CJSON[v3GetParams](),
		func(ctx context.Context, p v3GetParams) (v3GetParams, error) { return p, nil })
	req := httptest.NewRequest(http.MethodGet, "/users/42?fields=name", nil)
	req.Header.Set("X-Trace", "true")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
	}
}
