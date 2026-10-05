package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func etagDo(t testing.TB, mw Middleware, method string, hdr map[string]string, h Handler) (*httptest.ResponseRecorder, *Response) {
	t.Helper()
	r := httptest.NewRequest(method, "/x", nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}
	if err := mw(h)(r.Context(), &Request{Raw: r}, resp); err != nil {
		t.Errorf("err: %v", err)
	}
	return rec, resp
}

func etagBody(body string) Handler {
	return func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/plain")
		resp.Header().Set("Cache-Control", "max-age=60")
		resp.Header().Add("Vary", "Accept")
		_, err := resp.Write([]byte(body))
		return err
	}
}

func TestETagComputeAndNotModified(t *testing.T) {
	rec, _ := etagDo(t, ETag(), "GET", nil, etagBody("hello"))
	et := rec.Header().Get("Etag")
	if !strings.HasPrefix(et, `W/"5-`) || rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("etag=%q code=%d body=%q", et, rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Length") != "5" {
		t.Errorf("Content-Length = %q", rec.Header().Get("Content-Length"))
	}

	rec, resp := etagDo(t, ETag(), "GET", map[string]string{"If-None-Match": et}, etagBody("hello"))
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if h.Get("Etag") != et || h.Get("Cache-Control") != "max-age=60" || h.Get("Vary") != "Accept" ||
		h.Get("Content-Length") != "" || h.Get("Content-Type") != "" {
		t.Errorf("headers = %v", h)
	}
	if resp.StatusCode() != http.StatusNotModified || resp.Size() != 0 {
		t.Errorf("resp status=%d size=%d", resp.StatusCode(), resp.Size())
	}

	rec, _ = etagDo(t, ETag(), "GET", map[string]string{"If-None-Match": `W/"other"`}, etagBody("hello"))
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("miss: code=%d", rec.Code)
	}
}

