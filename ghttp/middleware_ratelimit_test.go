package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// rlOKHandler 是限流测试统一使用的终端:显式写 200,便于用状态码区分"放行"与"被拒"。
// rlOKHandler is the shared terminal for rate-limit tests: it writes 200 explicitly
// so the status code alone distinguishes "admitted" from "rejected".
func rlOKHandler(ctx context.Context, req *Request, resp *Response) error {
	resp.WriteHeader(http.StatusOK)
	return nil
}

// rlServer 构造一个挂了 mw 的 Server,并把 paths 全部注册为 rlOKHandler。
// rlServer builds a Server with mw mounted and every path registered to rlOKHandler.
func rlServer(t *testing.T, mw Middleware, paths ...string) *Server {
	t.Helper()
	s := New()
	s.Use(mw)
	for _, p := range paths {
		if err := s.RawHandle(http.MethodGet, p, rlOKHandler); err != nil {
			t.Fatalf("RawHandle(%q): %v", p, err)
		}
	}
	return s
}

// rlHit 发一次请求。remote 为空时沿用 httptest 默认 RemoteAddr;hdr 用于自定义 KeyFunc
// 按头限流的场景。
// rlHit issues one request. An empty remote keeps httptest's default RemoteAddr;
// hdr serves the custom-KeyFunc-by-header cases.
func rlHit(s *Server, path, remote string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if remote != "" {
		r.RemoteAddr = remote
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

// rlNewLimiter 直接构造一个 rateLimiter,用于绕过中间件层单测 allow 的补充/回收逻辑。
// 它复刻 RateLimit 的初始化(桶表必须预建,否则 allow 会向 nil map 写入而 panic)。
// rlNewLimiter builds a rateLimiter directly to unit-test allow's refill/reap logic
// below the middleware layer. It mirrors RateLimit's initialization (the bucket maps
// must be pre-created or allow would panic writing to a nil map).
func rlNewLimiter(rps float64, burst int, idle time.Duration) *rateLimiter {
	if idle <= 0 {
		idle = defaultRateLimitIdle
	}
	rl := &rateLimiter{
		cfg:   RateLimitConfig{RPS: rps, Burst: burst},
		burst: float64(burst),
		idle:  idle,
		// perShardMax 必须一并复刻:留零会让上限检查挡住每一个新桶。
		// perShardMax must be mirrored too: leaving it zero makes the cap reject every
		// new bucket.
		perShardMax: defaultRateLimitMaxKeys / rateLimitShards,
	}
	for i := range rl.shards {
		rl.shards[i].buckets = make(map[string]*tokenBucket)
	}
	return rl
}

// rlBucketCount 在持有分片锁的前提下读取 key 所属分片的桶数量,避免与 allow 竞争。
// rlBucketCount reads the bucket count of key's shard while holding the shard lock,
// so it never races allow.
func rlBucketCount(rl *rateLimiter, key string) int {
	sh := &rl.shards[shardIndex(key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return len(sh.buckets)
}

// rlHasBucket 在持有分片锁的前提下报告 key 的桶是否仍在表中。
// rlHasBucket reports whether key's bucket is still in the map, under the shard lock.
func rlHasBucket(rl *rateLimiter, key string) bool {
	sh := &rl.shards[shardIndex(key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	_, ok := sh.buckets[key]
	return ok
}

// --- 中间件层:突发、拒绝与响应形态 -------------------------------------------
// --- Middleware layer: burst, rejection, and response shape -------------------

// TestRateLimit_BurstThenReject 验证 Burst=3 时前 3 个请求放行、第 4 个以 429 拒绝。
// RPS 取极小值使测试期间几乎不补充令牌,从而让"第 4 个必拒"成为确定结果而非时序赌博。
func TestRateLimit_BurstThenReject(t *testing.T) {
	s := rlServer(t, RateLimit(RateLimitConfig{RPS: 0.001, Burst: 3}), "/x")

	for i := 1; i <= 3; i++ {
		if rec := rlHit(s, "/x", "203.0.113.10:1111", nil); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status=%d, want 200 (within burst)", i, rec.Code)
		}
	}

	rec := rlHit(s, "/x", "203.0.113.10:1111", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 4: status=%d, want 429", rec.Code)
	}
	// Retry-After 必须存在且非空:限流响应若不告知重试间隔,客户端只能盲目重试。
	if ra := rec.Header().Get(HeaderRetryAfter); ra == "" {
		t.Error("rejection must carry a non-empty Retry-After header")
	}
	// 错误体经统一错误链渲染,code 必须是稳定的机器可读串,供客户端区分限流与其它 4xx。
	body := rec.Body.String()
	if !strings.Contains(body, `"code":"rate_limit_exceeded"`) {
		t.Errorf("body=%s, want error code rate_limit_exceeded", body)
	}
	if !strings.Contains(body, `"message":`) {
		t.Errorf("body=%s, want a message field", body)
	}
	// 默认脱敏:不得把内部错误细节(key、速率)回传给客户端。
	if strings.Contains(body, "203.0.113.10") {
		t.Errorf("body leaked the limiting key: %s", body)
	}
}

// TestRateLimit_RetryAfterComposition 验证 Retry-After 是"补满一个令牌所需秒数"且至少
// 为 1——整秒语义下 0 会让客户端立即重试,退避失效。
func TestRateLimit_RetryAfterComposition(t *testing.T) {
	tests := []struct {
		name string
		rps  float64
		want string
	}{
		// RPS 很高时理论间隔远小于 1s,但必须夹到 1(不能是 "0")。
		{name: "high rps clamps to 1", rps: 100, want: "1"},
		{name: "one per second", rps: 1, want: "1"},
		// 每 10 秒一个令牌 → 10 秒后才可能有令牌。
		{name: "slow refill", rps: 0.1, want: "10"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := rlServer(t, RateLimit(RateLimitConfig{RPS: tc.rps, Burst: 1}), "/x")
			if rec := rlHit(s, "/x", "203.0.113.11:1", nil); rec.Code != http.StatusOK {
				t.Fatalf("first request status=%d, want 200", rec.Code)
			}
			rec := rlHit(s, "/x", "203.0.113.11:1", nil)
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("second request status=%d, want 429", rec.Code)
			}
			if got := rec.Header().Get(HeaderRetryAfter); got != tc.want {
				t.Errorf("Retry-After=%q, want %q", got, tc.want)
			}
		})
	}
}

// TestRateLimit_ZeroRPSPassesThrough 验证 RPS<=0 时透传:配置失误不应让全站不可用,
// 因此选择"不限流"而非"拒绝一切"。
func TestRateLimit_ZeroRPSPassesThrough(t *testing.T) {
	tests := []struct {
		name string
		cfg  RateLimitConfig
	}{
		{name: "zero rps", cfg: RateLimitConfig{RPS: 0}},
		{name: "negative rps", cfg: RateLimitConfig{RPS: -1, Burst: 1}},
		// Burst=1 且 RPS<=0 时仍须透传:RPS 是唯一的开关判据。
		{name: "zero rps with burst", cfg: RateLimitConfig{RPS: 0, Burst: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := rlServer(t, RateLimit(tc.cfg), "/x")
			for i := range 20 {
				if rec := rlHit(s, "/x", "203.0.113.12:1", nil); rec.Code != http.StatusOK {
					t.Fatalf("request %d: status=%d, want 200 (RPS<=0 must not limit)", i, rec.Code)
				}
			}
		})
	}
}

// TestRateLimit_EmptyKeyExempts 验证 KeyFunc 返回空串的请求完全豁免:这是放行内部探针
// 的机制,豁免请求既不消耗令牌也不建桶。
func TestRateLimit_EmptyKeyExempts(t *testing.T) {
	s := rlServer(t, RateLimit(RateLimitConfig{
		RPS:     0.001,
		Burst:   1,
		KeyFunc: func(req *Request) string { return "" },
	}), "/probe")

	for i := range 50 {
		if rec := rlHit(s, "/probe", "203.0.113.13:1", nil); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status=%d, want 200 (empty key exempts)", i, rec.Code)
		}
	}
}

// TestRateLimit_CustomKeyFunc 验证自定义 KeyFunc 生效:按请求头限流时,不同头值各自独立
// 计数,互不影响。
func TestRateLimit_CustomKeyFunc(t *testing.T) {
	s := rlServer(t, RateLimit(RateLimitConfig{
		RPS:     0.001,
		Burst:   1,
		KeyFunc: func(req *Request) string { return req.Header.Get("X-Tenant") },
	}), "/x")

	// 租户 A 用掉唯一令牌。
	if rec := rlHit(s, "/x", "", map[string]string{"X-Tenant": "a"}); rec.Code != http.StatusOK {
		t.Fatalf("tenant a first: status=%d, want 200", rec.Code)
	}
	if rec := rlHit(s, "/x", "", map[string]string{"X-Tenant": "a"}); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("tenant a second: status=%d, want 429", rec.Code)
	}
	// 租户 B 拥有自己的桶,不受 A 影响——否则限流维度就形同全局。
	if rec := rlHit(s, "/x", "", map[string]string{"X-Tenant": "b"}); rec.Code != http.StatusOK {
		t.Errorf("tenant b: status=%d, want 200 (keys are independent)", rec.Code)
	}
	// 同一 RemoteAddr 不再是限流维度:自定义 KeyFunc 完全取代了默认的 ClientIP。
	if rec := rlHit(s, "/x", "", map[string]string{"X-Tenant": "c"}); rec.Code != http.StatusOK {
		t.Errorf("tenant c: status=%d, want 200 (client IP must not be the key)", rec.Code)
	}
}

// TestRateLimit_DefaultKeyIsClientIP 验证默认 KeyFunc 按客户端 IP 限流:不同 IP 各自独立。
func TestRateLimit_DefaultKeyIsClientIP(t *testing.T) {
	s := rlServer(t, RateLimit(RateLimitConfig{RPS: 0.001, Burst: 1}), "/x")

	if rec := rlHit(s, "/x", "198.51.100.1:5000", nil); rec.Code != http.StatusOK {
		t.Fatalf("ip1 first: status=%d, want 200", rec.Code)
	}
	// 同 IP 换端口仍是同一客户端:RemoteIP 去端口后作 key。
	if rec := rlHit(s, "/x", "198.51.100.1:6000", nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("ip1 second (different port): status=%d, want 429 (port is not part of the key)", rec.Code)
	}
	if rec := rlHit(s, "/x", "198.51.100.2:5000", nil); rec.Code != http.StatusOK {
		t.Errorf("ip2: status=%d, want 200 (per-IP isolation)", rec.Code)
	}
}

// TestRateLimit_OnLimitedHook 验证 OnLimited 仅在拒绝时触发,且拿到被限的 key。
// 放行的请求不得触发钩子,否则打点会把正常流量计成限流。
func TestRateLimit_OnLimitedHook(t *testing.T) {
	var mu sync.Mutex
	var gotKeys []string
	var gotPaths []string
	s := rlServer(t, RateLimit(RateLimitConfig{
		RPS:   0.001,
		Burst: 2,
		OnLimited: func(req *Request, key string) {
			mu.Lock()
			defer mu.Unlock()
			gotKeys = append(gotKeys, key)
			gotPaths = append(gotPaths, req.URL.Path)
		},
	}), "/x")

	// 前 2 个在 Burst 内放行,钩子不应触发。
	rlHit(s, "/x", "203.0.113.20:1", nil)
	rlHit(s, "/x", "203.0.113.20:1", nil)
	mu.Lock()
	n := len(gotKeys)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("OnLimited fired %d times for admitted requests, want 0", n)
	}

	// 第 3、4 个被拒,钩子各触发一次。
	rlHit(s, "/x", "203.0.113.20:1", nil)
	rlHit(s, "/x", "203.0.113.20:1", nil)

	mu.Lock()
	defer mu.Unlock()
	if len(gotKeys) != 2 {
		t.Fatalf("OnLimited fired %d times, want exactly 2 (once per rejection)", len(gotKeys))
	}
	for i, k := range gotKeys {
		if k != "203.0.113.20" {
			t.Errorf("gotKeys[%d]=%q, want the client IP key", i, k)
		}
		if gotPaths[i] != "/x" {
			t.Errorf("gotPaths[%d]=%q, want /x (hook receives the live request)", i, gotPaths[i])
		}
	}
}

