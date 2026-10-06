package ghttp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// 大小写不敏感路径修正（WithRedirectFixedPath → fixedPath → findCaseInsensitivePath）
// Case-insensitive path repair (WithRedirectFixedPath → fixedPath → findCaseInsensitivePath)
// -----------------------------------------------------------------------------

func covFixedServer(t *testing.T, routes ...Route) *Server {
	t.Helper()
	s := NewServer(WithRedirectFixedPath())
	if err := s.Register(routes...); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	return s
}

func covEcho(_ context.Context, _ RequestOf[NoDataType]) (string, error) {
	return "ok", nil
}

func TestFixedPathCaseInsensitiveStatic(t *testing.T) {
	s := covFixedServer(t, Get("/Gopher/Path", covEcho))

	// 错误大小写 → 301 修正
	// wrong case → 301 repair
	rec := coreDo(s, http.MethodGet, "/gopher/path", "")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if loc := rec.Header().Get("Location"); loc != "/Gopher/Path" {
		t.Fatalf("Location = %q, want %q", loc, "/Gopher/Path")
	}

	// 多余尾斜杠 → 301 修正并去掉
	// redundant trailing slash → 301 repair without it
	rec = coreDo(s, http.MethodGet, "/Gopher/Path/", "")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("trailing slash: status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if loc := rec.Header().Get("Location"); loc != "/Gopher/Path" {
		t.Fatalf("trailing slash: Location = %q, want %q", loc, "/Gopher/Path")
	}

	// 正确路径不受影响
	// correct path is unaffected
	rec = coreDo(s, http.MethodGet, "/Gopher/Path", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("correct case: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestFixedPathParamsAndCatchAll(t *testing.T) {
	s := covFixedServer(t,
		Get("/Users/:id/posts/:pid", covEcho),
		Get("/files/*path", covEcho),
	)

	rec := coreDo(s, http.MethodGet, "/users/7/posts/9", "")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("params: status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if loc := rec.Header().Get("Location"); loc != "/Users/7/posts/9" {
		t.Fatalf("params: Location = %q, want %q", loc, "/Users/7/posts/9")
	}

	rec = coreDo(s, http.MethodGet, "/FILES/docs/a.pdf", "")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("catch-all: status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if loc := rec.Header().Get("Location"); loc != "/files/docs/a.pdf" {
		t.Fatalf("catch-all: Location = %q, want %q", loc, "/files/docs/a.pdf")
	}
}

func TestFixedPathNonGetRedirects308(t *testing.T) {
	s := covFixedServer(t, Delete("/Gopher/Del", func(context.Context, RequestOf[NoDataType]) (string, error) {
		return "ok", nil
	}))

	rec := coreDo(s, http.MethodDelete, "/gopher/del", "")
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusPermanentRedirect)
	}
	if loc := rec.Header().Get("Location"); loc != "/Gopher/Del" {
		t.Fatalf("Location = %q, want %q", loc, "/Gopher/Del")
	}
}

func TestFixedPathKeepsQuery(t *testing.T) {
	s := covFixedServer(t, Get("/Gopher/Path", covEcho))

	rec := coreDo(s, http.MethodGet, "/gopher/path?page=2&q=x", "")
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	loc := rec.Header().Get("Location")
	if want := "/Gopher/Path?page=2&q=x"; loc != want {
		t.Fatalf("Location = %q, want %q", loc, want)
	}
}

// TestRadixFindCaseInsensitiveUnicode 直测 radix 的多字节字符分支。
// TestRadixFindCaseInsensitiveUnicode exercises the multi-byte rune branch directly.
func TestRadixFindCaseInsensitiveUnicode(t *testing.T) {
	root := &radixNode{fullPath: "/"}
	h := Handler(func(context.Context, *Request, *Response) error { return nil })
	if err := addRadixRoute(root, "/Ünicode/Päth", h); err != nil {
		t.Fatalf("addRadixRoute failed: %v", err)
	}

	got, ok := root.findCaseInsensitivePath("/ünicode/päth", false)
	if !ok || string(got) != "/Ünicode/Päth" {
		t.Fatalf("got = %q, ok = %v, want %q", string(got), ok, "/Ünicode/Päth")
	}
	if _, ok := root.findCaseInsensitivePath("/nope", false); ok {
		t.Fatal("unexpected match for /nope")
	}
}

