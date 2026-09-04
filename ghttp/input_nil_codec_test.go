package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 缺陷 B(M10)回归:Body[B](nil) 返回的是一个【非 nil 的接口值】(内部 decoder 为 nil),
// 注册期的 in == nil 判不出它,随后 in.contentType() 直接 nil 解引用 panic——已导出的
// ErrMissingCodec 哨兵形同虚设。修复后注册期必须返回 ErrMissingCodec 且绝不 panic。

type nilCodecBody struct {
	Name string `json:"name"`
}

type nilCodecParams struct {
	ID int `path:"id"`
}

type nilCodecOut struct {
	OK bool `json:"ok"`
}

func nilCodecBodyHandler(_ context.Context, _ nilCodecBody) (nilCodecOut, error) {
	return nilCodecOut{OK: true}, nil
}

func nilCodecParamsBodyHandler(_ context.Context, _ nilCodecParams, _ nilCodecBody) (nilCodecOut, error) {
	return nilCodecOut{OK: true}, nil
}

// registerOutcome 捕获一次注册调用的结果:panic 与 error 二者之一。
type registerOutcome struct {
	err      error
	panicked any
}

// runRegister 执行 fn 并同时捕获 error 与 panic,使"不 panic"本身成为可断言的事实。
func runRegister(fn func() error) (out registerOutcome) {
	defer func() {
		out.panicked = recover()
	}()
	out.err = fn()
	return
}

// TestRegisterBody_NilCodecReturnsErrMissingCodec 覆盖所有 body 入口对两种 nil 形态的
// 处理:字面 nil 接口值,以及 Body[B](nil) 造出的内含 nil decoder 的非 nil 接口值。
func TestRegisterBody_NilCodecReturnsErrMissingCodec(t *testing.T) {
	// bodyOnly 是 registerBody 系入口(无 params)。
	bodyOnly := map[string]func(r router, in InputSpec[nilCodecBody]) error{
		"PostBody": func(r router, in InputSpec[nilCodecBody]) error {
			return PostBody(r, "/x", in, JSON[nilCodecOut](), nilCodecBodyHandler)
		},
		"PutBody": func(r router, in InputSpec[nilCodecBody]) error {
			return PutBody(r, "/x", in, JSON[nilCodecOut](), nilCodecBodyHandler)
		},
		"PatchBody": func(r router, in InputSpec[nilCodecBody]) error {
			return PatchBody(r, "/x", in, JSON[nilCodecOut](), nilCodecBodyHandler)
		},
		"DeleteBody": func(r router, in InputSpec[nilCodecBody]) error {
			return DeleteBody(r, "/x", in, JSON[nilCodecOut](), nilCodecBodyHandler)
		},
	}
	// paramsBody 是 registerParamsBody 系入口(params + body)。
	paramsBody := map[string]func(r router, in InputSpec[nilCodecBody]) error{
		"PostParamsBody": func(r router, in InputSpec[nilCodecBody]) error {
			return PostParamsBody(r, "/x/{id}", in, JSON[nilCodecOut](), nilCodecParamsBodyHandler)
		},
		"PutParamsBody": func(r router, in InputSpec[nilCodecBody]) error {
			return PutParamsBody(r, "/x/{id}", in, JSON[nilCodecOut](), nilCodecParamsBodyHandler)
		},
		"PatchParamsBody": func(r router, in InputSpec[nilCodecBody]) error {
			return PatchParamsBody(r, "/x/{id}", in, JSON[nilCodecOut](), nilCodecParamsBodyHandler)
		},
		"DeleteParamsBody": func(r router, in InputSpec[nilCodecBody]) error {
			return DeleteParamsBody(r, "/x/{id}", in, JSON[nilCodecOut](), nilCodecParamsBodyHandler)
		},
	}

	// inputs 是两种 nil 形态。第二种才是 M10 的真凶。
	inputs := map[string]InputSpec[nilCodecBody]{
		"literal nil interface":     nil,
		"Body with nil codec":       Body[nilCodecBody](nil),
		"Body with typed nil codec": Body[nilCodecBody](RequestDecoder(nil)),
	}

	for _, entries := range []map[string]func(r router, in InputSpec[nilCodecBody]) error{bodyOnly, paramsBody} {
		for entry, register := range entries {
			for shape, in := range inputs {
				t.Run(entry+"/"+shape, func(t *testing.T) {
					out := runRegister(func() error { return register(New(), in) })
					if out.panicked != nil {
						t.Fatalf("registration panicked: %v", out.panicked)
					}
					if !errors.Is(out.err, ErrMissingCodec) {
						t.Fatalf("err = %v, want ErrMissingCodec", out.err)
					}
				})
			}
		}
	}
}