// TestRateLimit_BurstDefaultsFromRPS 验证 Burst<=0 时取 max(1, ceil(RPS)):向上取整保证
// 小数 RPS 也至少允许 1 个突发,否则桶容量为 0 会拒绝一切。
func TestRateLimit_BurstDefaultsFromRPS(t *testing.T) {
	tests := []struct {
		name      string
		rps       float64
		burst     int
		wantFirst int // 期望连续放行的请求数 / expected consecutive admissions
	}{
		{name: "fractional rps yields burst 1", rps: 0.5, burst: 0, wantFirst: 1},
		{name: "integral rps yields that burst", rps: 3, burst: 0, wantFirst: 3},
		{name: "ceil of fractional", rps: 2.5, burst: -1, wantFirst: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 用 allow + 固定时钟判定容量,避免真实时间补充令牌干扰计数。
			mw := RateLimit(RateLimitConfig{RPS: tc.rps, Burst: tc.burst})
			h := mw(rlOKHandler)
			admitted := 0
			for range tc.wantFirst + 3 {
				rec := httptest.NewRecorder()
				req := &Request{Request: httptest.NewRequest(http.MethodGet, "/x", nil)}
				req.RemoteAddr = "203.0.113.30:1"
				if err := h(context.Background(), req, &Response{ResponseWriter: rec}); err == nil {
					admitted++
				}
			}
			if admitted != tc.wantFirst {
				t.Errorf("admitted=%d, want %d (burst derived from RPS)", admitted, tc.wantFirst)
			}
		})
	}
}

