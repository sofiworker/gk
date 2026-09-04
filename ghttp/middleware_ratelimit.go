package ghttp

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// ===========================================================================
// 限流:令牌桶(token bucket)中间件。
//
// 未挂载时零开销(纯中间件层);挂载后每请求一次映射查找 + 一次互斥区内的浮点运算。
//
// 桶状态惰性创建、按 key 分片存放,并周期性清理空闲桶,因此以客户端 IP 为 key 时不会
// 因为 IP 空间无界而无限增长内存。
//
// Rate limiting: a token-bucket middleware.
//
// Zero overhead when unmounted (a pure middleware layer); once mounted, each
// request costs one map lookup plus a little float arithmetic inside a mutex.
//
// Bucket state is created lazily, sharded by key, and idle buckets are reaped
// periodically, so keying by client IP cannot grow memory without bound just
// because the IP space is unbounded.
// ===========================================================================

// HeaderRetryAfter 是限流响应告知客户端重试间隔的标准头名。
// HeaderRetryAfter is the standard header telling a throttled client when to retry.
const HeaderRetryAfter = "Retry-After"

// RateLimitConfig 配置令牌桶限流。
// RateLimitConfig configures token-bucket rate limiting.
type RateLimitConfig struct {
	// RPS 是每秒补充的令牌数(平均允许速率)。<=0 时中间件透传(不限流)。
	// RPS is tokens refilled per second (the average allowed rate). A value <=0
	// makes the middleware pass through (no limiting).
	RPS float64

	// Burst 是桶容量(瞬时允许的突发量)。<=0 时取 max(1, ceil(RPS))。
	// Burst is the bucket capacity (instantaneous burst allowance). When <=0 it
	// becomes max(1, ceil(RPS)).
	Burst int

	// KeyFunc 决定限流维度。nil 时按 Request.ClientIP() 限流(遵循可信代理策略)。
	// 返回空串表示该请求不受限流(如放行内部探针)。
	// KeyFunc selects the limiting dimension. Nil limits by Request.ClientIP()
	// (honoring the trusted-proxy policy). Returning an empty string exempts the
	// request (e.g. letting internal probes through).
	KeyFunc func(req *Request) string

	// IdleTimeout 是桶的空闲回收时长:超过此时长未被访问的桶会被清理。<=0 时用
	// 默认 10 分钟。
	// IdleTimeout is the bucket idle-reap duration: a bucket untouched for longer
	// is removed. <=0 uses the 10-minute default.
	IdleTimeout time.Duration

	// OnLimited 在请求被拒时调用(可选),用于打点或告警。它不写响应。
	// OnLimited is invoked when a request is rejected (optional), for metrics or
	// alerting. It does not write the response.
	OnLimited func(req *Request, key string)

	// MaxKeys 是同时跟踪的 key 上限(0 时用默认 100000)。达到上限后新 key 不再建桶,
	// 直接放行。
	//
	// 为何必须有上限:IdleTimeout 回收只在【被访问的分片】上触发,且要等到 idle 之后。
	// 攻击者以每个请求一个全新 key(伪造 IP、遍历租户 ID)持续打入时,桶表在一个 idle
	// 窗口内可无界增长——这本身就是一条内存耗尽路径,恰恰由限流组件提供。达到上限时选择
	// 放行而非拒绝:限流是可用性保护而不是访问控制,把它变成"满了就全拒"会让攻击者用一
	// 次冲刷制造全站拒绝服务,比放过一部分请求更糟。
	// MaxKeys caps how many keys are tracked at once (0 uses the 100000 default).
	// Beyond it, a new key gets no bucket and passes through.
	//
	// Why a cap is required: IdleTimeout reaping fires only on the shard being touched,
	// and only after idle elapses. An attacker sending a brand-new key per request
	// (forged IPs, enumerated tenant IDs) can grow the bucket table without bound within
	// one idle window — a memory-exhaustion path supplied by the rate limiter itself.
	// Passing through rather than rejecting at the cap is deliberate: rate limiting is an
	// availability protection, not access control, and turning it into "reject everything
	// once full" would let one flush produce a site-wide denial of service, which is
	// worse than letting some requests past.
	MaxKeys int
}

// defaultRateLimitMaxKeys 是默认的 key 跟踪上限。按每桶约 100 字节估算,10 万 key 的
// 峰值内存在 10 MiB 量级,对服务是可接受的常数开销。
// defaultRateLimitMaxKeys is the default key-tracking cap. At roughly 100 bytes per
// bucket, 100000 keys peak in the 10 MiB range — an acceptable constant for a service.
const defaultRateLimitMaxKeys = 100000

// defaultRateLimitIdle 是桶空闲回收的默认时长。
// defaultRateLimitIdle is the default bucket idle-reap duration.
const defaultRateLimitIdle = 10 * time.Minute