// TestRegisterBody_NilCodecOnGroupReturnsErrMissingCodec 确认分组注册路径同样受保护
// (Group 是独立的 router 实现)。
func TestRegisterBody_NilCodecOnGroupReturnsErrMissingCodec(t *testing.T) {
	g := New().Group("/api")
	out := runRegister(func() error {
		return PostBody(g, "/x", Body[nilCodecBody](nil), JSON[nilCodecOut](), nilCodecBodyHandler)
	})
	if out.panicked != nil {
		t.Fatalf("registration panicked: %v", out.panicked)
	}
	if !errors.Is(out.err, ErrMissingCodec) {
		t.Fatalf("err = %v, want ErrMissingCodec", out.err)
	}
}

// TestRegisterBody_NilCodecLeavesNoRoute 断言失败的注册不留下半成品路由:否则请求期
// 才会在 decode 上 nil 解引用,把注册期缺陷推迟成线上 500。
func TestRegisterBody_NilCodecLeavesNoRoute(t *testing.T) {
	s := New()
	if err := PostBody(s, "/x", Body[nilCodecBody](nil), JSON[nilCodecOut](), nilCodecBodyHandler); !errors.Is(err, ErrMissingCodec) {
		t.Fatalf("err = %v, want ErrMissingCodec", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (a rejected registration must not leave a route)", rec.Code)
	}
}

// TestRegisterBody_NilOutputStillChecked 确认 ErrMissingOutput 优先级不变:两个哨兵
// 各管一侧,新增的 codec 检查不得抢占输出检查。
func TestRegisterBody_NilOutputStillChecked(t *testing.T) {
	out := runRegister(func() error {
		return PostBody[nilCodecBody, nilCodecOut](New(), "/x", Body[nilCodecBody](nil), nil, nilCodecBodyHandler)
	})
	if out.panicked != nil {
		t.Fatalf("registration panicked: %v", out.panicked)
	}
	if !errors.Is(out.err, ErrMissingOutput) {
		t.Fatalf("err = %v, want ErrMissingOutput", out.err)
	}
}

// TestBodyInput_AccessorsTolerateNilCodec 直接覆盖内部访问器:它们必须自身安全,
// 不能依赖"注册期已先拒绝"这一调用顺序假设。
func TestBodyInput_AccessorsTolerateNilCodec(t *testing.T) {
	in := Body[nilCodecBody](nil)
	// Body[T](nil) 仍是非 nil 接口值——这正是 in == nil 检查失效的根因,固化它以防回归。
	if in == nil {
		t.Fatal("Body[T](nil) unexpectedly returned a nil interface value")
	}
	spec, ok := in.(bodyInput[nilCodecBody])
	if !ok {
		t.Fatalf("Body returned %T, want bodyInput", in)
	}
	if !spec.codecMissing() {
		t.Error("codecMissing() = false, want true for a nil codec")
	}
	if got := spec.contentType(); got != "" {
		t.Errorf("contentType() = %q, want empty", got)
	}
	if got := spec.contentTypes(); got != nil {
		t.Errorf("contentTypes() = %v, want nil", got)
	}
}

// TestBodyInput_CodecPresentIsNotReportedMissing 对照组:真实 codec 不得被误判为缺失。
func TestBodyInput_CodecPresentIsNotReportedMissing(t *testing.T) {
	cases := map[string]InputSpec[nilCodecBody]{
		"JSONBody":         JSONBody[nilCodecBody](),
		"XMLBody":          XMLBody[nilCodecBody](),
		"FormBody":         FormBody[nilCodecBody](),
		"Body(JSONCodec)":  Body[nilCodecBody](JSONCodec()),
		"Body(XMLCodec)":   Body[nilCodecBody](XMLCodec()),
		"Body(customDecl)": Body[nilCodecBody](ctSingleDecoder{ct: "application/json"}),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			spec, ok := in.(bodyInput[nilCodecBody])
			if !ok {
				t.Fatalf("got %T, want bodyInput", in)
			}
			if spec.codecMissing() {
				t.Error("codecMissing() = true for a present codec")
			}
			if err := PostBody(New(), "/x", in, JSON[nilCodecOut](), nilCodecBodyHandler); err != nil {
				t.Errorf("registration failed: %v", err)
			}
		})
	}
}

// TestTextBody_NilCodecUnaffected 确认 TextBody 这类不带参数的糖没有 nil 通路。
func TestTextBody_NilCodecUnaffected(t *testing.T) {
	if err := PostBody(New(), "/x", TextBody[string](), JSON[nilCodecOut](),
		func(_ context.Context, _ string) (nilCodecOut, error) { return nilCodecOut{OK: true}, nil }); err != nil {
		t.Fatalf("TextBody registration failed: %v", err)
	}
}
