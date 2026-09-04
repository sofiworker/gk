package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ===========================================================================
// miss(404/405)冷路径的正确性回归(缺陷 M8)。
//
// 两个原缺陷:
//  1. `_ = m.notFoundHandler(...)` 丢弃返回值——自定义 handler 返回 error 且未写响应时,
//     net/http 隐式回 200 空体,错误彻底消失;
//  2. 构造 `&Request{Request: r}` 未设 owner——handler 内 ClientIP() 因 owner==nil 短路,
//     不遵循 WithTrustedProxies,同一请求命中时得真实客户端 IP、miss 时退化成代理 IP。
//
// 两条分发路径都必须覆盖:无全局中间件走 dispatchRaw → writeMissRaw(裸 w),有全局中间件
// 走 dispatchChained → writeMiss(池化 Response)。缺陷在两个函数里各存在一份,只测一条
// 路径会漏掉另一条,故本文件所有用例都对 withGlobalMW ∈ {false,true} 各跑一遍。
// ===========================================================================

// missErrStatus 是一个自带状态码的业务错误,用来验证 miss handler 返回的 error 确实流经
// 统一错误链(而不是被粗暴地一律当 500)。
type missErrStatus struct{ status int }

func (e missErrStatus) Error() string   { return "miss handler failed" }
func (e missErrStatus) HTTPStatus() int { return e.status }

// missServer 构造一个带 /only-post 路由的 Server:GET /nope 触发 404,GET /only-post
// 触发 405(Allow: POST)。withGlobalMW 决定走 dispatchChained 还是 dispatchRaw。
func missServer(t *testing.T, withGlobalMW bool, opts ...Option) *Server {
	t.Helper()
	s := New(opts...)
	if withGlobalMW {
		// 一个透传中间件即可让 buildChain 产出非 nil globalChain,把分发切到链路径。
		s.Use(func(next Handler) Handler { return next })
	}
	if err := s.RawHandle(http.MethodPost, "/only-post", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatalf("register /only-post failed: %v", err)
	}
	return s
}

// TestMiss_CustomHandlerErrorReachesErrorChain 锁定缺陷 1:自定义 404/405 handler 返回
// error 且未写响应时,客户端必须收到该 error 对应的状态码,而不是隐式 200 空体。
func TestMiss_CustomHandlerErrorReachesErrorChain(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"plain error maps to 500", errors.New("boom"), http.StatusInternalServerError, "internal"},
		{"StatusCoder is honored", missErrStatus{status: http.StatusServiceUnavailable}, http.StatusServiceUnavailable, "unavailable"},
		{"sentinel maps by classification", ErrInvalidInput, http.StatusBadRequest, "invalid_input"},
	}
	for _, c := range cases {
		for _, which := range []string{"404", "405"} {
			for _, withGlobalMW := range []bool{false, true} {
				t.Run(c.name+"/"+which+"/globalMW="+boolName(withGlobalMW), func(t *testing.T) {
					failing := func(_ context.Context, _ *Request, _ *Response) error { return c.err }
					var hookStatus int
					var hookErr error
					opts := []Option{WithErrorHook(func(_ *http.Request, status int, err error) {
						hookStatus, hookErr = status, err
					})}
					url := "/nope"
					if which == "404" {
						opts = append(opts, WithNotFoundHandler(failing))
					} else {
						opts = append(opts, WithMethodNotAllowedHandler(failing))
						url = "/only-post"
					}
					s := missServer(t, withGlobalMW, opts...)

					rec := httptest.NewRecorder()
					s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))

					if rec.Code != c.wantStatus {
						t.Errorf("client status = %d, want %d (a returned error must not become an implicit 200)",
							rec.Code, c.wantStatus)
					}
					if body := rec.Body.String(); !strings.Contains(body, `"code":"`+c.wantCode+`"`) {
						t.Errorf("body = %q, want the unified error body carrying code %q", body, c.wantCode)
					}
					// 错误必须同时到达观测钩子,否则运维侧看不到 miss handler 在报错。
					if hookStatus != c.wantStatus || hookErr == nil {
						t.Errorf("onError hook got (status=%d, err=%v), want (status=%d, non-nil err)",
							hookStatus, hookErr, c.wantStatus)
					}
					// 405 的 Allow 头由框架先写,handler 报错后仍应保留。
					if which == "405" {
						if allow := rec.Header().Get("Allow"); allow != "POST" {
							t.Errorf("Allow = %q, want POST even when the handler errors", allow)
						}
					}
				})
			}
		}
	}
}

