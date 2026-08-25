package app

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ——— typed 路由端到端测试：覆盖各种请求体 ———

// newTypedApp 创建含 typed 路由的测试 app。
func newTypedApp(t *testing.T) (http.Handler, func()) {
	t.Helper()
	h, cleanup := New(Config{Secret: testSecret})
	return h, cleanup
}

func typedGet(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func typedPost(t *testing.T, handler http.Handler, target, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, target, r)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func typedMultipart(t *testing.T, handler http.Handler, target string, build func(w *multipart.Writer)) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	build(w)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// ——— 测试开始 ———

func TestTyped_ParamsBinding(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedGet(t, h, "/typed/params/42?lang=en&weight=10")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":42`) || !strings.Contains(rec.Body.String(), `"lang":"en"`) || !strings.Contains(rec.Body.String(), `"weight":10`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_ParamsMissingRequired(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/typed/params/", nil)
	h.ServeHTTP(rec, req)
	// path "/typed/params/" 不匹配 "/typed/params/{id}" → 404
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing id, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTyped_JSONBodyDecode(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/json", `{"name":"alice","tags":["ghttp"],"count":3}`, "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"alice"`) || !strings.Contains(rec.Body.String(), `"count":3`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_JSONBodyDecodeError(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/json", `not json`, "application/json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestTyped_XMLBodyDecode(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/xml", `<payload><name>alice</name></payload>`, "application/xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"alice"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_XMLBodyDecodeError(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/xml", `<payload><name>`, "application/xml")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for truncated XML, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTyped_FormURLEncoded(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/form", "name=alice&note=hello&size=5&ok=true&rate=0.5", "application/x-www-form-urlencoded")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"alice"`) || !strings.Contains(rec.Body.String(), `"size":5`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_FormMissingFields(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/form", "name=alice", "application/x-www-form-urlencoded")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"alice"`) || !strings.Contains(rec.Body.String(), `"size":0`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_FormMultipart(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedMultipart(t, h, "/typed/form", func(w *multipart.Writer) {
		_ = w.WriteField("name", "bob")
		_ = w.WriteField("note", "multipart")
		_ = w.WriteField("size", "3")
		_ = w.WriteField("ok", "true")
		_ = w.WriteField("rate", "0.25")
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"bob"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_TextBody(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/text", "hello world", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"value":"hello world"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_TextBodyEmpty(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/text", "", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"value":""`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_MixedParamsBody(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedPost(t, h, "/typed/mixed/7", `{"name":"alice","count":1}`, "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"alice"`) || !strings.Contains(rec.Body.String(), `"count":1`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_UploadFileOnly(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedMultipart(t, h, "/typed/upload", func(w *multipart.Writer) {
		part, _ := w.CreateFormFile("file", "test.txt")
		_, _ = part.Write([]byte("hello upload"))
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"filename":"test.txt"`) || !strings.Contains(rec.Body.String(), `"size":12`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_UploadWithFormFields(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedMultipart(t, h, "/typed/upload-mixed", func(w *multipart.Writer) {
		_ = w.WriteField("note", "greeting")
		part, _ := w.CreateFormFile("file", "hello.txt")
		_, _ = part.Write([]byte("data"))
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"note":"greeting"`) || !strings.Contains(rec.Body.String(), `"filename":"hello.txt"`) || !strings.Contains(rec.Body.String(), `"size":4`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestTyped_GetNone(t *testing.T) {
	h, cleanup := newTypedApp(t)
	t.Cleanup(cleanup)
	rec := typedGet(t, h, "/typed/none")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"value":"none"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
