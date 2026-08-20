package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// 链式折叠(chain collapse)等价性测试:matchStaticChild 内的就地循环必须与
// 递归慢路径在所有边界形态下行为一致。
// chain-collapse equivalence tests: the inline loop inside matchStaticChild
// must behave identically to the recursive slow path on every edge shape.

func chainProbe(t *testing.T, method, path string, handlers ...func(*Server)) (int, string) {
	t.Helper()
	s := New(WithProduces(MIMEJSON))
	for _, h := range handlers {
		h(s)
	}
	s.finalizeRoutes()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	body := rec.Body.String()
	return rec.Code, body
}

func chainEcho(s *Server, method, pattern string) {
	var def *EndpointBuilder
	switch method {
	case http.MethodGet:
		def = Get(pattern)
	case http.MethodPost:
		def = Post(pattern)
	default:
		panic("unsupported method " + method)
	}
	s.MustMount(Handle(def, NoInput(), JSONOutput[map[string]string](),
		func(_ context.Context, _ EmptyInput) (map[string]string, error) {
			return map[string]string{"ok": "1"}, nil
		}))
}

func TestChainCollapseStaticParamAlternation(t *testing.T) {
	code, body := chainProbe(t, http.MethodGet, "/a/v1/b/v2",
		func(s *Server) { chainEcho(s, http.MethodGet, "/a/{p1}/b/{p2}") })
	if code != 200 || !strings.Contains(body, `"ok"`) {
		t.Fatalf("code=%d body=%q", code, body)
	}
}

func TestChainCollapseDeepStaticChain(t *testing.T) {
	code, _ := chainProbe(t, http.MethodGet, "/api/v1/users/lists",
		func(s *Server) { chainEcho(s, http.MethodGet, "/api/v1/users/lists") })
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
}

// 静态链中途分叉:分叉节点必须交回慢路径,两个分支都可达。
// a mid-chain fork must hand back to the slow path with both branches reachable.
func TestChainCollapseForkMidChain(t *testing.T) {
	code1, _ := chainProbe(t, http.MethodGet, "/a/v1/b/x",
		func(s *Server) {
			chainEcho(s, http.MethodGet, "/a/{p1}/b/x")
			chainEcho(s, http.MethodGet, "/a/{p1}/b/y")
		})
	if code1 != 200 {
		t.Fatalf("branch x code=%d", code1)
	}
	code2, _ := chainProbe(t, http.MethodGet, "/a/v1/b/y",
		func(s *Server) {
			chainEcho(s, http.MethodGet, "/a/{p1}/b/x")
			chainEcho(s, http.MethodGet, "/a/{p1}/b/y")
		})
	if code2 != 200 {
		t.Fatalf("branch y code=%d", code2)
	}
}

// 参数与静态并存:静态优先于参数的既有次序保持。
// param and static coexist: static-first ordering is preserved.
func TestChainCollapseStaticBeatsParam(t *testing.T) {
	code, _ := chainProbe(t, http.MethodGet, "/a/lit/b",
		func(s *Server) {
			chainEcho(s, http.MethodGet, "/a/{p1}/b")
			chainEcho(s, http.MethodGet, "/a/lit/b")
		})
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
}

// 段内碎片:共享前缀的静态段(ali/alice)不得被链折叠误判。
// in-segment fragments: shared-prefix statics (ali/alice) must not be
// misjudged by the chain collapse.
func TestChainCollapseInSegmentFragments(t *testing.T) {
	code1, _ := chainProbe(t, http.MethodGet, "/u/ali",
		func(s *Server) {
			chainEcho(s, http.MethodGet, "/u/ali")
			chainEcho(s, http.MethodGet, "/u/alice")
		})
	if code1 != 200 {
		t.Fatalf("ali code=%d", code1)
	}
	code2, _ := chainProbe(t, http.MethodGet, "/u/alice",
		func(s *Server) {
			chainEcho(s, http.MethodGet, "/u/ali")
			chainEcho(s, http.MethodGet, "/u/alice")
		})
	if code2 != 200 {
		t.Fatalf("alice code=%d", code2)
	}
	code3, _ := chainProbe(t, http.MethodGet, "/u/alicia",
		func(s *Server) {
			chainEcho(s, http.MethodGet, "/u/ali")
			chainEcho(s, http.MethodGet, "/u/alice")
		})
	if code3 != 404 {
		t.Fatalf("alicia code=%d", code3)
	}
}