// TestMiss_CustomHandlerWrittenResponseNotRewritten 锁定修复的另一半:handler 已写响应
// 后【再】返回 error 时,writeError 依 Written() 只记录不改写——不能双写 header,也不能
// 把用户已发出的状态码篡改成 500。
func TestMiss_CustomHandlerWrittenResponseNotRewritten(t *testing.T) {
	for _, withGlobalMW := range []bool{false, true} {
		t.Run("globalMW="+boolName(withGlobalMW), func(t *testing.T) {
			var hookStatus int
			var hookErr error
			s := missServer(t, withGlobalMW,
				WithErrorHook(func(_ *http.Request, status int, err error) {
					hookStatus, hookErr = status, err
				}),
				WithNotFoundHandler(func(_ context.Context, _ *Request, resp *Response) error {
					resp.WriteHeader(http.StatusGone)
					_, _ = resp.WriteString("gone-and-then-error")
					return errors.New("late failure")
				}),
			)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

			if rec.Code != http.StatusGone {
				t.Errorf("status = %d, want 410 (a committed response must not be rewritten)", rec.Code)
			}
			if body := rec.Body.String(); body != "gone-and-then-error" {
				t.Errorf("body = %q, want the handler's own bytes with nothing appended", body)
			}
			// 已提交也要被观测到:错误本身必须原样送达钩子,否则根因丢失。
			if hookErr == nil || hookErr.Error() != "late failure" {
				t.Errorf("onError err = %v, want the handler's own error", hookErr)
			}
			// 上报的状态码是【客户端实际收到的】410,而非错误分类出的 500:响应头已落地,
			// 500 只是"本应回什么"。上报分类值会让告警看到一个客户端从未收到的状态码
			// (M12 的另一半),与 Logger/metrics 一贯采用 resp.Status() 的口径也相互矛盾。
			if hookStatus != http.StatusGone {
				t.Errorf("onError status = %d, want 410 (the status the client actually received)", hookStatus)
			}
		})
	}
}

