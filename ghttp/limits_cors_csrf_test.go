package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- 限流 --- //

// TestRateLimit_NaNRPSIsPassthrough 锁定 NaN 分支：`RPS <= 0` 对 NaN 恒为 false，于是
// NaN 会穿过校验、把桶容量与补充速率都变成 NaN，`tokens < 1` 永假 → 每个请求都被放行，
// 限流静默失效（比拒绝更危险，因为看不出配置错了）。
// Locks the NaN branch: `RPS <= 0` is false for NaN, so NaN slips past validation and
// makes both bucket capacity and refill NaN, `tokens < 1` is never true, and every
// request is admitted — limiting fails silently, which is worse than failing closed
// because nothing reveals the misconfiguration.
func TestRateLimit_NaNRPSIsPassthrough(t *testing.T) {
	t.Parallel()

	zero := 0.0
	nan := zero / zero // 非常量表达式，避开编译期除零检查 / non-constant to dodge the compile-time check
	mw := RateLimit(RateLimitConfig{RPS: nan, Burst: 10})
	var called int
	h := mw(func(_ context.Context, _ *Request, resp *Response) error {
		called++
		resp.WriteHeader(http.StatusOK)
		return nil
	})
	s := New()
	if err := s.RawHandle("GET", "/x", func(ctx context.Context, req *Request, resp *Response) error {
		return h(ctx, req, resp)
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	for i := 0; i < 30; i++ {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (passthrough)", i, rec.Code)
		}
	}
	if called != 30 {
		t.Fatalf("handler ran %d times, want 30", called)
	}
}

// TestRateLimit_ReapWindowCoversRefill 锁定回收窗口下限：桶在仍持有未用令牌时被回收，
// 下次访问按新 key 重建为满桶，于是每个 idle 周期都能白得一次完整 burst——长期吞吐可达
// burst/idle 而非声称的 RPS。
// Locks the reap-window floor: a bucket reaped while still holding unused tokens is
// rebuilt full on the next visit, granting a fresh whole burst every idle period, so
// long-run throughput becomes burst/idle rather than the claimed RPS.
func TestRateLimit_ReapWindowCoversRefill(t *testing.T) {
	t.Parallel()

	// RPS=1、Burst=1000、IdleTimeout=1s：补满需要 1000s，1s 回收等于形同虚设。
	// RPS=1, Burst=1000, IdleTimeout=1s: refilling takes 1000s, so a 1s reap is a no-op
	// guard that hands out a free burst constantly.
	rl := newRateLimiter(RateLimitConfig{RPS: 1, Burst: 1000, IdleTimeout: time.Second}, 1000, time.Second, func(*Request) string { return "k" })
	if min := time.Duration(float64(1000) / 1 * float64(time.Second)); rl.idle < min {
		t.Fatalf("idle = %v, want >= %v (the empty-to-full refill time)", rl.idle, min)
	}
}

// TestRateLimit_UnidentifiablePeerIsNotExempt 锁定默认维度的豁免漏洞：空 key 被定义为
// "调用方显式豁免"，若默认 KeyFunc 在取不到客户端地址时返回空串，这类流量就整体跳过限流
// ——最需要管束的 unidentified 流量反而不受管。取不到地址时（unix socket、被剥掉的
// RemoteAddr）落到共享兜底桶，仍受同一速率约束。
// Locks the exemption hole in the default dimension: an empty key means "explicitly
// exempt", so if the default KeyFunc returns empty when no client address resolves, that
// traffic skips limiting entirely — the unidentified flow, the one most in need. When no
// address resolves (unix socket, stripped RemoteAddr) requests share one fallback bucket
// and stay under the same rate.
func TestRateLimit_UnidentifiablePeerIsNotExempt(t *testing.T) {
	t.Parallel()

	s := rlServer(t, RateLimit(RateLimitConfig{RPS: 0.001, Burst: 1}), "/x")
	hit := func() int {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.RemoteAddr = "" // 完全取不到对端地址 / no peer address at all
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec.Code
	}
	if got := hit(); got != http.StatusOK {
		t.Fatalf("first: status = %d, want 200", got)
	}
	if got := hit(); got != http.StatusTooManyRequests {
		t.Fatalf("second: status = %d, want 429 (an unidentifiable peer must not be exempt)", got)
	}
}

// TestRateLimit_CustomEmptyKeyStillExempts 是反向保护：显式豁免语义不得被破坏。
// TestRateLimit_CustomEmptyKeyStillExempts is the reverse guard: an explicit exemption
// must still work.
func TestRateLimit_CustomEmptyKeyStillExempts(t *testing.T) {
	t.Parallel()

	rl := newRateLimiter(RateLimitConfig{RPS: 1, Burst: 1}, 1, defaultRateLimitIdle, func(*Request) string { return "" })
	if got := rl.keyFunc(&Request{}); got != "" {
		t.Fatalf("custom KeyFunc returning \"\" must stay, got %q", got)
	}
}

// --- CORS --- //

// TestCORS_OriginMatchIsCaseInsensitive 锁定来源大小写不敏感：Origin 只含
// scheme://host[:port]，二者按 URL 语义都不区分大小写；与 CSRF 已有的归一保持同一口径，
// 否则两道防线对"是否同源"的判断会分叉。
// Locks case-insensitive origin matching: an Origin is only scheme://host[:port], both
// case-insensitive per URL semantics, and must share CSRF's convention — otherwise the
// two defenses disagree about what counts as the same origin.
func TestCORS_OriginMatchIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	s := New()
	s.Use(CORS(CORSConfig{AllowOrigins: []string{"https://App.Example.com"}}))
	if err := s.RawHandle("GET", "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Header().Set("Content-Type", "text/plain")
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.Write([]byte("ok"))
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Origin", "https://app.EXAMPLE.com")
	s.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.EXAMPLE.com" {
		t.Fatalf("ACAO = %q, want the echoed origin", got)
	}
}