// rateLimitUnknownKey 是默认 KeyFunc 取不到客户端地址时的兜底分桶键。
// rateLimitUnknownKey is the fallback bucket key when the default KeyFunc cannot
// resolve a client address.
const rateLimitUnknownKey = "__ghttp_unknown_client__"

// tokenBucket 是一个 key 的桶状态。用 tokens + last 的惰性补充模型:不跑定时器,
// 取令牌时按经过的时间一次性补足,因此空闲的桶不消耗任何 CPU。
// tokenBucket is one key's bucket state. It uses a lazy tokens + last refill
// model: no timer runs, and tokens are topped up in one step from the elapsed time
// when taken, so idle buckets consume no CPU at all.
type tokenBucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter 持有全部桶。用分片(shard)降低锁竞争:高并发下所有 key 争同一把锁会
// 让限流本身成为瓶颈,分片后不同 key 大概率落在不同锁上。
// rateLimiter holds every bucket. It shards to reduce lock contention: with all
// keys contending one lock, the limiter itself becomes the bottleneck under load;
// sharding sends different keys to different locks with high probability.
type rateLimiter struct {
	cfg   RateLimitConfig
	burst float64
	idle  time.Duration
	// perShardMax 是每分片的桶上限,由 MaxKeys 均摊到各分片得出。按分片而非全局计数,
	// 是为了让检查留在已持有的分片锁内,不引入跨分片的全局计数器(那会成为竞争热点)。
	// perShardMax is the per-shard bucket cap derived by spreading MaxKeys across
	// shards. Counting per shard keeps the check inside the already-held shard lock and
	// avoids a global counter, which would become a contention hot spot.
	perShardMax int
	shards      [rateLimitShards]rateLimitShard
	keyFunc     func(req *Request) string
}

// rateLimitShards 是分片数。取 2 的幂使取模退化为位与。
// rateLimitShards is the shard count. A power of two turns the modulo into a bit-and.
const rateLimitShards = 16

// rateLimitShard 是一个分片:一把锁 + 该片的桶表 + 上次清理时间。
// rateLimitShard is one shard: a lock, its bucket table, and the last sweep time.
type rateLimitShard struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	lastGC  time.Time
}

// RateLimit 返回令牌桶限流中间件。超过速率的请求以 429 拒绝,并带 Retry-After 头。
//
// 语义:每个 key 一个桶,容量 Burst、每秒补充 RPS 个令牌;每请求取一个令牌,取不到即拒。
// 因此它允许最多 Burst 的突发,长期平均速率收敛到 RPS。
//
// 默认按客户端 IP 限流;要按路由、用户或租户限流,用 KeyFunc 指定(例如
// func(req *ghttp.Request) string { return req.MatchedRoute() })。
//
// RateLimit returns the token-bucket rate-limiting middleware. Requests over the
// rate are rejected with 429 plus a Retry-After header.
//
// Semantics: one bucket per key with capacity Burst, refilled at RPS tokens per
// second; each request takes one token and is rejected if none is available. It
// therefore permits bursts up to Burst while the long-run average converges to RPS.
//
// It limits by client IP by default; to limit by route, user, or tenant, supply
// KeyFunc (e.g. func(req *ghttp.Request) string { return req.MatchedRoute() }).
func RateLimit(cfg RateLimitConfig) Middleware {
	// 用 `!(RPS > 0)` 而非 `RPS <= 0`：后者对 NaN 恒为 false，于是 NaN 会一路穿过——
	// 桶容量与补充速率都变成 NaN，`tokens < 1` 恒 false，结果每个请求都被放行却仍在
	// 记账，限流静默失效。NaN 可由 `RateLimitConfig{RPS: float64(n)/0}`、反序列化或
	// 配置合并产生，不是纯理论输入。
	// `!(RPS > 0)` rather than `RPS <= 0`: the latter is false for NaN, so NaN would
	// sail through — bucket capacity and refill both NaN, `tokens < 1` never true, and
	// every request admitted while still being tracked, silently disabling limiting.
	// NaN is reachable via division by zero, deserialization or config merging; it is
	// not a theoretical input.
	if !(cfg.RPS > 0) {
		// 无效速率:透传而非静默拒绝一切,避免配置失误造成全站不可用。
		// An invalid rate passes through rather than silently rejecting everything,
		// so a misconfiguration cannot take the whole site down.
		return func(next Handler) Handler { return next }
	}
	burst := cfg.Burst
	if burst <= 0 {
		burst = int(cfg.RPS)
		if float64(burst) < cfg.RPS {
			burst++ // 向上取整 / round up
		}
		if burst < 1 {
			burst = 1
		}
	}
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = defaultRateLimitIdle
	}
	keyFunc := cfg.KeyFunc
	if keyFunc == nil {
		// 默认维度不能返回空串：中间件把空 key 定义为"调用方显式豁免"，若 ClientIP()
		// 在某些部署下取不到地址（unix socket、被剥掉的 RemoteAddr、解析失败的转发头），
		// 这些流量就会整体跳过限流——最需要保护的 unidentified 流量反而不受管。落到一个
		// 共享兜底桶，至少仍受同一速率约束。自定义 KeyFunc 返回空串的豁免语义保持不变。
		// The default dimension must never be empty: the middleware defines an empty
		// key as "explicitly exempt", so if ClientIP() yields no address in some
		// deployment (unix sockets, a stripped RemoteAddr, an unparseable forwarded
		// header) that traffic skips limiting entirely — the unidentified flow, the one
		// most in need of it, goes unmanaged. Falling back to one shared bucket keeps it
		// under the same rate. A custom KeyFunc returning "" still means exempt.
		keyFunc = func(req *Request) string {
			if ip := req.ClientIP(); ip != "" {
				return ip
			}
			return rateLimitUnknownKey
		}
	}
	rl := newRateLimiter(cfg, burst, idle, keyFunc)

	// retryAfter 是补满一个令牌所需的秒数,注册期算一次(RPS 不变)。至少 1 秒——
	// Retry-After 的整秒语义下 0 会让客户端立即重试,失去退避意义。
	// retryAfter is the seconds needed to refill one token, computed once at
	// registration (RPS is fixed). At least 1: with Retry-After's whole-second
	// semantics, 0 would make clients retry instantly and lose the backoff.
	retryAfter := strconv.Itoa(max(1, int(1.0/cfg.RPS+0.999999)))

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			key := rl.keyFunc(req)
			if key == "" {
				return next(ctx, req, resp) // 豁免 / exempt
			}
			if rl.allow(key, time.Now()) {
				return next(ctx, req, resp)
			}
			if rl.cfg.OnLimited != nil {
				rl.cfg.OnLimited(req, key)
			}
			resp.Header().Set(HeaderRetryAfter, retryAfter)
			return fmt.Errorf("%w: key %q exceeds %.6g req/s", ErrRateLimitExceeded, key, rl.cfg.RPS)
		}
	}
}