// 转义兜底:%62 编码的静态段必须命中字面 "b" 节点(链不匹配时回退慢路径重放)。
// escape fallback: a %62-encoded static segment must hit the literal "b"
// node (the chain bails into the slow path replay on mismatch).
func TestChainCollapseEscapeFallback(t *testing.T) {
	// httptest.NewRequest 会保留 EscapedPath;使用显式 URL 保证 %62 上送。
	// httptest.NewRequest keeps EscapedPath; build the URL explicitly so %62
	// reaches the mux.
	s := New(WithProduces(MIMEJSON))
	chainEcho(s, http.MethodGet, "/x/{p1}/b")
	s.finalizeRoutes()
	u := &url.URL{Path: "/x/v/", RawPath: "/x/v/%62", Opaque: ""}
	_ = u
	req := httptest.NewRequest(http.MethodGet, "http://test/x/v/%62", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("escaped static code=%d", rec.Code)
	}
}

// 转义参数值:链内消费的参数值必须经 PathUnescape。
// escaped param values: values consumed in-chain must be PathUnescaped.
func TestChainCollapseEscapedParamValue(t *testing.T) {
	s := New(WithProduces(MIMEJSON))
	s.MustMount(Handle(Get("/a/{p1}/b"), PathString("p1"), JSONOutput[map[string]string](),
		func(_ context.Context, p1 string) (map[string]string, error) {
			return map[string]string{"v": p1}, nil
		}))
	s.finalizeRoutes()
	req := httptest.NewRequest(http.MethodGet, "http://test/a/a%2Fb/b", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "a/b") {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

// catch-all 挂在链尾:链必须交回慢路径执行 catch-all 扫描。
// a trailing catch-all must hand back to the slow path for its scan.
func TestChainCollapseTrailingCatchAll(t *testing.T) {
	code, body := chainProbe(t, http.MethodGet, "/files/a/b/c",
		func(s *Server) {
			s.MustMount(Handle(Get("/files/{rest...}"), PathString("rest"), JSONOutput[map[string]string](),
				func(_ context.Context, rest string) (map[string]string, error) {
					return map[string]string{"rest": rest}, nil
				}))
		})
	if code != 200 || !strings.Contains(body, `"a/b/c"`) {
		t.Fatalf("code=%d body=%q", code, body)
	}
}

// 链尾 405:终端 allow 收集必须保留(链交回慢路径后收集)。
// 405 at chain end: terminal allow collection must survive the handoff.
func TestChainCollapseMethodNotAllowed(t *testing.T) {
	s := New(WithProduces(MIMEJSON))
	chainEcho(s, http.MethodPost, "/a/{p1}/b/{p2}")
	s.finalizeRoutes()
	req := httptest.NewRequest(http.MethodGet, "/a/v1/b/v2", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "POST") {
		t.Fatalf("Allow=%q", allow)
	}
}

// 链死路径:参数耗尽/段不匹配必须 404 且不泄漏段记录。
// dead chains: param exhaustion or segment mismatch must 404 without
// leaking segment records.
func TestChainCollapseDeadEnds(t *testing.T) {
	s := New(WithProduces(MIMEJSON))
	chainEcho(s, http.MethodGet, "/a/{p1}/b/{p2}")
	s.finalizeRoutes()
	for _, path := range []string{"/a/v1/b", "/a/v1/c/v2", "/a/v1/b/v2/extra", "/a/v1"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Fatalf("path %q code=%d", path, rec.Code)
		}
	}
}

// 尾斜杠路由:strict 语义在链尾保持。
// trailing-slash routing: strict semantics survive at the chain end.
func TestChainCollapseTrailingSlash(t *testing.T) {
	s := New(WithProduces(MIMEJSON))
	chainEcho(s, http.MethodGet, "/a/{p1}/b/")
	s.finalizeRoutes()
	req := httptest.NewRequest(http.MethodGet, "/a/v1/b/", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("trailing code=%d", rec.Code)
	}
}

// dot 段与空段:链内校验必须与慢路径一致(400)。
// dot and empty segments: in-chain validation must match the slow path (400).
func TestChainCollapseInvalidSegments(t *testing.T) {
	s := New(WithProduces(MIMEJSON))
	chainEcho(s, http.MethodGet, "/a/{p1}/b/{p2}")
	s.finalizeRoutes()
	for _, path := range []string{"/a/../b/v2", "/a//b/v2"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("path %q code=%d", path, rec.Code)
		}
	}
}