// TestRateLimit_HugeRPSDoesNotStarve 验证 max(1, ...) 的下界保护:RPS 大到 int(RPS) 溢出
// 为负数时,若不夹到 1,桶容量会变成负值而拒绝【一切】请求——把"限流"配成了"熔断"。
func TestRateLimit_HugeRPSDoesNotStarve(t *testing.T) {
	// 1e19 超过 int64 上限,int(RPS) 的行为是实现定义的负值。
	tests := []struct {
		name string
		rps  float64
	}{
		{name: "beyond int64 range", rps: 1e19},
		{name: "astronomically large", rps: 1e300},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := rlServer(t, RateLimit(RateLimitConfig{RPS: tc.rps}), "/x")
			for i := range 20 {
				rec := rlHit(s, "/x", "203.0.113.200:1", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("request %d: status=%d, want 200 (an overflowed burst must clamp to >=1, never reject everything)", i, rec.Code)
				}
			}
		})
	}
}

// --- rateLimiter.allow:注入时钟直接验证补充与上限 ----------------------------
// --- rateLimiter.allow: injected clock verifies refill and the cap -------------

// TestRateLimit_RefillOverTime 验证令牌按经过时间补充。全程注入合成时钟而不 sleep:
// 依赖真实时间的限流测试必然又慢又不稳定。
func TestRateLimit_RefillOverTime(t *testing.T) {
	rl := rlNewLimiter(1, 2, time.Hour) // 1 token/s,容量 2;idle 放大以隔离清理逻辑
	base := time.Unix(1700000000, 0)
	const key = "k"

	// 排空:新 key 首次放行(桶初始满),再取一次用尽第 2 个令牌。
	if !rl.allow(key, base) {
		t.Fatal("take 1 should be admitted (a new bucket starts full)")
	}
	if !rl.allow(key, base) {
		t.Fatal("take 2 should be admitted (burst is 2)")
	}
	if rl.allow(key, base) {
		t.Fatal("take 3 at the same instant must be rejected (bucket drained)")
	}

	// 时间不前进则永远无令牌:补充完全由 now 驱动,不存在隐式时钟。
	if rl.allow(key, base) {
		t.Error("no time elapsed must not refill any token")
	}

	// 不足一个令牌的时间片:0.5s × 1 token/s = 0.5 < 1,仍应拒绝。
	if rl.allow(key, base.Add(500*time.Millisecond)) {
		t.Error("0.5s at 1 token/s yields 0.5 tokens, still below one whole token")
	}

	// 累计满 1 个令牌后放行(上一次调用已把 last 推到 +0.5s,再走 0.5s 即补满 1 个)。
	if !rl.allow(key, base.Add(time.Second)) {
		t.Error("a full second must refill one token")
	}
}