// allow 尝试为 key 取一个令牌,报告是否放行。now 由调用方传入,便于测试注入时钟。
// allow tries to take one token for key, reporting whether to admit. now is passed
// in so tests can inject a clock.
func (rl *rateLimiter) allow(key string, now time.Time) bool {
	sh := &rl.shards[shardIndex(key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	b, ok := sh.buckets[key]
	if !ok {
		// 仅在触到上限时才强制清理,给"已过期但尚未被回收"的桶一次让位机会。
		// 不能无条件提前 sweep:分片的 lastGC 零值距 now 远超 idle,那会让每个分片的首个
		// 请求都触发一次全表扫描,并把同一批次刚建的桶误清掉。
		// Force a reap only when the cap is hit, giving expired-but-unreaped buckets a
		// chance to make room. An unconditional early sweep is wrong: a shard's zero-value
		// lastGC is further from now than idle, so every shard's first request would scan
		// the whole table and wrongly reap buckets created in that same batch.
		if len(sh.buckets) >= rl.perShardMax {
			rl.reap(sh, now)
		}
		if len(sh.buckets) >= rl.perShardMax {
			// 达到本分片上限:不建桶,直接放行。见 MaxKeys 的说明——限流是可用性保护,
			// "满了就全拒"会把内存压力变成全站拒绝服务。
			// At this shard's cap: create nothing and pass through. See MaxKeys — rate
			// limiting is an availability protection, and "reject once full" would turn
			// memory pressure into a site-wide denial of service.
			return true
		}
		// 新 key:桶初始为满,首个请求必然放行(而非因"桶空"误拒新客户端)。
		// A new key starts with a full bucket, so its first request is always
		// admitted rather than wrongly rejected for an "empty" bucket.
		sh.buckets[key] = &tokenBucket{tokens: rl.burst - 1, last: now}
		rl.sweep(sh, now)
		return true
	}

	// 惰性补充:按经过时间补足令牌,上限为桶容量。
	// Lazy refill: top up by elapsed time, capped at the bucket capacity.
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens += elapsed.Seconds() * rl.cfg.RPS
		if b.tokens > rl.burst {
			b.tokens = rl.burst
		}
	}
	b.last = now
	rl.sweep(sh, now)

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep 周期性清理本分片中空闲过久的桶。调用方须持有分片锁。清理频率与 idle 同阶,
// 因此均摊成本可忽略,且不需要后台 goroutine(中间件无 Close 时机可回收它)。
// sweep periodically reaps buckets idle for too long in this shard. The caller must
// hold the shard lock. Its frequency is on the order of idle, so the amortized cost
// is negligible and no background goroutine is needed (a middleware has no Close
// hook to reap one).
func (rl *rateLimiter) sweep(sh *rateLimitShard, now time.Time) {
	if now.Sub(sh.lastGC) < rl.idle {
		return
	}
	rl.reap(sh, now)
}

// reap 无条件清理本分片中空闲过久的桶并更新 lastGC。调用方须持有分片锁。
// 与 sweep 的区别是它不看频率门限,供"触到 key 上限、必须立刻腾位"的路径调用。
// reap unconditionally removes buckets idle for too long in this shard and updates
// lastGC. The caller must hold the shard lock. Unlike sweep it ignores the frequency
// gate, for the path that hit the key cap and must free room now.
func (rl *rateLimiter) reap(sh *rateLimitShard, now time.Time) {
	sh.lastGC = now
	for k, b := range sh.buckets {
		if now.Sub(b.last) >= rl.idle {
			delete(sh.buckets, k)
		}
	}
}

// newRateLimiter 归一化配置并预建分片,是 rateLimiter 的唯一构造口。
// 单独抽出而非内联在 RateLimit 里,是为了让配置归一化(尤其是 MaxKeys 的默认值与下限)
// 可以被直接单测,而不必透过中间件闭包去间接观察。
// newRateLimiter normalizes the config and pre-creates shards; it is the single
// construction point for rateLimiter. It is factored out of RateLimit so config
// normalization — notably the MaxKeys default and floor — is directly unit-testable
// instead of only observable through the middleware closure.
func newRateLimiter(cfg RateLimitConfig, burst int, idle time.Duration, keyFunc func(*Request) string) *rateLimiter {
	// 回收窗口必须不短于"从空桶补满"所需时间 burst/RPS，否则限流可被节奏化绕过：桶在仍
	// 持有未用令牌时被删除，下次访问按新 key 重建为满桶，于是每个 idle 周期都白得一次完整
	// burst。以 RPS=1、Burst=1000、IdleTimeout=1s 为例，每 2 秒打一轮 1000 请求即可长期
	// 维持 ~500 RPS，而配置声称 1 RPS。取该下限后，被回收的桶必然已接近满，删除不再额外
	// 放行。放在唯一构造口而非 RateLimit 里，是为了让任何构造路径都受同一保护。
	// The reap window must be at least the empty-to-full refill time burst/RPS, or
	// pacing defeats limiting: a bucket deleted while still holding unused tokens is
	// rebuilt full on the next visit, granting a fresh whole burst every idle period.
	// With RPS=1, Burst=1000 and IdleTimeout=1s, firing 1000 requests every 2 seconds
	// sustains ~500 RPS while the config claims 1. With this floor a reaped bucket is
	// necessarily near-full, so deleting it admits nothing extra. It lives in the single
	// construction point so no path can skip the protection.
	if cfg.RPS > 0 {
		if refill := time.Duration(float64(burst) / cfg.RPS * float64(time.Second)); idle < refill {
			idle = refill
		}
	}
	maxKeys := cfg.MaxKeys
	if maxKeys <= 0 {
		maxKeys = defaultRateLimitMaxKeys
	}
	// 上限均摊到各分片,至少 1(否则配了极小的 MaxKeys 会让所有请求都建不了桶)。
	// Spread the cap across shards, at least 1 (else a tiny MaxKeys would stop every
	// request from getting a bucket).
	perShardMax := maxKeys / rateLimitShards
	if perShardMax < 1 {
		perShardMax = 1
	}
	rl := &rateLimiter{
		cfg:         cfg,
		burst:       float64(burst),
		idle:        idle,
		perShardMax: perShardMax,
		keyFunc:     keyFunc,
	}
	for i := range rl.shards {
		rl.shards[i].buckets = make(map[string]*tokenBucket)
	}
	return rl
}

// shardIndex 用 FNV-1a 把 key 散列到分片下标。
// shardIndex hashes key to a shard index with FNV-1a.
func shardIndex(key string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h & (rateLimitShards - 1)
}

// RateLimitByRoute 是按命中路由模板限流的便捷构造器:同一路由共享一个桶,用于保护
// 单个昂贵端点(而非按客户端限流)。未命中路由的请求被豁免。
// RateLimitByRoute is a convenience constructor limiting by matched route template:
// one bucket per route, protecting a single expensive endpoint rather than limiting
// per client. Requests that matched no route are exempt.
func RateLimitByRoute(rps float64, burst int) Middleware {
	return RateLimit(RateLimitConfig{
		RPS:     rps,
		Burst:   burst,
		KeyFunc: func(req *Request) string { return req.MatchedRoute() },
	})
}
