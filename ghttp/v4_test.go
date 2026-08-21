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