// TestRateLimit_TokensCappedAtBurst 验证令牌上限为 Burst:空闲极久也不得攒出超额突发,
// 否则长时间空闲的客户端可一次性打穿下游。
func TestRateLimit_TokensCappedAtBurst(t *testing.T) {
	const burst = 3
	rl := rlNewLimiter(1000, burst, time.Hour) // RPS 极高,放大"未设上限就会溢出"的差异
	base := time.Unix(1700000000, 0)
	const key = "k"

	if !rl.allow(key, base) {
		t.Fatal("first take should be admitted")
	}

	// 推进一年:若无上限,补充量会达到天文数字。
	far := base.Add(365 * 24 * time.Hour)
	admitted := 0
	for range burst + 10 {
		if rl.allow(key, far) {
			admitted++
		}
	}
	if admitted != burst {
		t.Errorf("admitted=%d after a huge idle gap, want exactly %d (tokens capped at Burst)", admitted, burst)
	}
}

// TestRateLimit_PerKeyIsolation 验证桶按 key 隔离:排空 A 不得影响 B。
func TestRateLimit_PerKeyIsolation(t *testing.T) {
	rl := rlNewLimiter(0.001, 2, time.Hour)
	now := time.Unix(1700000000, 0)

	// 排空 key A。
	for i := range 2 {
		if !rl.allow("A", now) {
			t.Fatalf("A take %d should be admitted", i+1)
		}
	}
	if rl.allow("A", now) {
		t.Fatal("A should be drained")
	}

	// B 有独立的桶,完整容量可用。
	for i := range 2 {
		if !rl.allow("B", now) {
			t.Errorf("B take %d rejected, want admitted (buckets are per key)", i+1)
		}
	}
	if rl.allow("B", now) {
		t.Error("B should be drained only after its own Burst is spent")
	}
	// 排空 B 之后 A 仍是空的:两者状态互不回灌。
	if rl.allow("A", now) {
		t.Error("A must stay drained after B's activity")
	}
}

