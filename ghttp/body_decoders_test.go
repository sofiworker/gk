package ghttp

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type formTarget struct {
	Name  string  `form:"name" json:"name"`
	Count int     `form:"count" json:"count"`
	Ratio float64 `form:"ratio" json:"ratio"`
	OK    bool    `form:"ok" json:"ok"`
}

func TestFormBody_DecodesURLEncoded(t *testing.T) {
	dec := formCodec{}
	if dec.ContentType() != "" {
		t.Fatalf("ContentType = %q", dec.ContentType())
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=alice&count=3&ratio=0.5&ok=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ghttpReq := &Request{Request: req}
	var dst formTarget
	if err := dec.Decode(ghttpReq, &dst); err != nil {
		t.Fatal(err)
	}
	if dst.Name != "alice" || dst.Count != 3 || dst.Ratio != 0.5 || !dst.OK {
		t.Fatalf("decoded = %+v", dst)
	}
}

func TestFormBody_MultipartFormValues(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("name", "bob")
	_ = w.WriteField("count", "7")
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	ghttpReq := &Request{Request: req}
	var dst formTarget
	if err := (formCodec{}).Decode(ghttpReq, &dst); err != nil {
		t.Fatal(err)
	}
	if dst.Name != "bob" || dst.Count != 7 {
		t.Fatalf("decoded = %+v", dst)
	}
}

func TestFormBody_ErrorOnMalformed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ghttpReq := &Request{Request: req}
	err := (formCodec{}).Decode(ghttpReq, &formTarget{})
	if err == nil {
		t.Fatal("expected error on malformed form")
	}
}

func TestFormBody_EmptyBodySucceeds(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ghttpReq := &Request{Request: req}
	var dst formTarget
	if err := (formCodec{}).Decode(ghttpReq, &dst); err != nil {
		t.Fatalf("empty body should succeed: %v", err)
	}
	if dst.Name != "" || dst.Count != 0 {
		t.Fatalf("empty body should yield zero values: %+v", dst)
	}
}

func TestFormBody_ErrorOnIntOverflow(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("count=99999999999999999999"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ghttpReq := &Request{Request: req}
	err := (formCodec{}).Decode(ghttpReq, &formTarget{})
	if err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestTextBody_DecodesToString(t *testing.T) {
	dec := textCodec{}
	if dec.ContentType() != "text/plain" {
		t.Fatalf("ContentType = %q", dec.ContentType())
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello world"))
	ghttpReq := &Request{Request: req}
	var dst string
	if err := dec.Decode(ghttpReq, &dst); err != nil {
		t.Fatal(err)
	}
	if dst != "hello world" {
		t.Fatalf("decoded = %q", dst)
	}
}

func TestTextBody_DecodesToBytes(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("raw bytes"))
	ghttpReq := &Request{Request: req}
	var dst []byte
	if err := (textCodec{}).Decode(ghttpReq, &dst); err != nil {
		t.Fatal(err)
	}
	if string(dst) != "raw bytes" {
		t.Fatalf("decoded = %q", dst)
	}
}

func TestTextBody_BadTarget(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	ghttpReq := &Request{Request: req}
	var dst int
	if err := (textCodec{}).Decode(ghttpReq, &dst); err == nil {
		t.Fatal("expected error for int target")
	}
}

func TestTextBody_EmptyBodySucceeds(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ghttpReq := &Request{Request: req}
	var dst string
	if err := (textCodec{}).Decode(ghttpReq, &dst); err != nil {
		t.Fatalf("empty body should succeed: %v", err)
	}
	if dst != "" {
		t.Fatalf("empty body should yield empty string: %q", dst)
	}
}

func TestPostBody_FormDecoder(t *testing.T) {
	m := New()
	if err := PostBody(m, "/form", FormBody[formTarget](), JSON[formTarget](),
		func(_ context.Context, b formTarget) (formTarget, error) {
			return b, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader("name=alice&count=3&ratio=0.5&ok=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"alice"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostBody_TextDecoder_String(t *testing.T) {
	m := New()
	if err := PostBody(m, "/echo", TextBody[string](), JSON[string](),
		func(_ context.Context, b string) (string, error) {
			return b, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("hello"))
	req.Header.Set("Content-Type", "text/plain")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hello"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostBody_TextDecoder_Bytes(t *testing.T) {
	m := New()
	if err := PostBody(m, "/echo-bytes", TextBody[[]byte](), JSON[[]byte](),
		func(_ context.Context, b []byte) ([]byte, error) {
			return b, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo-bytes", strings.NewReader("binary"))
	req.Header.Set("Content-Type", "text/plain")
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
}

// uploadBody 是一个含单文件上传字段的表单请求体:文件现在归请求体,由 FormBody[T] 解码。
// uploadBody is a form body with a single file upload field: files now belong to
// the request body, decoded by FormBody[T].
type uploadBody struct {
	File Upload `form:"file"`
}

func TestFormBody_Upload(t *testing.T) {
	m := New()
	if err := PostBody(m, "/upload", FormBody[uploadBody](), JSON[uploadResult](),
		func(_ context.Context, b uploadBody) (uploadResult, error) {
			f, err := b.File.Open()
			if err != nil {
				return uploadResult{}, err
			}
			defer f.Close()
			data, _ := io.ReadAll(f)
			return uploadResult{Filename: b.File.Filename, Size: len(data)}, nil
		}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("file", "hello.txt")
	_, _ = part.Write([]byte("hello ghttp"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"filename":"hello.txt"`) || !strings.Contains(rec.Body.String(), `"size":11`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

type uploadResult struct {
	Filename string `json:"filename"`
	Size     int    `json:"size"`
}

// mixedUploadBody 把表单文本字段与上传文件放在同一个请求体结构体里,由 FormBody[T] 一并解码。
// mixedUploadBody puts a form text field and an uploaded file in one body struct,
// decoded together by FormBody[T].
type mixedUploadBody struct {
	Note string `form:"note"`
	File Upload `form:"file"`
}

// TestPostParamsBody_FormBodyWithPathParam 验证 path 参数(params) + 表单请求体(含文件)。
// TestPostParamsBody_FormBodyWithPathParam checks a path param (params) plus a
// form request body (with a file).
func TestPostParamsBody_FormBodyWithPathParam(t *testing.T) {
	m := New()
	if err := PostParamsBody(m, "/users/{id}/upload", FormBody[mixedUploadBody](), JSON[mixedUploadResult](),
		func(_ context.Context, p mixedUploadPathParams, b mixedUploadBody) (mixedUploadResult, error) {
			f, _ := b.File.Open()
			defer f.Close()
			data, _ := io.ReadAll(f)
			return mixedUploadResult{UserID: p.UserID, Note: b.Note, Filename: b.File.Filename, Size: len(data)}, nil
		}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("note", "greeting")
	part, _ := w.CreateFormFile("file", "test.txt")
	_, _ = part.Write([]byte("data"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/users/7/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"note":"greeting"`) || !strings.Contains(body, `"filename":"test.txt"`) || !strings.Contains(body, `"user_id":7`) {
		t.Fatalf("body=%s", body)
	}
}

type mixedUploadPathParams struct {
	UserID int64 `path:"id"`
}

type mixedUploadResult struct {
	UserID   int64  `json:"user_id"`
	Note     string `json:"note"`
	Filename string `json:"filename"`
	Size     int    `json:"size"`
}