// -----------------------------------------------------------------------------
// 冲突 catch-all 回退表（lookupCatchAll）与 405/HEAD 行为
// Conflicting catch-all fallback table (lookupCatchAll) with 405/HEAD behavior
// -----------------------------------------------------------------------------

func TestConflictingCatchAllFallback(t *testing.T) {
	s := NewServer()
	err := s.Register(
		Get("/static/*filepath", covEcho),
		// 与前一条冲突，进入 catchAll 回退表
		// conflicts with the first, lands in the catchAll fallback table
		Get("/static/*rest", covEcho),
	)
	if err != nil {
		t.Fatalf("conflicting catch-all should be shimmed, got: %v", err)
	}

	// 树内 catch-all 正常命中
	// in-tree catch-all matches normally
	if rec := coreDo(s, http.MethodGet, "/static/a/b.css", ""); rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", rec.Code, http.StatusOK)
	}

	// HEAD 无显式路由时回退到 GET（同时穿过 lookupCatchAll）
	// HEAD falls back to GET (passing through lookupCatchAll)
	if rec := coreDo(s, http.MethodHead, "/static/a/b.css", ""); rec.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want %d", rec.Code, http.StatusOK)
	}

	// 未注册方法 → 405，Allow 恰为 GET/HEAD/OPTIONS（树与回退表不得重复贡献）
	// unregistered method → 405 with Allow exactly GET/HEAD/OPTIONS (no duplicates from tree+fallback)
	rec := coreDo(s, http.MethodPost, "/static/a/b.css", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	allow := rec.Header().Get("Allow")
	if allow != "GET, HEAD, OPTIONS" {
		t.Fatalf("Allow = %q, want %q", allow, "GET, HEAD, OPTIONS")
	}
}

func TestConflictingCatchAllDuplicate(t *testing.T) {
	s := NewServer()
	if err := s.Register(Get("/static/*filepath", covEcho), Get("/static/*rest", covEcho)); err != nil {
		t.Fatalf("first register failed: %v", err)
	}
	err := s.Register(Get("/static/*rest", covEcho))
	if !errors.Is(err, ErrRouteConflict) {
		t.Fatalf("err = %v, want ErrRouteConflict", err)
	}
}

// TestInvalidWildcardRegistration 覆盖 findWildcard 的非法通配符分支。
// TestInvalidWildcardRegistration covers findWildcard's invalid wildcard branches.
func TestInvalidWildcardRegistration(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"two params in segment", "/x/:a:b"},
		{"param and catch-all mixed", "/x/:a*"},
		{"catch-all and param mixed", "/x/*a:b"},
		{"invalid escape string", "/a\\bc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer()
			err := s.Register(Get(tc.path, covEcho))
			if !errors.Is(err, ErrRouteConflict) {
				t.Fatalf("path %q: err = %v, want ErrRouteConflict", tc.path, err)
			}
		})
	}
}

