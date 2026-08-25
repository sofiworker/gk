package ghttp

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ——— form: 标量字段直接绑定到 params(免 body 解码器) ——— //

type formParamsReq struct {
	Name  string  `form:"name"`
	Age   int     `form:"age"`
	Score float64 `form:"score"`
	Admin bool    `form:"admin"`
}

type formParamsResult struct {
	Name  string  `json:"name"`
	Age   int     `json:"age"`
	Score float64 `json:"score"`
	Admin bool    `json:"admin"`
}

func TestFormParams_URLEncoded(t *testing.T) {
	m := New()
	if err := PostParams(m, "/form", JSON[formParamsResult](),
		func(_ context.Context, p formParamsReq) (formParamsResult, error) {
			return formParamsResult{Name: p.Name, Age: p.Age, Score: p.Score, Admin: p.Admin}, nil
		}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/form",
		strings.NewReader("name=alice&age=30&score=9.5&admin=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !strings.Contains(got, `"name":"alice"`) || !strings.Contains(got, `"age":30`) ||
		!strings.Contains(got, `"score":9.5`) || !strings.Contains(got, `"admin":true`) {
		t.Fatalf("unexpected body: %s", got)
	}
}

func TestFormParams_Multipart(t *testing.T) {
	m := New()
	if err := PostParams(m, "/form-mp", JSON[formParamsResult](),
		func(_ context.Context, p formParamsReq) (formParamsResult, error) {
			return formParamsResult{Name: p.Name, Age: p.Age}, nil
		}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "bob")
	_ = w.WriteField("age", "42")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/form-mp", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"bob"`) ||
		!strings.Contains(rec.Body.String(), `"age":42`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— form 字段与 path/query/header + upload 混用 ——— //

type formMixedReq struct {
	ID     int64  `path:"id"`
	Trace  string `header:"X-Trace"`
	Title  string `form:"title"`
	Cover  Upload `form:"cover"`
	Public bool   `form:"public"`
}

type formMixedResult struct {
	ID     int64  `json:"id"`
	Trace  string `json:"trace"`
	Title  string `json:"title"`
	Cover  string `json:"cover"`
	Public bool   `json:"public"`
}

func TestFormParams_MixedWithPathHeaderUpload(t *testing.T) {
	m := New()
	if err := PostParams(m, "/posts/{id}", JSON[formMixedResult](),
		func(_ context.Context, p formMixedReq) (formMixedResult, error) {
			return formMixedResult{
				ID: p.ID, Trace: p.Trace, Title: p.Title,
				Cover: p.Cover.Filename, Public: p.Public,
			}, nil
		}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("title", "hello world")
	_ = w.WriteField("public", "true")
	part, _ := w.CreateFormFile("cover", "cover.png")
	_, _ = part.Write([]byte("imgbytes"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/posts/77", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Trace", "trace-xyz")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !strings.Contains(got, `"id":77`) || !strings.Contains(got, `"trace":"trace-xyz"`) ||
		!strings.Contains(got, `"title":"hello world"`) || !strings.Contains(got, `"cover":"cover.png"`) ||
		!strings.Contains(got, `"public":true`) {
		t.Fatalf("unexpected body: %s", got)
	}
}

// ——— form 字段的 validate ——— //

type formValidateReq struct {
	Email string `form:"email" validate:"required,email"`
	Qty   int    `form:"qty" validate:"min=1,max=10"`
}

func TestFormParams_ValidateRequired(t *testing.T) {
	m := New()
	if err := PostParams(m, "/fv", JSON[map[string]string](),
		func(_ context.Context, p formValidateReq) (map[string]string, error) {
			return map[string]string{"email": p.Email}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// 缺 email → 400。Missing email → 400.
	req := httptest.NewRequest(http.MethodPost, "/fv", strings.NewReader("qty=3"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required form field, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFormParams_ValidateRange(t *testing.T) {
	m := New()
	if err := PostParams(m, "/fv2", JSON[map[string]int](),
		func(_ context.Context, p formValidateReq) (map[string]int, error) {
			return map[string]int{"qty": p.Qty}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// qty=99 越界 → 400。qty=99 out of range → 400.
	req := httptest.NewRequest(http.MethodPost, "/fv2",
		strings.NewReader("email=a@b.com&qty=99"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range form field, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— form 字段缺失且非必填:保留零值 ——— //

func TestFormParams_OptionalMissing(t *testing.T) {
	m := New()
	if err := PostParams(m, "/fo", JSON[formParamsResult](),
		func(_ context.Context, p formParamsReq) (formParamsResult, error) {
			return formParamsResult{Name: p.Name, Age: p.Age}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// 只给 name,age 缺失应为 0。Only name; missing age stays 0.
	req := httptest.NewRequest(http.MethodPost, "/fo", strings.NewReader("name=solo"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"solo"`) ||
		!strings.Contains(rec.Body.String(), `"age":0`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ——— 纯 path/query/header 端点不受影响(不解析 body) ——— //

type noFormReq struct {
	ID   int64  `path:"id"`
	Page int    `query:"page"`
	Auth string `header:"Authorization"`
}

func TestFormParams_NoFormFieldsSkipsParse(t *testing.T) {
	m := New()
	if err := GetParams(m, "/items/{id}", JSON[map[string]any](),
		func(_ context.Context, p noFormReq) (map[string]any, error) {
			return map[string]any{"id": p.ID, "page": p.Page, "auth": p.Auth}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// GET 带 body(异常但合法):不应被解析,端点照常工作。
	// GET with a body (unusual but legal): must not be parsed, endpoint still works.
	req := httptest.NewRequest(http.MethodGet, "/items/5?page=2",
		strings.NewReader("should=not&be=parsed"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	got := rec.Body.String()
	if !strings.Contains(got, `"id":5`) || !strings.Contains(got, `"page":2`) ||
		!strings.Contains(got, `"auth":"Bearer tok"`) {
		t.Fatalf("unexpected body: %s", got)
	}
}