func TestETagMatchForms(t *testing.T) {
	et := etagCompute([]byte("hello"))
	strong := strings.TrimPrefix(et, "W/")
	tests := []struct {
		name, inm string
		want      int
	}{
		{"list", `"a", ` + et + `, "b"`, 304},
		{"star", "*", 304},
		{"weak vs strong", strong, 304},
		{"quoted comma", `"a,b", "c"`, 200},
		{"miss list", `"a", "b"`, 200},
		{"empty", "", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr := map[string]string{}
			if tt.inm != "" {
				hdr["If-None-Match"] = tt.inm
			}
			rec, _ := etagDo(t, ETag(), "GET", hdr, etagBody("hello"))
			if rec.Code != tt.want {
				t.Errorf("code = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestETagHead(t *testing.T) {
	rec, _ := etagDo(t, ETag(), "HEAD", nil, etagBody("hello"))
	et := rec.Header().Get("Etag")
	if et == "" || rec.Code != 200 {
		t.Fatalf("etag=%q code=%d", et, rec.Code)
	}
	rec, _ = etagDo(t, ETag(), "HEAD", map[string]string{"If-None-Match": et}, etagBody("hello"))
	if rec.Code != 304 {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestETagSkipsOtherMethods(t *testing.T) {
	rec, _ := etagDo(t, ETag(), "POST", map[string]string{"If-None-Match": "*"}, etagBody("hello"))
	if rec.Code != 200 || rec.Header().Get("Etag") != "" {
		t.Fatalf("code=%d etag=%q", rec.Code, rec.Header().Get("Etag"))
	}
}

func TestETagNon200NotComputed(t *testing.T) {
	h := func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusNotFound)
		_, err := resp.Write([]byte("nope"))
		return err
	}
	rec, _ := etagDo(t, ETag(), "GET", map[string]string{"If-None-Match": "*"}, h)
	if rec.Code != 404 || rec.Header().Get("Etag") != "" || rec.Body.String() != "nope" {
		t.Fatalf("code=%d etag=%q body=%q", rec.Code, rec.Header().Get("Etag"), rec.Body.String())
	}
}

func TestETagHandlerProvided(t *testing.T) {
	h := func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("ETag", `"v1"`)
		_, err := resp.Write([]byte("body"))
		return err
	}
	rec, _ := etagDo(t, ETag(), "GET", nil, h)
	if rec.Header().Get("Etag") != `"v1"` || rec.Code != 200 || rec.Body.String() != "body" {
		t.Fatalf("headers=%v code=%d", rec.Header(), rec.Code)
	}
	rec, _ = etagDo(t, ETag(), "GET", map[string]string{"If-None-Match": `W/"v1"`}, h)
	if rec.Code != 304 || rec.Body.Len() != 0 || rec.Header().Get("Etag") != `"v1"` {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestETagOverLimitPassThrough(t *testing.T) {
	big := strings.Repeat("x", 100)
	rec, _ := etagDo(t, ETag(WithETagMaxBytes(50)), "GET", map[string]string{"If-None-Match": "*"}, etagBody(big))
	if rec.Code != 200 || rec.Body.String() != big || rec.Header().Get("Etag") != "" {
		t.Fatalf("code=%d len=%d etag=%q", rec.Code, rec.Body.Len(), rec.Header().Get("Etag"))
	}
	// 分块写入累计超限 / cumulative overrun across chunks
	h := func(_ context.Context, _ *Request, resp *Response) error {
		for i := 0; i < 10; i++ {
			if _, err := resp.Write([]byte("0123456789")); err != nil {
				return err
			}
		}
		return nil
	}
	rec, _ = etagDo(t, ETag(WithETagMaxBytes(35)), "GET", nil, h)
	if rec.Code != 200 || rec.Body.Len() != 100 || rec.Header().Get("Etag") != "" {
		t.Fatalf("chunked: code=%d len=%d", rec.Code, rec.Body.Len())
	}
	// 声明的 Content-Length 超限直接直通 / declared Content-Length over the cap passes through
	h = func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Length", strconv.Itoa(len(big)))
		_, err := resp.Write([]byte(big))
		return err
	}
	rec, _ = etagDo(t, ETag(WithETagMaxBytes(50)), "GET", nil, h)
	if rec.Header().Get("Etag") != "" || rec.Body.Len() != 100 {
		t.Fatalf("content-length: %v", rec.Header())
	}
}

func TestETagFlushPassThrough(t *testing.T) {
	h := func(_ context.Context, _ *Request, resp *Response) error {
		_, _ = resp.Write([]byte("part1"))
		resp.Flush()
		_, err := resp.Write([]byte("part2"))
		return err
	}
	rec, _ := etagDo(t, ETag(), "GET", map[string]string{"If-None-Match": "*"}, h)
	if rec.Code != 200 || rec.Body.String() != "part1part2" || rec.Header().Get("Etag") != "" || !rec.Flushed {
		t.Fatalf("code=%d body=%q flushed=%v", rec.Code, rec.Body.String(), rec.Flushed)
	}
}

func TestETagUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &etagWriter{orig: rec}
	if w.Unwrap() != http.ResponseWriter(rec) {
		t.Fatal("Unwrap mismatch")
	}
}

func TestETagWithJSONHandler(t *testing.T) {
	type out struct {
		Name string `json:"name"`
	}
	s := NewServer()
	s.Use(ETag())
	err := s.Register(Get("/u", func(context.Context, RequestOf[NoDataType]) (out, error) {
		return out{Name: "alice"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	rec := coreDo(s, "GET", "/u", "")
	et := rec.Header().Get("Etag")
	if rec.Code != 200 || et == "" || !strings.Contains(rec.Body.String(), "alice") {
		t.Fatalf("code=%d etag=%q body=%q", rec.Code, et, rec.Body.String())
	}
	rec = coreDo(s, "GET", "/u", "", "If-None-Match", et)
	if rec.Code != 304 || rec.Body.Len() != 0 {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestETagHandlerErrorBeforeWrite(t *testing.T) {
	s := NewServer()
	s.Use(ETag())
	err := s.Register(Raw(http.MethodGet, "/e", func(context.Context, *Request, *Response) error {
		return ErrNotFound
	}))
	if err != nil {
		t.Fatal(err)
	}
	if rec := coreDo(s, "GET", "/e", ""); rec.Code != 404 {
		t.Fatalf("code = %d", rec.Code)
	}
}