// TestRateLimit_IdleBucketSweep 验证空闲桶被回收:sweep 由后续 allow 触发(无后台
// goroutine),因此陈旧桶在下一次跨过 IdleTimeout 的调用时才消失。
// 关键在于回收必须真的发生——否则以客户端 IP 为 key 会随 IP 空间无界增长内存。
func TestRateLimit_IdleBucketSweep(t *testing.T) {
	const idle = time.Minute
	rl := rlNewLimiter(1, 2, idle)
	base := time.Unix(1700000000, 0)

	// 选两个落在同一分片的 key:sweep 只清理被访问到的那个分片。
	const stale = "stale-key"
	var fresh string
	for _, cand := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z"} {
		if cand != stale && shardIndex(cand) == shardIndex(stale) {
			fresh = cand
			break
		}
	}
	if fresh == "" {
		t.Fatal("no same-shard companion key found; cannot exercise sweep deterministically")
	}

	if !rl.allow(stale, base) {
		t.Fatal("stale key first take should be admitted")
	}
	if !rlHasBucket(rl, stale) {
		t.Fatal("stale key bucket should exist right after allow")
	}

	// 推进到超过 IdleTimeout 后用另一个 key 触发 sweep。
	later := base.Add(idle + time.Second)
	if !rl.allow(fresh, later) {
		t.Fatal("fresh key first take should be admitted")
	}

	if rlHasBucket(rl, stale) {
		t.Errorf("stale bucket survived a sweep past IdleTimeout=%v (memory would grow unbounded)", idle)
	}
	// 刚创建的桶不得被同一次 sweep 误删。
	if !rlHasBucket(rl, fresh) {
		t.Error("the freshly created bucket must not be reaped by the sweep that it triggered")
	}
	if got := rlBucketCount(rl, stale); got != 1 {
		t.Errorf("shard bucket count=%d, want 1 (only the fresh bucket remains)", got)
	}
}

// TestRateLimit_ActiveBucketNotSwept 验证持续活跃的桶不会被回收:回收判据是"最后访问
// 时间"而非"创建时间",否则长连接客户端会被周期性重置配额。
func TestRateLimit_ActiveBucketNotSwept(t *testing.T) {
	const idle = time.Minute
	rl := rlNewLimiter(1000, 5, idle)
	base := time.Unix(1700000000, 0)
	const key = "busy"

	// 以小于 idle 的步长持续访问,跨越数个 idle 周期。
	for i := range 10 {
		now := base.Add(time.Duration(i) * (idle / 2))
		if !rl.allow(key, now) {
			t.Fatalf("step %d rejected unexpectedly (RPS is high enough)", i)
		}
	}
	if !rlHasBucket(rl, key) {
		t.Error("an actively used bucket must not be reaped")
	}
}

// --- RateLimitByRoute -------------------------------------------------------

