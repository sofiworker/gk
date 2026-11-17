package ghttp

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func decGzip(t testing.TB, s string) string {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return b.String()
}

func decZlib(t testing.TB, s string) string {
	t.Helper()
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return b.String()
}

func decRawFlate(t testing.TB, s string) string {
	t.Helper()
	var b bytes.Buffer
	zw, _ := flate.NewWriter(&b, flate.DefaultCompression)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return b.String()
}

// decEchoServer 返回回显请求体的服务器（Decompress 作为全局中间件）。
// decEchoServer returns a server echoing the body, with Decompress as global middleware.
func decEchoServer(t *testing.T, opts ...DecompressOption) *Server {
	t.Helper()
	s := NewServer()
	s.Use(Decompress(opts...))
	err := s.Register(Raw(http.MethodPost, "/x", func(_ context.Context, req *Request, resp *Response) error {
		if req.Raw.Header.Get("X-Plain") == "" {
			if req.Raw.Header.Get("Content-Encoding") != "" || req.Raw.Header.Get("Content-Length") != "" {
				t.Errorf("headers not cleaned: %v", req.Raw.Header)
			}
			if req.Raw.ContentLength != -1 {
				t.Errorf("ContentLength = %d", req.Raw.ContentLength)
			}
		}
		b, err := io.ReadAll(req.Raw.Body)
		if err != nil {
			return err
		}
		_, err = resp.Write(b)
		return err
	}))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDecompressRoundTrip(t *testing.T) {
	plain := strings.Repeat("hello decompress ", 200)
	tests := []struct {
		name, enc, body string
		plainHdr        bool
	}{
		{"gzip", "gzip", decGzip(t, plain), false},
		{"x-gzip", "X-Gzip", decGzip(t, plain), false},
		{"deflate zlib", "deflate", decZlib(t, plain), false},
		{"deflate raw", "deflate", decRawFlate(t, plain), false},
		{"stacked", "gzip, deflate", decZlib(t, decGzip(t, plain)), false},
		{"stacked reverse", "deflate, gzip", decGzip(t, decZlib(t, plain)), false},
		{"identity", "identity", plain, true},
	}
	s := decEchoServer(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr := []string{"Content-Encoding", tt.enc}
			if tt.plainHdr {
				hdr = append(hdr, "X-Plain", "1")
			}
			rec := coreDo(s, "POST", "/x", tt.body, hdr...)
			if rec.Code != 200 || rec.Body.String() != plain {
				t.Fatalf("code=%d len=%d", rec.Code, rec.Body.Len())
			}
		})
	}
}

func TestDecompressNoEncodingPassThrough(t *testing.T) {
	rec := coreDo(decEchoServer(t), "POST", "/x", "plain", "X-Plain", "1")
	if rec.Code != 200 || rec.Body.String() != "plain" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestDecompressUnsupported415(t *testing.T) {
	for _, enc := range []string{"br", "gzip, zstd", "compress"} {
		rec := coreDo(decEchoServer(t), "POST", "/x", "data", "Content-Encoding", enc)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s: code = %d", enc, rec.Code)
		}
		if rec.Header().Get("Accept-Encoding") == "" {
			t.Errorf("%s: missing Accept-Encoding hint", enc)
		}
	}
	rec := coreDo(decEchoServer(t), "POST", "/x", "data", "Content-Encoding", "gzip,gzip,gzip,gzip,gzip")
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("too many layers: code = %d", rec.Code)
	}
}

func TestDecompressBomb413(t *testing.T) {
	plain := strings.Repeat("A", 1<<20)
	body := decGzip(t, plain)
	if len(body) > 10<<10 {
		t.Fatalf("test data not compressible enough: %d", len(body))
	}
	s := decEchoServer(t, WithDecompressMaxBytes(64<<10))
	rec := coreDo(s, "POST", "/x", body, "Content-Encoding", "gzip")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d", rec.Code)
	}
	// 恰好等于上限则放行 / exactly at the cap is allowed
	s = decEchoServer(t, WithDecompressMaxBytes(int64(len(plain))))
	if rec := coreDo(s, "POST", "/x", body, "Content-Encoding", "gzip"); rec.Code != 200 || rec.Body.Len() != len(plain) {
		t.Fatalf("at cap: code = %d len=%d", rec.Code, rec.Body.Len())
	}
}

func TestDecompressBombTypedRoute(t *testing.T) {
	type in struct {
		Pad string `json:"pad"`
	}
	s := NewServer()
	s.Use(Decompress(WithDecompressMaxBytes(1024)))
	err := s.Register(Post("/j", func(ctx context.Context, r RequestOf[in]) (string, error) {
		if _, err := r.Data(ctx); err != nil {
			return "", err
		}
		return "ok", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	big := `{"pad":"` + strings.Repeat("a", 1<<20) + `"}`
	rec := coreDo(s, "POST", "/j", decGzip(t, big), "Content-Encoding", "gzip", "Content-Type", "application/json")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	small := `{"pad":"x"}`
	rec = coreDo(s, "POST", "/j", decGzip(t, small), "Content-Encoding", "gzip", "Content-Type", "application/json")
	if rec.Code != 200 {
		t.Fatalf("small code = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDecompressCorrupt400(t *testing.T) {
	good := decGzip(t, strings.Repeat("hello ", 1000))
	tests := map[string]string{
		"garbage":   "this is not gzip at all",
		"truncated": good[:len(good)/2],
	}
	s := decEchoServer(t)
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			rec := coreDo(s, "POST", "/x", body, "Content-Encoding", "gzip")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code = %d", rec.Code)
			}
		})
	}
}

func TestDecompressBodyErrors(t *testing.T) {
	b := &decBody{src: io.NopCloser(strings.NewReader("junk")), layers: []string{"gzip"}, max: 100}
	if _, err := io.ReadAll(b); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	// 上游传输错误透传 / upstream transport errors pass through
	boom := errors.New("boom")
	src := io.MultiReader(strings.NewReader(decGzip(t, strings.Repeat("abc", 100))[:15]), decErrReader{boom})
	b = &decBody{src: io.NopCloser(src), layers: []string{"gzip"}, max: 1000}
	if _, err := io.ReadAll(b); !errors.Is(err, boom) || errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	var mbe *http.MaxBytesError
	b = &decBody{src: io.NopCloser(strings.NewReader(decGzip(t, strings.Repeat("a", 500)))), layers: []string{"gzip"}, max: 10}
	n, err := io.Copy(io.Discard, b)
	if !errors.As(err, &mbe) || n != 0 || mbe.Limit != 10 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

type decErrReader struct{ err error }

func (e decErrReader) Read([]byte) (int, error) { return 0, e.err }