// TestMiss_CustomHandlerClientIPHonorsTrustedProxies 锁定缺陷 2:自定义 404/405 handler
// 内的 ClientIP() 必须与命中路由时【结果一致】,即遵循 WithTrustedProxies 解析转发头。
func TestMiss_CustomHandlerClientIPHonorsTrustedProxies(t *testing.T) {
	const (
		proxyPeer = "10.1.2.3:41234" // 落在可信网段内
		realIP    = "1.2.3.4"        // XFF 里的真实客户端
	)
	for _, withGlobalMW := range []bool{false, true} {
		t.Run("globalMW="+boolName(withGlobalMW), func(t *testing.T) {
			var hitIP, notFoundIP, notAllowedIP string
			s := missServer(t, withGlobalMW,
				WithTrustedProxies("10.0.0.0/8"),
				WithNotFoundHandler(func(_ context.Context, req *Request, resp *Response) error {
					notFoundIP = req.ClientIP()
					resp.WriteHeader(http.StatusNotFound)
					return nil
				}),
				WithMethodNotAllowedHandler(func(_ context.Context, req *Request, resp *Response) error {
					notAllowedIP = req.ClientIP()
					resp.WriteHeader(http.StatusMethodNotAllowed)
					return nil
				}),
			)
			if err := s.RawHandle(http.MethodGet, "/hit", func(_ context.Context, req *Request, resp *Response) error {
				hitIP = req.ClientIP()
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			send := func(method, url string) {
				r := httptest.NewRequest(method, url, nil)
				r.RemoteAddr = proxyPeer
				r.Header.Set("X-Forwarded-For", realIP)
				s.ServeHTTP(httptest.NewRecorder(), r)
			}
			send(http.MethodGet, "/hit")          // 命中:基准值
			send(http.MethodGet, "/missing")      // 404 handler
			send(http.MethodDelete, "/only-post") // 405 handler

			if hitIP != realIP {
				t.Fatalf("hit ClientIP = %q, want %q — the baseline itself is broken", hitIP, realIP)
			}
			if notFoundIP != hitIP {
				t.Errorf("404 handler ClientIP = %q, want %q (must match the hit path, i.e. honor WithTrustedProxies)",
					notFoundIP, hitIP)
			}
			if notAllowedIP != hitIP {
				t.Errorf("405 handler ClientIP = %q, want %q (must match the hit path)", notAllowedIP, hitIP)
			}
		})
	}
}

// TestMiss_CustomHandlerUntrustedPeerStillDirectIP 反向锁定:owner 就位不等于盲信转发头。
// 直连对端【不】在可信网段内时,miss handler 的 ClientIP() 仍须返回直连 IP。
func TestMiss_CustomHandlerUntrustedPeerStillDirectIP(t *testing.T) {
	for _, withGlobalMW := range []bool{false, true} {
		t.Run("globalMW="+boolName(withGlobalMW), func(t *testing.T) {
			var gotIP string
			s := missServer(t, withGlobalMW,
				WithTrustedProxies("10.0.0.0/8"),
				WithNotFoundHandler(func(_ context.Context, req *Request, resp *Response) error {
					gotIP = req.ClientIP()
					resp.WriteHeader(http.StatusNotFound)
					return nil
				}),
			)
			r := httptest.NewRequest(http.MethodGet, "/nope", nil)
			r.RemoteAddr = "203.0.113.9:5555" // 公网直连,不可信
			r.Header.Set("X-Forwarded-For", "6.6.6.6")
			s.ServeHTTP(httptest.NewRecorder(), r)

			if gotIP != "203.0.113.9" {
				t.Errorf("ClientIP = %q, want 203.0.113.9 (an untrusted peer's forwarded header must be ignored)", gotIP)
			}
		})
	}
}

// TestMiss_DefaultBehaviorUnchanged 锁定未配置自定义 handler 时的默认 miss 行为不变:
// 404/405 的状态码、Allow 头、Content-Type 与统一 JSON 错误体都保持原样。
func TestMiss_DefaultBehaviorUnchanged(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		url        string
		wantStatus int
		wantAllow  string
		wantBody   string
	}{
		{"404 unified body", http.MethodGet, "/nope", http.StatusNotFound, "",
			`{"error":{"code":"not_found","message":"Not Found"}}`},
		{"405 unified body with Allow", http.MethodGet, "/only-post", http.StatusMethodNotAllowed, "POST",
			`{"error":{"code":"method_not_allowed","message":"Method Not Allowed"}}`},
	}
	for _, c := range cases {
		for _, withGlobalMW := range []bool{false, true} {
			t.Run(c.name+"/globalMW="+boolName(withGlobalMW), func(t *testing.T) {
				var hookStatus int
				s := missServer(t, withGlobalMW,
					WithErrorHook(func(_ *http.Request, status int, _ error) { hookStatus = status }))
				rec := httptest.NewRecorder()
				s.ServeHTTP(rec, httptest.NewRequest(c.method, c.url, nil))

				if rec.Code != c.wantStatus {
					t.Errorf("status = %d, want %d", rec.Code, c.wantStatus)
				}
				if allow := rec.Header().Get("Allow"); allow != c.wantAllow {
					t.Errorf("Allow = %q, want %q", allow, c.wantAllow)
				}
				if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
					t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
				}
				if body := rec.Body.String(); body != c.wantBody {
					t.Errorf("body = %q, want %q", body, c.wantBody)
				}
				if hookStatus != c.wantStatus {
					t.Errorf("onError status = %d, want %d", hookStatus, c.wantStatus)
				}
			})
		}
	}
}

// TestMiss_TSRRedirectUnaffected 锁定 TSR 优先级不变:仅差尾斜杠时先发 301,
// 不进入 404/405 分支(故自定义 handler 与错误链都不参与)。
func TestMiss_TSRRedirectUnaffected(t *testing.T) {
	for _, withGlobalMW := range []bool{false, true} {
		t.Run("globalMW="+boolName(withGlobalMW), func(t *testing.T) {
			called := false
			s := missServer(t, withGlobalMW,
				WithNotFoundHandler(func(_ context.Context, _ *Request, resp *Response) error {
					called = true
					resp.WriteHeader(http.StatusNotFound)
					return nil
				}),
			)
			if err := s.RawHandle(http.MethodGet, "/tsr", func(_ context.Context, _ *Request, resp *Response) error {
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tsr/", nil))

			if rec.Code != http.StatusMovedPermanently {
				t.Errorf("status = %d, want 301 (TSR must win over the 404 branch)", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != "/tsr" {
				t.Errorf("Location = %q, want /tsr", loc)
			}
			if called {
				t.Error("the custom 404 handler must not run when TSR handles the request")
			}
		})
	}
}

func boolName(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