// TestRateLimitByRoute_IndependentRoutes 验证按路由模板限流:不同路由各有一个桶,
// 排空昂贵端点不应牵连其它端点。
func TestRateLimitByRoute_IndependentRoutes(t *testing.T) {
	s := rlServer(t, RateLimitByRoute(0.001, 1), "/cheap", "/expensive")

	if rec := rlHit(s, "/expensive", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("/expensive first: status=%d, want 200", rec.Code)
	}
	if rec := rlHit(s, "/expensive", "", nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("/expensive second: status=%d, want 429", rec.Code)
	}
	// 换客户端 IP 也仍被拒:key 是路由而非客户端,这正是"保护单个端点"的语义。
	if rec := rlHit(s, "/expensive", "198.51.100.77:1", nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("/expensive from another client: status=%d, want 429 (key is the route, not the client)", rec.Code)
	}
	// 另一条路由的桶未受影响。
	if rec := rlHit(s, "/cheap", "", nil); rec.Code != http.StatusOK {
		t.Errorf("/cheap: status=%d, want 200 (routes are limited independently)", rec.Code)
	}
}

// TestRateLimitByRoute_ParamRouteSharesBucket 验证同一模板的不同具体路径共享一个桶:
// key 是低基数模板(/items/:id)而非原始 path,否则桶表会随 id 基数爆炸。
func TestRateLimitByRoute_ParamRouteSharesBucket(t *testing.T) {
	s := rlServer(t, RateLimitByRoute(0.001, 1), "/items/{id}")

	if rec := rlHit(s, "/items/1", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("/items/1: status=%d, want 200", rec.Code)
	}
	if rec := rlHit(s, "/items/2", "", nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("/items/2: status=%d, want 429 (same route template shares one bucket)", rec.Code)
	}
}

// TestRateLimitByRoute_MissIsExempt 验证未命中路由的请求被豁免:MatchedRoute 为空 →
// key 为空 → 放行,由 404 逻辑自行应答。若不豁免,所有 404 会共享一个桶而互相限流。
func TestRateLimitByRoute_MissIsExempt(t *testing.T) {
	s := rlServer(t, RateLimitByRoute(0.001, 1), "/exists")

	for i := range 10 {
		rec := rlHit(s, "/nope", "", nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("miss %d: status=%d, want 404 (an unmatched request is exempt, not throttled)", i, rec.Code)
		}
		if rec.Header().Get(HeaderRetryAfter) != "" {
			t.Errorf("miss %d should not carry Retry-After", i)
		}
	}
	// 豁免的 404 洪水不得消耗真实路由的令牌。
	if rec := rlHit(s, "/exists", "", nil); rec.Code != http.StatusOK {
		t.Errorf("/exists after a 404 flood: status=%d, want 200", rec.Code)
	}
}

// --- shardIndex -------------------------------------------------------------

// TestShardIndex_WithinRange 验证散列结果恒落在 [0, rateLimitShards):越界会直接索引
// 出数组边界而 panic。同时验证同一 key 稳定映射到同一分片,否则桶状态会分裂。
func TestShardIndex_WithinRange(t *testing.T) {
	keys := []string{
		"",                                  // 空 key(豁免路径不会走到这里,但散列必须安全)
		"a",                                 // 单字节
		"192.168.1.1",                       // IPv4 形态
		"2001:db8::1",                       // IPv6 形态
		"/users/:id",                        // 路由模板形态
		"tenant-abcdefghijklmnopqrstuvwxyz", // 长 key
		strings.Repeat("x", 1024),           // 超长 key
		"\x00\xff\x7f",                      // 非 ASCII 字节
		"key with spaces and 中文",            // 多字节 UTF-8
		"AAAA", "AAAB", "AAAC", "BAAA",      // 近似 key,检查分布不越界
	}
	for _, k := range keys {
		idx := shardIndex(k)
		if idx >= rateLimitShards {
			t.Errorf("shardIndex(%q)=%d, out of range [0,%d)", k, idx, rateLimitShards)
		}
		// 确定性:同一 key 必须稳定落在同一分片,否则同一客户端会拿到多个桶。
		if again := shardIndex(k); again != idx {
			t.Errorf("shardIndex(%q) not deterministic: %d then %d", k, idx, again)
		}
	}
}

