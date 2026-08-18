package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOperationMapInputs345BuildsBusinessInput 验证扁平化的三/四/五元组合器
// 按顺序构造输入并映射业务类型,且错误短路顺序正确。
// TestOperationMapInputs345BuildsBusinessInput verifies the flat 3/4/5-way
// combinators build inputs in order with correct error short-circuiting.
func TestOperationMapInputs345BuildsBusinessInput(t *testing.T) {
	type mapped struct {
		A, B, C, D, E string
	}

	t.Run("3", func(t *testing.T) {
		input := MapInputs3(PathString("a"), PathString("b"), PathString("c"), func(a, b, c string) mapped {
			return mapped{A: a, B: b, C: c}
		})
		server := New()
		server.MustMount(GetJSON("/m/{a}/{b}/{c}", input, func(_ context.Context, m mapped) (mapped, error) { return m, nil }))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/m/1/2/3", nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != `{"A":"1","B":"2","C":"3","D":"","E":""}` {
			t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("4", func(t *testing.T) {
		input := MapInputs4(PathString("a"), PathString("b"), PathString("c"), PathString("d"), func(a, b, c, d string) mapped {
			return mapped{A: a, B: b, C: c, D: d}
		})
		server := New()
		server.MustMount(GetJSON("/m/{a}/{b}/{c}/{d}", input, func(_ context.Context, m mapped) (mapped, error) { return m, nil }))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/m/1/2/3/4", nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != `{"A":"1","B":"2","C":"3","D":"4","E":""}` {
			t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("5", func(t *testing.T) {
		input := MapInputs5(PathString("a"), PathString("b"), PathString("c"), PathString("d"), PathString("e"), func(a, b, c, d, e string) mapped {
			return mapped{A: a, B: b, C: c, D: d, E: e}
		})
		server := New()
		server.MustMount(GetJSON("/m/{a}/{b}/{c}/{d}/{e}", input, func(_ context.Context, m mapped) (mapped, error) { return m, nil }))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/m/1/2/3/4/5", nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != `{"A":"1","B":"2","C":"3","D":"4","E":"5"}` {
			t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
		}
	})
}

// TestOperationMapInputs345RejectInvalidComposition 验证扁平组合器保留注册期
// 校验:重复参数、nil mapper 与 nil 输入都在 Mount 时报错。
// TestOperationMapInputs345RejectInvalidComposition verifies the flat
// combinators keep registration-time validation: duplicate params, nil mappers
// and nil inputs are rejected at Mount.
func TestOperationMapInputs345RejectInvalidComposition(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		input := MapInputs4(QueryString("a"), QueryString("b"), QueryString("c"), QueryString("a"), func(a, b, c, d string) string {
			return a + b + c + d
		})
		server := New()
		if err := server.Mount(Handle(Get("/dup"), input, TextOutput(), func(_ context.Context, s string) (string, error) { return s, nil })); err == nil {
			t.Fatal("mount with duplicate query parameter should fail")
		}
	})
	t.Run("nil mapper", func(t *testing.T) {
		input := MapInputs5[string, string, string, string, string, string](
			QueryString("a"), QueryString("b"), QueryString("c"), QueryString("d"), QueryString("e"), nil)
		server := New()
		if err := server.Mount(Handle(Get("/nilmap"), input, TextOutput(), func(_ context.Context, s string) (string, error) { return s, nil })); err == nil {
			t.Fatal("mount with nil mapper should fail")
		}
	})
	t.Run("nil input", func(t *testing.T) {
		input := MapInputs3(nil, QueryString("b"), QueryString("c"), func(a, b, c string) string {
			return a + b + c
		})
		server := New()
		if err := server.Mount(Handle(Get("/nilin"), input, TextOutput(), func(_ context.Context, s string) (string, error) { return s, nil })); err == nil {
			t.Fatal("mount with nil input should fail")
		}
	})
}

// TestOperationDefaultInputsFallBack 验证 Default 变体:缺失回退默认值、存在
// 时解析、约束违规返回 400。
// TestOperationDefaultInputsFallBack verifies the Default variants: absent
// values fall back, present values parse, and constraint violations return 400.
func TestOperationDefaultInputsFallBack(t *testing.T) {
	server := New()
	server.MustMount(Handle(Get("/defaults"),
		MapInputs5(
			QueryStringDefault("q", "fallback", AllowedValues("fallback", "real")),
			QueryBoolDefault("flag", true),
			QueryFloat64Default("score", 1.5, Minimum(0)),
			HeaderStringDefault("X-Trace", "none"),
			CookieStringDefault("sid", "anon"),
			func(q string, flag bool, score float64, trace string, sid string) map[string]any {
				return map[string]any{"q": q, "flag": flag, "score": score, "trace": trace, "sid": sid}
			},
		),
		JSONOutput[map[string]any](),
		func(_ context.Context, m map[string]any) (map[string]any, error) { return m, nil },
	))

	// 全部缺失:默认值。
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/defaults", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != `{"flag":true,"q":"fallback","score":1.5,"sid":"anon","trace":"none"}` {
		t.Fatalf("body = %q", got)
	}

	// 显式提供:解析值 + header + cookie。
	request := httptest.NewRequest(http.MethodGet, "/defaults?q=real&flag=false&score=2.5", nil)
	request.Header.Set("X-Trace", "t-1")
	request.AddCookie(&http.Cookie{Name: "sid", Value: "s-1"})
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if got := recorder.Body.String(); got != `{"flag":false,"q":"real","score":2.5,"sid":"s-1","trace":"t-1"}` {
		t.Fatalf("body = %q", got)
	}

	// 约束违规:400。
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/defaults?q=banned", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// TestOperationValidatedInputForwardsRequestOnly 验证 ValidatedInput 透传内层
// 输入的 requestOnly 标记,保证校验包装不阻断无状态快路径。
// TestOperationValidatedInputForwardsRequestOnly verifies ValidatedInput
// forwards the inner input's requestOnly marker so validation wrappers do not
// block the stateless fast path.
func TestOperationValidatedInputForwardsRequestOnly(t *testing.T) {
	if !isRequestOnlyInput(ValidatedInput(QueryString("q"), func(_ context.Context, _ string) error { return nil })) {
		t.Fatal("ValidatedInput over request-only input should stay request-only")
	}
	if isRequestOnlyInput(ValidatedInput(JSONBody[map[string]any](), func(_ context.Context, _ map[string]any) error { return nil })) {
		t.Fatal("ValidatedInput over body input must not be request-only")
	}
}
