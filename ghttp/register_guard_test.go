package ghttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRegister_RejectsLowercaseStandardMethod 锁定 P1-6：Go 服务器原样透传客户端方法，
// 注册 "get" 永远匹配不到 GET，却会建出一棵无用树，并让 MatchRoute 对真实请求返回空串
// ——观测层彻底丢失路由归属，且没人会想到是大小写笔误。
// Locks P1-6: Go passes the client's method verbatim, so a "get" route can never be
// hit by GET yet still builds a tree, and MatchRoute returns an empty route for the
// real request — route attribution silently disappears from observability, with no
// hint that a capitalization typo was the cause.
func TestRegister_RejectsLowercaseStandardMethod(t *testing.T) {
	t.Parallel()

	s := New()
	err := s.RawHandle("get", "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	})
	if err == nil {
		t.Fatal("want an error for a lowercase standard method")
	}
	if !strings.Contains(err.Error(), `"GET"`) {
		t.Fatalf("error should suggest the canonical form: %v", err)
	}
}

// TestRegister_RejectsMethodWithInvalidTokenChars 锁另一半：方法含空格/控制字节会建出
// 任何合法客户端都发不出来的路由，静默浪费内存且永不命中。
// Locks the other half: a method containing a space or control byte builds a route no
// legal client can ever send, silently wasting memory and never matching.
func TestRegister_RejectsMethodWithInvalidTokenChars(t *testing.T) {
	t.Parallel()

	s := New()
	for _, bad := range []string{"GE T", "GET\n", "", "GET\x00"} {
		err := s.RawHandle(bad, "/x", func(_ context.Context, _ *Request, _ *Response) error { return nil })
		if err == nil {
			t.Fatalf("want an error for method %q", bad)
		}
		if !errors.Is(err, ErrInvalidParam) {
			t.Fatalf("error %#v does not wrap ErrInvalidParam", err)
		}
	}
}

// TestRegister_CustomMethodTokenAllowed 是反向保护：自定义方法（RFC 9110 允许扩展）不
// 得被误杀。
// TestRegister_CustomMethodTokenAllowed is the reverse guard: custom methods (allowed
// as extensions by RFC 9110) must not be rejected.
func TestRegister_CustomMethodTokenAllowed(t *testing.T) {
	t.Parallel()

	s := New()
	if err := s.RawHandle("WEBHOOK", "/hook", func(_ context.Context, _ *Request, _ *Response) error { return nil }); err != nil {
		t.Fatalf("custom method WEBHOOK must be registrable: %v", err)
	}
}

// TestRegister_AfterServingIsRefused 锁定 P1-7：路由树是 map + 无锁读，服务开始后注册与
// 匹配构成数据竞争（可被 race detector 判死），却长期"看起来能跑"。现在显式拒绝。
// Locks P1-7: the route tree is a map read without locks, so registering after serving
// began races with matching (the race detector rightly flags it) while appearing to
// work. It is now refused explicitly.
func TestRegister_AfterServingIsRefused(t *testing.T) {
	t.Parallel()

	s := New()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})

	// 等 Serve 进入 running 状态后再注册，必须被守卫拒绝（而不是"看起来成功"）。
	// Wait until Serve has reached the running state, then require the guard to refuse
	// the registration rather than letting it appear to succeed.
	deadline := time.Now().Add(3 * time.Second)
	for !s.mux.serving.Load() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !s.mux.serving.Load() {
		t.Fatal("Serve never marked the server as running")
	}
	err = s.RawHandle("GET", "/late", func(_ context.Context, _ *Request, _ *Response) error { return nil })
	if !errors.Is(err, ErrRegistrationAfterStart) {
		t.Fatalf("registration after serving = %v, want ErrRegistrationAfterStart", err)
	}
}