// TestShardIndex_UsesAllShards 验证散列会用到多个分片:若所有 key 都落在同一片,
// 分片就退化为单锁,失去降低竞争的意义。
func TestShardIndex_UsesAllShards(t *testing.T) {
	seen := make(map[uint32]bool)
	for i := range 4096 {
		seen[shardIndex("key-"+strings.Repeat("0", i%7)+string(rune('a'+i%26))+itoaShard(i))] = true
	}
	if len(seen) != rateLimitShards {
		t.Errorf("distinct shards used=%d, want %d (a degenerate hash defeats sharding)", len(seen), rateLimitShards)
	}
}

// itoaShard 是测试内的极简十进制转换,避免为构造 key 引入额外依赖。
// itoaShard is a minimal decimal conversion used only to build test keys.
func itoaShard(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// --- 并发安全 ---------------------------------------------------------------

// TestRateLimit_ConcurrentAllowIsRaceFree 验证 allow 在多 goroutine 抢同一 key 时既
// 无数据竞争(配合 -race),也不会因竞态多发令牌。固定时钟使上界严格等于 Burst:
// 时间不前进就不该有任何补充,放行数超过 Burst 即意味着丢失了互斥。
func TestRateLimit_ConcurrentAllowIsRaceFree(t *testing.T) {
	const (
		burst      = 8
		goroutines = 16
		perG       = 25
	)
	rl := rlNewLimiter(1000, burst, time.Hour)
	now := time.Unix(1700000000, 0) // 固定时钟:补充量恒为 0

	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := 0
			for range perG {
				if rl.allow("hot", now) {
					local++
				}
			}
			mu.Lock()
			admitted += local
			mu.Unlock()
		}()
	}
	wg.Wait()

	if admitted != burst {
		t.Errorf("admitted=%d with a frozen clock, want exactly %d (excess means a lost mutex)", admitted, burst)
	}
}