// TestEscapedColonLiteralSegment 覆盖 findWildcard 的 \\: 转义分支：冒号成为字面量。
// TestEscapedColonLiteralSegment covers the \\: escape branch: the colon is literal.
func TestEscapedColonLiteralSegment(t *testing.T) {
	s := NewServer()
	if err := s.Register(Get("/files/\\:name", covEcho)); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	rec := coreDo(s, http.MethodGet, `/files/\:name`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %.100s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// -----------------------------------------------------------------------------
// gzip / etag 写入器：1xx 透传与重复 WriteHeader（直测写入器）
// gzip / etag writers: 1xx passthrough and duplicate WriteHeader (direct writer tests)
// -----------------------------------------------------------------------------

func TestValueMissingReportsInvalidInput(t *testing.T) {
	s := NewServer()
	err := s.Register(Get("/check", func(_ context.Context, req RequestOf[NoDataType]) (string, error) {
		if _, err := req.QueryValue("missing").String(); !errors.Is(err, ErrInvalidInput) {
			return "", fmt.Errorf("query: err = %v, want ErrInvalidInput", err)
		}
		if _, err := req.HeaderValue("X-Missing").String(); !errors.Is(err, ErrInvalidInput) {
			return "", fmt.Errorf("header: err = %v, want ErrInvalidInput", err)
		}
		if _, err := req.CookieValue("missing").String(); !errors.Is(err, ErrInvalidInput) {
			return "", fmt.Errorf("cookie: err = %v, want ErrInvalidInput", err)
		}
		if _, err := req.PathValue("missing").String(); !errors.Is(err, ErrInvalidInput) {
			return "", fmt.Errorf("path: err = %v, want ErrInvalidInput", err)
		}
		return "ok", nil
	}))
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	rec := coreDo(s, http.MethodGet, "/check", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %.200s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// -----------------------------------------------------------------------------
// 表单解码：嵌入指针结构体、time.Time 与 TextUnmarshaler（formFieldAlloc/formIsLeaf）
// Form decoding: embedded pointer structs, time.Time, TextUnmarshaler (formFieldAlloc/formIsLeaf)
// -----------------------------------------------------------------------------

type CovFormInner struct {
	City string `form:"city"`
	Zip  string `form:"zip"`
}

type CovFormPtrInner struct {
	Line string `form:"line"`
}

// covFormBody 用匿名嵌入展开嵌套结构（命名 struct 字段不受表单解码支持），
// 其中 *CovFormPtrInner 覆盖 formFieldAlloc 的按需分配分支。
// covFormBody flattens nested structs via anonymous embedding (named struct fields are not
// supported by form decoding); *CovFormPtrInner covers formFieldAlloc's lazy allocation.
type covFormBody struct {
	Name string `form:"name"`
	Age  int    `form:"age"`
	CovFormInner
	*CovFormPtrInner
	TS      time.Time   `form:"ts"`
	TSValue covTextTime `form:"tsv"`
}

// covTextTime 实现 encoding.TextUnmarshaler，覆盖 formIsLeaf 的 TextUnmarshaler 分支。
// covTextTime implements encoding.TextUnmarshaler to cover the formIsLeaf branch.
type covTextTime struct{ t time.Time }

func (c *covTextTime) UnmarshalText(b []byte) error {
	parsed, err := time.Parse(time.RFC3339, string(b))
	if err != nil {
		return err
	}
	c.t = parsed
	return nil
}

func TestFormDecodeNestedAndTextUnmarshaler(t *testing.T) {
	s := NewServer()
	err := s.Register(Post("/form", func(ctx context.Context, req RequestOf[covFormBody]) (map[string]any, error) {
		body, err := req.Data(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"name":  body.Name,
			"age":   body.Age,
			"city":  body.City,
			"zip":   body.Zip,
			"line":  body.Line,
			"ts":    body.TS.Unix(),
			"tsv":   body.TSValue.t.Unix(),
			"year":  body.TS.Year(),
			"vyear": body.TSValue.t.Year(),
		}, nil
	}, WithInput(FormInput[covFormBody]())))
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := coreDo(s, http.MethodPost, "/form",
		"name=Alice&age=30&city=Shanghai&zip=200000&line=1+Example+Road"+
			"&ts=2024-01-02T15%3A04%3A05Z&tsv=2023-06-15T08%3A00%3A00Z",
		"Content-Type", "application/x-www-form-urlencoded")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body %.300s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"name":"Alice"`, `"age":30`, `"city":"Shanghai"`, `"zip":"200000"`,
		`"line":"1 Example Road"`, `"year":2024`, `"vyear":2023`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body %s missing %s", body, want)
		}
	}
}

// -----------------------------------------------------------------------------
// gzip / etag 写入器：1xx 透传与重复 WriteHeader（直测写入器）
// gzip / etag writers: 1xx passthrough and duplicate WriteHeader (direct writer tests)
// -----------------------------------------------------------------------------

// covStatusWriter 记录每一次 WriteHeader，用于验证 1xx 透传 + 最终状态提交。
// covStatusWriter records every WriteHeader to verify 1xx passthrough and final commit.
type covStatusWriter struct {
	header   http.Header
	statuses []int
	body     []byte
}

func newCovStatusWriter() *covStatusWriter { return &covStatusWriter{header: make(http.Header)} }

func (w *covStatusWriter) Header() http.Header { return w.header }

func (w *covStatusWriter) WriteHeader(code int) { w.statuses = append(w.statuses, code) }

func (w *covStatusWriter) Write(p []byte) (int, error) {
	w.body = append(w.body, p...)
	return len(p), nil
}

func TestGzWriterEarlyHintsPassThrough(t *testing.T) {
	rec := newCovStatusWriter()
	gw := &gzWriter{orig: rec, cfg: &gzConfig{level: 0, types: gzDefaultTypes}}

	gw.WriteHeader(http.StatusEarlyHints)
	if len(rec.statuses) != 1 || rec.statuses[0] != http.StatusEarlyHints {
		t.Fatalf("statuses = %v, want [%d]", rec.statuses, http.StatusEarlyHints)
	}

	// 1xx 之后正式 200；重复 WriteHeader 被忽略
	// then a real 200; duplicate WriteHeader is ignored
	gw.WriteHeader(http.StatusOK)
	gw.WriteHeader(http.StatusInternalServerError)
	if n, err := gw.Write([]byte("payload")); n != len("payload") || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	gw.close()
	if len(rec.statuses) != 2 || rec.statuses[1] != http.StatusOK {
		t.Fatalf("statuses = %v, want [103, 200]", rec.statuses)
	}
	if len(rec.body) == 0 {
		t.Fatal("body is empty")
	}
}

func TestEtagWriterEarlyHintsPassThrough(t *testing.T) {
	rec := newCovStatusWriter()
	ew := &etagWriter{orig: rec, max: 1 << 20}

	ew.WriteHeader(http.StatusEarlyHints)
	if len(rec.statuses) != 1 || rec.statuses[0] != http.StatusEarlyHints {
		t.Fatalf("statuses = %v, want [%d]", rec.statuses, http.StatusEarlyHints)
	}

	ew.WriteHeader(http.StatusOK)
	ew.WriteHeader(http.StatusTeapot)
	if _, err := ew.Write([]byte("etag-body")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	ew.finish(&Response{Writer: rec})
	if len(rec.statuses) != 2 || rec.statuses[1] != http.StatusOK {
		t.Fatalf("statuses = %v, want [103, 200]", rec.statuses)
	}
	if string(rec.body) != "etag-body" {
		t.Fatalf("body = %q, want %q", rec.body, "etag-body")
	}
	if rec.header.Get("Etag") == "" {
		t.Fatal("Etag header missing")
	}
}

// TestServerEarlyHintsWithGzip 走完整服务器链路验证 1xx 在 gzip 写入器下透传。
// TestServerEarlyHintsWithGzip verifies 1xx passthrough through the gzip writer end to end.
func TestServerEarlyHintsWithGzip(t *testing.T) {
	s := NewServer()
	if err := s.Register(Raw(http.MethodGet, "/early", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusEarlyHints)
		_, err := resp.Write([]byte("early-body"))
		return err
	})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	s.Use(Gzip())

	rec := coreDo(s, http.MethodGet, "/early", "", "Accept-Encoding", "gzip")
	if rec.Code != http.StatusEarlyHints {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusEarlyHints)
	}
	if rec.Body.String() != "early-body" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "early-body")
	}
}

// -----------------------------------------------------------------------------
// 静态资源错误映射（staticMapErr）
// Static file error mapping (staticMapErr)
// -----------------------------------------------------------------------------

func TestStaticMapErr(t *testing.T) {
	var nf HTTPError
	notFound := staticMapErr(fs.ErrNotExist)
	if !errors.As(notFound, &nf) || nf.Status != http.StatusNotFound {
		t.Fatalf("ErrNotExist: got %v, want NotFound HTTPError", notFound)
	}
	if got := staticMapErr(fs.ErrPermission); !errors.As(got, &nf) || nf.Status != http.StatusNotFound {
		t.Fatalf("ErrPermission: got %v, want NotFound HTTPError", got)
	}
	if got := staticMapErr(fs.ErrInvalid); !errors.As(got, &nf) || nf.Status != http.StatusNotFound {
		t.Fatalf("ErrInvalid: got %v, want NotFound HTTPError", got)
	}
	other := errors.New("boom")
	if got := staticMapErr(other); got != other {
		t.Fatalf("other: got %v, want passthrough", got)
	}
}
