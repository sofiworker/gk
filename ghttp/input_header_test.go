package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type authHeaders struct {
	Token   string
	Version int
	Trace   bool
}

// Header 组合器:多字段端到端 + 大小写无关(canonicalize)。
func TestHeaderCombinatorEndToEnd(t *testing.T) {
	m := New()
	err := Get(m, "/secure",
		NoInput[NoPath](),
		Header(
			HStr("X-Token", func(h *authHeaders, v string) { h.Token = v }),
			HInt("X-Api-Version", func(h *authHeaders, v int) { h.Version = v }),
			HBool("X-Trace", func(h *authHeaders, v bool) { h.Trace = v }),
		),
		NoInput[NoBody](),
		JSON[authHeaders](),
		func(ctx context.Context, in RequestInput[NoPath, authHeaders, NoBody]) (authHeaders, error) {
			return in.Query, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	// 故意用非规范大小写,验证 Header.Values 的 canonicalize。
	req.Header.Set("x-token", "secret")
	req.Header.Set("X-API-VERSION", "3")
	req.Header.Set("x-trace", "on")
	m.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"Token":"secret","Version":3,"Trace":true}`+"\n" {
		t.Errorf("header body = %s", got)
	}
}

// Header .Required() 缺失 → ErrMissingRequired。
func TestHeaderRequiredMissing(t *testing.T) {
	src := Header(HStr("X-Token", func(h *authHeaders, v string) { h.Token = v }).Required())
	decode, err := src.(hsource[authHeaders]).bind(&endpointSpec{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	_, derr := decode(&Request{Request: req})
	if !errors.Is(derr, ErrMissingRequired) {
		t.Errorf("missing required header: err = %v, want ErrMissingRequired", derr)
	}
}

// Header 类型解析失败 → 500 兜底,业务函数不执行。
func TestHeaderTypeError(t *testing.T) {
	m := New()
	ran := false
	err := Get(m, "/secure",
		NoInput[NoPath](),
		Header(HInt("X-Api-Version", func(h *authHeaders, v int) { h.Version = v })),
		NoInput[NoBody](),
		JSON[authHeaders](),
		func(ctx context.Context, in RequestInput[NoPath, authHeaders, NoBody]) (authHeaders, error) {
			ran = true
			return in.Query, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("X-Api-Version", "notnum")
	m.ServeHTTP(rec, req)
	if ran {
		t.Error("handler ran despite header parse error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// 单值 Header 源 + 与 middleware 贯通:HeaderString 取值,RequestID 中间件同时生效。
func TestHeaderScalarWithMiddleware(t *testing.T) {
	m := New()
	m.Use(RequestID())
	var reqID string
	err := Get(m, "/echo",
		NoInput[NoPath](),
		HeaderString("X-Token"),
		NoInput[NoBody](),
		JSON[string](),
		func(ctx context.Context, in RequestInput[NoPath, string, NoBody]) (string, error) {
			reqID = RequestIDFromContext(ctx)
			return in.Query, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	req.Header.Set("X-Token", "hello")
	m.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != `"hello"`+"\n" {
		t.Errorf("scalar header body = %s, want \"hello\"", got)
	}
	if reqID == "" {
		t.Error("middleware did not run alongside header source")
	}
	if rec.Header().Get(HeaderRequestID) == "" {
		t.Error("RequestID header not set")
	}
}

// 注册期空名:Header 字段名为空 → ErrInvalidParam。
func TestHeaderEmptyNameRegError(t *testing.T) {
	m := New()
	err := Get(m, "/x",
		NoInput[NoPath](),
		Header(HStr("", func(h *authHeaders, v string) {})),
		NoInput[NoBody](),
		JSON[authHeaders](),
		func(ctx context.Context, in RequestInput[NoPath, authHeaders, NoBody]) (authHeaders, error) {
			return in.Query, nil
		})
	if !errors.Is(err, ErrInvalidParam) {
		t.Errorf("empty header name: err = %v, want ErrInvalidParam", err)
	}
}

// 覆盖 HInt64/HBool 组合器 + HeaderInt/HeaderInt64/HeaderBool 单值源。
func TestHeaderFullCoverage(t *testing.T) {
	// HInt64 + HBool 组合器。
	m := New()
	if err := Get(m, "/h",
		NoInput[NoPath](),
		Header(
			HInt64("X-Big", func(h *authHeaders, v int64) { h.Version = int(v) }),
			HBool("X-On", func(h *authHeaders, v bool) { h.Trace = v }),
		),
		NoInput[NoBody](),
		JSON[authHeaders](),
		func(ctx context.Context, in RequestInput[NoPath, authHeaders, NoBody]) (authHeaders, error) {
			return in.Query, nil
		},
	); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/h", nil)
	req.Header.Set("X-Big", "77")
	req.Header.Set("X-On", "true")
	m.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != `{"Token":"","Version":77,"Trace":true}`+"\n" {
		t.Errorf("HInt64+HBool combo body = %s", got)
	}

	// HeaderInt 单值源。
	m2 := New()
	if err := Get(m2, "/hi",
		NoInput[NoPath](), HeaderInt("X-N"), NoInput[NoBody](),
		JSON[int](),
		func(ctx context.Context, in RequestInput[NoPath, int, NoBody]) (int, error) { return in.Query, nil },
	); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/hi", nil)
	req.Header.Set("X-N", "9")
	m2.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != "9\n" {
		t.Errorf("HeaderInt = %s", got)
	}

	// HeaderInt64 单值源。
	m3 := New()
	if err := Get(m3, "/hi64",
		NoInput[NoPath](), HeaderInt64("X-N64"), NoInput[NoBody](),
		JSON[int64](),
		func(ctx context.Context, in RequestInput[NoPath, int64, NoBody]) (int64, error) { return in.Query, nil },
	); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/hi64", nil)
	req.Header.Set("X-N64", "88")
	m3.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != "88\n" {
		t.Errorf("HeaderInt64 = %s", got)
	}

	// HeaderBool 单值源 + 缺失返回零值。
	m4 := New()
	if err := Get(m4, "/hb",
		NoInput[NoPath](), HeaderBool("X-B"), NoInput[NoBody](),
		JSON[bool](),
		func(ctx context.Context, in RequestInput[NoPath, bool, NoBody]) (bool, error) { return in.Query, nil },
	); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/hb", nil)
	req.Header.Set("X-B", "yes")
	m4.ServeHTTP(rec, req)
	if got := rec.Body.String(); got != "true\n" {
		t.Errorf("HeaderBool = %s", got)
	}
	// 缺失 → false。
	rec = httptest.NewRecorder()
	m4.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hb", nil))
	if got := rec.Body.String(); got != "false\n" {
		t.Errorf("HeaderBool absent = %s, want false", got)
	}
}

// HInt64/HBool 解析错误分支 + 空组合器退化。
func TestHeaderEdgeBranches(t *testing.T) {
	// HInt64 解析错误。
	src := Header(HInt64("X-Big", func(h *authHeaders, v int64) { h.Version = int(v) }))
	decode, err := src.(hsource[authHeaders]).bind(&endpointSpec{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Big", "notnum")
	if _, derr := decode(&Request{Request: req}); !errors.Is(derr, ErrInvalidInput) {
		t.Errorf("HInt64 bad: err = %v, want ErrInvalidInput", derr)
	}

	// HBool 解析错误。
	src = Header(HBool("X-On", func(h *authHeaders, v bool) { h.Trace = v }))
	decode, err = src.(hsource[authHeaders]).bind(&endpointSpec{})
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-On", "maybe")
	if _, derr := decode(&Request{Request: req}); !errors.Is(derr, ErrInvalidInput) {
		t.Errorf("HBool bad: err = %v, want ErrInvalidInput", derr)
	}

	// 空组合器退化为 NoInput[authHeaders]。
	empty := Header[authHeaders]()
	if _, ok := empty.(none[authHeaders]); !ok {
		t.Errorf("Header() with no fields should degrade to none[authHeaders], got %T", empty)
	}
}