// TestRateLimit_ConcurrentDistinctKeys 验证并发创建不同 key 的桶不会丢失或竞争:
// 每个新 key 的首个请求必然放行(新桶初始为满),因此放行数应恰好等于 key 数。
func TestRateLimit_ConcurrentDistinctKeys(t *testing.T) {
	const keys = 64
	rl := rlNewLimiter(1000, 1, time.Hour)
	now := time.Unix(1700000000, 0)

	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for i := range keys {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if rl.allow("key-"+itoaShard(i), now) {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if admitted != keys {
		t.Errorf("admitted=%d, want %d (each new key starts with a full bucket)", admitted, keys)
	}
}

// TestRateLimit_ConcurrentMiddlewareIsRaceFree 验证中间件整体在并发下 race-clean,
// 且放行数落在 [1, Burst+宽松补充] 内。上界取宽松值:真实时钟在测试期间会补少量令牌。
func TestRateLimit_ConcurrentMiddlewareIsRaceFree(t *testing.T) {
	const (
		burst      = 5
		goroutines = 12
		perG       = 10
	)
	var hookHits int64
	var hookMu sync.Mutex
	s := rlServer(t, RateLimit(RateLimitConfig{
		RPS:   10,
		Burst: burst,
		OnLimited: func(req *Request, key string) {
			hookMu.Lock()
			hookHits++
			hookMu.Unlock()
		},
	}), "/x")

	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, limited := 0, 0
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perG {
				rec := rlHit(s, "/x", "203.0.113.99:1", nil)
				mu.Lock()
				switch rec.Code {
				case http.StatusOK:
					ok++
				case http.StatusTooManyRequests:
					limited++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	total := goroutines * perG
	if ok+limited != total {
		t.Fatalf("ok+limited=%d, want %d (every request must end 200 or 429)", ok+limited, total)
	}
	if ok < 1 {
		t.Error("at least the initial burst must be admitted")
	}
	// 宽松上界:Burst 加上测试运行期间可能补充的令牌,仍必须远小于总请求数。
	if ok > burst+total {
		t.Errorf("admitted=%d exceeds the loose upper bound %d", ok, burst+total)
	}
	if ok >= total {
		t.Error("nothing was throttled; the limiter did not engage")
	}
	hookMu.Lock()
	defer hookMu.Unlock()
	if int(hookHits) != limited {
		t.Errorf("OnLimited fired %d times, want %d (once per rejection)", hookHits, limited)
	}
}

// TestRateLimit_KeyCapBoundsMemory 锁定桶数有上限。
//
// IdleTimeout 回收只在【被访问的分片】上触发,且要等到 idle 之后。攻击者以每请求一个
// 全新 key(伪造 IP、遍历租户 ID)持续打入时,桶表在一个 idle 窗口内可无界增长——
// 这本身就是一条内存耗尽路径,恰恰由限流组件提供。
func TestRateLimit_KeyCapBoundsMemory(t *testing.T) {
	const maxKeys = 64
	var limited int
	mw := RateLimit(RateLimitConfig{
		RPS:       1,
		Burst:     1,
		MaxKeys:   maxKeys,
		KeyFunc:   func(req *Request) string { return req.Header.Get("X-Tenant") },
		OnLimited: func(*Request, string) { limited++ },
	})
	terminal := func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}
	s := New()
	s.Use(mw)
	if err := s.RawHandle(http.MethodGet, "/x", RawHandlerFunc(terminal)); err != nil {
		t.Fatal(err)
	}
	// 每个请求一个全新 key,数量远超上限。
	const attempts = 5000
	for i := 0; i < attempts; i++ {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("X-Tenant", "tenant-"+strconv.Itoa(i))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		// 达到上限后新 key 放行(限流是可用性保护,不该把内存压力变成全站拒绝服务)。
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d got status %d, want 200: a fresh key must not be rejected", i, rec.Code)
		}
	}
	if limited != 0 {
		t.Errorf("OnLimited fired %d times for distinct keys, want 0", limited)
	}

	// 总桶数必须被上限约束住,而不是随 attempts 线性增长。直接在 limiter 上复演同一批
	// key,以便读到桶表(中间件闭包不暴露内部 limiter)。
	rl := newRateLimiter(RateLimitConfig{RPS: 1, Burst: 1, MaxKeys: maxKeys},
		1, defaultRateLimitIdle, func(*Request) string { return "" })
	now := time.Now()
	for i := 0; i < attempts; i++ {
		rl.allow("tenant-"+strconv.Itoa(i), now)
	}
	total := 0
	for i := range rl.shards {
		sh := &rl.shards[i]
		sh.mu.Lock()
		total += len(sh.buckets)
		sh.mu.Unlock()
	}
	// 每分片上限为 maxKeys/shards,故总量不超过 maxKeys(向下取整后可能略小)。
	if total > maxKeys {
		t.Errorf("tracked %d buckets for %d distinct keys, want at most MaxKeys=%d "+
			"(unbounded growth is a memory-exhaustion path)", total, attempts, maxKeys)
	}
	if total == 0 {
		t.Error("no buckets tracked at all: the limiter would never limit anything")
	}
}

// TestRateLimit_KeyCapStillLimitsTrackedKeys 确认上限没有让被跟踪的 key 失去限流。
func TestRateLimit_KeyCapStillLimitsTrackedKeys(t *testing.T) {
	rl := rlNewLimiter(1, 1, time.Minute)
	rl.perShardMax = 1
	now := time.Now()
	// 同一个 key 反复请求:第一次放行,之后按令牌桶拒绝(与上限无关)。
	if !rl.allow("k", now) {
		t.Fatal("first request for a key must be admitted")
	}
	if rl.allow("k", now) {
		t.Error("the second request in the same instant must be rejected: the cap must not disable limiting")
	}
}

// TestRateLimit_MaxKeysDefaultAndFloor 锁定 MaxKeys 的默认值与下限。
func TestRateLimit_MaxKeysDefaultAndFloor(t *testing.T) {
	cases := []struct {
		name    string
		maxKeys int
		want    int
	}{
		{"zero uses the default", 0, defaultRateLimitMaxKeys / rateLimitShards},
		{"negative uses the default", -5, defaultRateLimitMaxKeys / rateLimitShards},
		{"tiny value floors at 1", 1, 1},
		{"below shard count floors at 1", rateLimitShards - 1, 1},
		{"exact multiple divides evenly", rateLimitShards * 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rl := newRateLimiter(RateLimitConfig{RPS: 1, Burst: 1, MaxKeys: tc.maxKeys},
				1, defaultRateLimitIdle, func(*Request) string { return "" })
			if got := rl.perShardMax; got != tc.want {
				t.Errorf("perShardMax=%d, want %d", got, tc.want)
			}
		})
	}
}
