package ghttp

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ErrTooManyRequests 表示 429 Too Many Requests。
// ErrTooManyRequests represents 429 Too Many Requests.
var ErrTooManyRequests = HTTPError{
	Status:  http.StatusTooManyRequests,
	Message: "too many requests",
}

// rlDefaultIdleTTL 是空闲桶的默认保留时间。
// rlDefaultIdleTTL is the default retention of idle buckets.
const rlDefaultIdleTTL = 10 * time.Minute

type rlBucket struct {
	tokens float64
	last   time.Time
}

type rlConfig struct {
	key     func(*Request) string
	clock   func() time.Time
	idleTTL time.Duration
}

// RateLimitOption 配置 RateLimit 中间件。
// RateLimitOption configures the RateLimit middleware.
type RateLimitOption func(*rlConfig)

// WithRateLimitKey 设置限流维度的键函数，默认 ClientIP。
// WithRateLimitKey sets the key function for limiting; default is ClientIP.
func WithRateLimitKey(fn func(*Request) string) RateLimitOption {
	return func(c *rlConfig) {
		if fn != nil {
			c.key = fn
		}
	}
}

// WithRateLimitClock 设置时钟（测试用）。
// WithRateLimitClock sets the clock (for tests).
func WithRateLimitClock(fn func() time.Time) RateLimitOption {
	return func(c *rlConfig) {
		if fn != nil {
			c.clock = fn
		}
	}
}

// WithRateLimitIdleTTL 设置空闲桶保留时间，默认 10 分钟；<=0 关闭清理。
// 清理为惰性执行（在请求路径上每个 TTL 周期扫描一次），不启动后台 goroutine。
// WithRateLimitIdleTTL sets idle bucket retention (default 10m); <=0 disables cleanup.
// Cleanup is lazy (swept on the request path once per TTL); no background goroutine.
func WithRateLimitIdleTTL(d time.Duration) RateLimitOption {
	return func(c *rlConfig) { c.idleTTL = d }
}

// rlLimiter 是并发安全的令牌桶集合。
// rlLimiter is a concurrency-safe set of token buckets.
type rlLimiter struct {
	rate, burst float64
	idleTTL     time.Duration
	mu          sync.Mutex
	buckets     map[string]*rlBucket
	lastSweep   time.Time
}

// allow 尝试取一个令牌；失败时返回需要等待的时间。
// allow tries to take a token; on failure it returns the wait time.
func (l *rlLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.idleTTL > 0 {
		if l.lastSweep.IsZero() {
			l.lastSweep = now
		} else if now.Sub(l.lastSweep) >= l.idleTTL {
			for k, b := range l.buckets {
				if now.Sub(b.last) >= l.idleTTL {
					delete(l.buckets, k)
				}
			}
			l.lastSweep = now
		}
	}
	b := l.buckets[key]
	if b == nil {
		b = &rlBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else if el := now.Sub(b.last); el > 0 {
		b.tokens = math.Min(l.burst, b.tokens+el.Seconds()*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	if l.rate <= 0 {
		return false, time.Duration(math.MaxInt64)
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// RateLimit 返回内存令牌桶限流中间件：每个键每秒补充 rate 个令牌，桶容量 burst。
// 超限时设置 Retry-After（向上取整秒）并返回 ErrTooManyRequests。
// rate<=0 时不补充令牌；burst<1 按 1 处理。
// RateLimit returns an in-memory token bucket middleware: per key, rate tokens/second with
// capacity burst. When exceeded it sets Retry-After (rounded up to seconds) and returns
// ErrTooManyRequests. rate<=0 means no refill; burst<1 is treated as 1.
func RateLimit(rate float64, burst int, opts ...RateLimitOption) Middleware {
	cfg := &rlConfig{key: ClientIP, clock: time.Now, idleTTL: rlDefaultIdleTTL}
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}
	if burst < 1 {
		burst = 1
	}
	l := &rlLimiter{rate: rate, burst: float64(burst), idleTTL: cfg.idleTTL, buckets: make(map[string]*rlBucket)}
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			ok, wait := l.allow(cfg.key(req), cfg.clock())
			if ok {
				return next(ctx, req, resp)
			}
			secs := int64(math.MaxInt32)
			if wait < time.Duration(secs)*time.Second {
				secs = int64(math.Ceil(wait.Seconds()))
			}
			if secs < 1 {
				secs = 1
			}
			resp.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
			return ErrTooManyRequests
		}
	}
}
