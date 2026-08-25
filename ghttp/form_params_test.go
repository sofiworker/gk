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

// ——— form: 标量字段作为请求体,经 FormBody[T]() 解码 ——— //
// 表单字段属于请求体(而非 path/query/header 参数),与文件上传走同一条 form 解码轴。
// Form fields belong to the request body (not path/query/header params) and share
// the same form-decode axis as file uploads.

type formBodyReq struct {
	Name  string  `form:"name"`
	Age   int     `form:"age"`
	Score float64 `form:"score"`
	Admin bool    `form:"admin"`
}

type formBodyResult struct {
	Name  string  `json:"name"`
	Age   int     `json:"age"`
	Score float64 `json:"score"`
	Admin bool    `json:"admin"`
}

func TestFormBody_URLEncoded(t *testing.T) {
	m := New()
	if err := PostBody(m, "/form", FormBody[formBodyReq](), JSON[formBodyResult](),
		func(_ context.Context, b formBodyReq) (formBodyResult, error) {
			return formBodyResult{Name: b.Name, Age: b.Age, Score: b.Score, Admin: b.Admin}, nil
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

func TestFormBody_Multipart(t *testing.T) {
	m := New()
	if err := PostBody(m, "/form-mp", FormBody[formBodyReq](), JSON[formBodyResult](),
		func(_ context.Context, b formBodyReq) (formBodyResult, error) {
			return formBodyResult{Name: b.Name, Age: b.Age}, nil
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

// ——— path/header 参数(params) + 含文件与文本的表单请求体(body) 混用 ——— //

type formMixedParams struct {
	ID    int64  `path:"id"`
	Trace string `header:"X-Trace"`
}

type formMixedBody struct {
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

func TestFormBody_MixedWithPathHeaderUpload(t *testing.T) {
	m := New()
	if err := PostParamsBody(m, "/posts/{id}", FormBody[formMixedBody](), JSON[formMixedResult](),
		func(_ context.Context, p formMixedParams, b formMixedBody) (formMixedResult, error) {
			return formMixedResult{
				ID: p.ID, Trace: p.Trace, Title: b.Title,
				Cover: b.Cover.Filename, Public: b.Public,
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

// ——— form 字段缺失且非必填:保留零值 ——— //

func TestFormBody_OptionalMissing(t *testing.T) {
	m := New()
	if err := PostBody(m, "/fo", FormBody[formBodyReq](), JSON[formBodyResult](),
		func(_ context.Context, b formBodyReq) (formBodyResult, error) {
			return formBodyResult{Name: b.Name, Age: b.Age}, nil
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
