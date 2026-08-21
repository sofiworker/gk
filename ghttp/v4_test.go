package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ===========================================================================
// 甲-2 专用入口测试（正式版 v4）。
// Tests for v4-style typed entries (formal version).
// ============================================================================

type v4Params struct {
	ID     int64  `path:"id"`
	Fields string `query:"fields"`
	Trace  string `header:"X-Trace"`
}

type v4Out struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ——— GetParams（仅 params，无 body）——

func TestGetParams_PathQueryHeader(t *testing.T) {
	m := New()
	if err := GetParams(m, "/users/{id}", JSON[v4Out](), func(ctx context.Context, p v4Params) (v4Out, error) {
		return v4Out{ID: p.ID, Name: p.Fields + ":" + p.Trace}, nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42?fields=golang", nil)
	req.Header.Set("X-Trace", "abc")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"id":42`) || !strings.Contains(body, `golang:abc`) {
		t.Errorf("bind failed: %s", body)
	}
}

func TestDeleteParams_Registration(t *testing.T) {
	m := New()
	if err := DeleteParams(m, "/items/{id}", JSON[v4Out](), func(ctx context.Context, p v4Params) (v4Out, error) {
		return v4Out{ID: p.ID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/items/99", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":99`) {
		t.Errorf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— PostParamsBody（params + body）——

type v4Body struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func TestPostParamsBody_MixedInput(t *testing.T) {
	m := New()
	if err := PostParamsBody(m, "/users/{id}", JSONBody(), JSON[v4Out]().Status(http.StatusCreated),
		func(ctx context.Context, p v4Params, b v4Body) (v4Out, error) {
			return v4Out{ID: p.ID, Name: b.Name}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users/7", strings.NewReader(`{"name":"alice","email":"a@x.com"}`))
	req.Header.Set("Content-Type", "application/json")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"id":7`) || !strings.Contains(body, `"name":"alice"`) {
		t.Errorf("mixed bind failed: %s", body)
	}
}

func TestPutParamsBody_FullMix(t *testing.T) {
	m := New()
	if err := PutParamsBody(m, "/users/{id}", JSONBody(), JSON[v4Out](),
		func(ctx context.Context, p v4Params, b v4Body) (v4Out, error) {
			return v4Out{ID: p.ID, Name: b.Name}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/users/15?fields=x", strings.NewReader(`{"name":"bob","email":"b@x.com"}`))
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"bob"`) {
		t.Errorf("put mix failed: %s", rec.Body.String())
	}
}

// ——— nil decoder 报错 ——

func TestPostParamsBody_NilDecoderError(t *testing.T) {
	m := New()
	err := PostParamsBody(m, "/x/{id}", nil, JSON[v4Out](),
		func(ctx context.Context, p v4Params, b v4Body) (v4Out, error) { return v4Out{}, nil })
	if err != ErrMissingCodec {
		t.Errorf("want ErrMissingCodec, got %v", err)
	}
}

// ——— nil output 报错 ——

func TestGetParams_NilOutputError(t *testing.T) {
	m := New()
	err := GetParams[v4Params, v4Out](m, "/x/{id}", nil,
		func(ctx context.Context, p v4Params) (v4Out, error) { return v4Out{}, nil })
	if err != ErrMissingOutput {
		t.Errorf("want ErrMissingOutput, got %v", err)
	}
}

// ——— 无 tag 字段静默跳过 ——

type v4PartialTag struct {
	Valid   int    `query:"valid"`
	Skipped string // 无 tag，静默跳过
}

func TestGetParams_UntaggedFieldSkipped(t *testing.T) {
	m := New()
	if err := GetParams(m, "/skip", JSON[int](), func(ctx context.Context, p v4PartialTag) (int, error) {
		if p.Skipped != "" {
			t.Errorf("untagged field should stay zero, got %q", p.Skipped)
		}
		return p.Valid, nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/skip?valid=123&skipped=ignored", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "123") {
		t.Errorf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— GetNone（无 params 无 body）——

func TestGetNone_HealthCheck(t *testing.T) {
	m := New()
	if err := GetNone(m, "/health", JSON[v4Out](), func(ctx context.Context) (v4Out, error) {
		return v4Out{ID: 1, Name: "ok"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"ok"`) {
		t.Errorf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— PostBody（仅 body，无 params）——

func TestPostBody_NoParams(t *testing.T) {
	m := New()
	if err := PostBody(m, "/register", JSONBody(), JSON[v4Out]().Status(http.StatusCreated),
		func(ctx context.Context, b v4Body) (v4Out, error) {
			return v4Out{ID: 0, Name: b.Name}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"name":"carol","email":"c@x.com"}`))
	req.Header.Set("Content-Type", "application/json")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"name":"carol"`) {
		t.Errorf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— PutBody 用 PUT method 注册（回归 method 委托 bug）——

func TestPutBody_UsesPutMethod(t *testing.T) {
	m := New()
	if err := PutBody(m, "/replace", JSONBody(), JSON[v4Out](),
		func(ctx context.Context, b v4Body) (v4Out, error) {
			return v4Out{Name: b.Name}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// PUT 请求应命中
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/replace", strings.NewReader(`{"name":"x"}`)))
	if rec.Code != http.StatusOK {
		t.Errorf("PUT should hit: code=%d", rec.Code)
	}
	// POST 请求不应命中（405 或 404），验证没有错误地注册成 POST
	rec2 := httptest.NewRecorder()
	m.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/replace", strings.NewReader(`{"name":"x"}`)))
	if rec2.Code == http.StatusOK {
		t.Errorf("POST should NOT hit a PUT route, but got 200 — method delegation bug")
	}
}

// ——— XMLCodec 作为 body decoder（验证单侧可替换）——

type v4XMLBody struct {
	XMLName struct{} `xml:"user"`
	Name    string   `xml:"name"`
}

func TestPostBody_XMLDecoder(t *testing.T) {
	m := New()
	if err := PostBody(m, "/xml", XMLCodec(), JSON[v4Out](),
		func(ctx context.Context, b v4XMLBody) (v4Out, error) {
			return v4Out{Name: b.Name}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/xml", strings.NewReader(`<user><name>xmluser</name></user>`))
	req.Header.Set("Content-Type", "application/xml")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"xmluser"`) {
		t.Errorf("XML decode failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
}