// TestCORS_ExplicitListWinsOverWildcard 锁定优先级：配了 `["*", "https://a.example"]`
// 时，a.example 必须拿到回显的自身 Origin，而不是被通配分支抢先命中写成 `*`。
// Locks precedence: with ["*", "https://a.example"], that origin must get its own value
// echoed rather than being captured by the wildcard branch as `*`.
func TestCORS_ExplicitListWinsOverWildcard(t *testing.T) {
	t.Parallel()

	s := New()
	s.Use(CORS(CORSConfig{AllowOrigins: []string{"*", "https://a.example"}}))
	if err := s.RawHandle("GET", "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x", nil)
	r.Header.Set("Origin", "https://a.example")
	s.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://a.example" {
		t.Fatalf("ACAO = %q, want the explicit origin to win over the wildcard", got)
	}
}

// TestCORS_VaryIsIdempotent 锁定 Vary 幂等：ACAO 依赖 Origin 就必须声明 Vary，但重复
// 追加会让下游缓存键解析做无谓工作，并读成"存在多条策略"。
// Locks Vary idempotency: ACAO depends on Origin so Vary is mandatory, but duplicates
// make cache-key parsing do pointless work and read as several policies.
func TestCORS_VaryIsIdempotent(t *testing.T) {
	t.Parallel()

	c := newCORS(CORSConfig{AllowOrigins: []string{"https://a.example"}})
	dst := http.Header{}
	dst.Add("Vary", "Accept-Encoding")
	req := http.Header{}
	req.Set("Origin", "https://a.example")
	// 同一响应上应用两次（链上复用/与其他中间件叠加）。
	// Apply twice on one response (chain reuse or stacking with other middleware).
	c.apply(dst, req, "GET")
	c.apply(dst, req, "GET")

	var origins int
	for _, v := range dst.Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Origin") {
				origins++
			}
		}
	}
	if origins != 1 {
		t.Fatalf("Vary declares Origin %d times, want exactly 1 (%v)", origins, dst.Values("Vary"))
	}
}

// --- CSRF --- //

// TestCSRF_TrustedOriginsOnlyDropsHostFallback 验证收紧开关：开启后"Origin 的 host 等于
// 请求 Host"不再放行，Host 可被客户端影响的部署（通配虚拟主机、DNS 重绑定）里这才是运维
// 想要的硬白名单。
// Verifies the tightening switch: with it on, "Origin host equals request Host" no
// longer admits the request, which is the hard whitelist operators want where Host is
// client-influenced (wildcard vhosts, DNS rebinding).
func TestCSRF_TrustedOriginsOnlyDropsHostFallback(t *testing.T) {
	t.Parallel()

	trusted := map[string]bool{"https://partner.io": true}

	r := httptest.NewRequest("POST", "/x", nil)
	r.Header.Set("Origin", "http://example.com") // 与 req.Host 同 / equals req.Host
	if err := checkCSRFOrigin(&Request{Request: r}, trusted, false); err != nil {
		t.Fatalf("default mode must keep the same-origin fallback: %v", err)
	}
	if err := checkCSRFOrigin(&Request{Request: r}, trusted, true); err == nil {
		t.Fatal("TrustedOriginsOnly must refuse the Host-reflection fallback")
	}
	// 显式列出的来源在两种模式下都放行。
	// An explicitly listed origin passes in both modes.
	r2 := httptest.NewRequest("POST", "/x", nil)
	r2.Header.Set("Origin", "https://partner.io")
	if err := checkCSRFOrigin(&Request{Request: r2}, trusted, true); err != nil {
		t.Fatalf("a listed origin must pass even in strict mode: %v", err)
	}
}

// TestCSRF_SkipsBodyParseForOversizedForm 锁定预检前的体量门：为一个注定缺 token 的请求
// 解析超大 multipart 体，会先吃掉内存与临时文件（ParseMultipartForm 的 32 MiB 只管驻留
// 内存，不是总大小），之后才拒绝——攻击者可用注定失败的请求消耗资源。
// Locks the declared-length gate: parsing an oversized multipart body just to look for a
// token spends memory and temp files first (ParseMultipartForm's 32 MiB caps only the
// in-memory portion, not the total) and only then rejects, letting an attacker burn
// resources on requests that were always going to fail.
func TestCSRF_SkipsBodyParseForOversizedForm(t *testing.T) {
	t.Parallel()

	s := New()
	var parsed bool
	// 用一个会暴露"体是否被读过"的 handler：CSRF 在 token 缺失时应当拒绝且不消费体。
	// Use a handler that reveals whether the body was read: CSRF must reject on a
	// missing token without consuming the body.
	if err := PostBody(s, "/up", FormBody[map[string]string](), JSON[string](),
		func(_ context.Context, _ map[string]string) (string, error) { parsed = true; return "ok", nil }); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.Use(CSRF(CSRFConfig{}))

	body := strings.Repeat("x", maxCSRFPreAuthBodyBytes+4096)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/up", strings.NewReader(body))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=B")
	r.ContentLength = int64(len(body))
	s.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a token-less state-changing request", rec.Code)
	}
	if parsed {
		t.Fatal("the body was parsed even though the token check should have failed first")
	}
}
