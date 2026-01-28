package gserver

import (
	"context"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"time"
)

type requestIDKey struct{}

type RequestIDConfig struct {
	HeaderName            string
	DisableIncoming       bool
	DisableResponseHeader bool
}

var requestIDSeq uint64

func RequestID(cfg RequestIDConfig) HandlerFunc {
	if cfg.HeaderName == "" {
		cfg.HeaderName = "X-Request-ID"
	}

	return func(ctx *Context) {
		if ctx == nil || ctx.fastCtx == nil {
			if ctx != nil {
				ctx.Next()
			}
			return
		}

		var id string
		if !cfg.DisableIncoming {
			b := ctx.fastCtx.Request.Header.Peek(cfg.HeaderName)
			if len(b) != 0 {
				id = string(b)
			}
		}
		if id == "" {
			id = newRequestID()
		}

		ctx.Set(requestIDKey{}, id)
		if !cfg.DisableResponseHeader {
			ctx.Header(cfg.HeaderName, id)
		}

		ctx.Next()
	}
}

func newRequestID() string {
	seq := atomic.AddUint64(&requestIDSeq, 1)
	ts := uint64(time.Now().UnixNano())
	var b [32]byte
	out := b[:0]
	out = strconv.AppendUint(out, ts, 16)
	out = append(out, '-')
	out = strconv.AppendUint(out, seq, 16)
	return string(out)
}

type CORSConfig struct {
	AllowOrigins     []string
	AllowOriginFunc  func(origin []byte) bool
	AllowMethods     string
	AllowHeaders     string
	ExposeHeaders    string
	AllowCredentials bool
	MaxAge           time.Duration
}

func CORS(cfg CORSConfig) HandlerFunc {
	if cfg.AllowMethods == "" {
		cfg.AllowMethods = "GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS"
	}
	if cfg.AllowHeaders == "" {
		cfg.AllowHeaders = "Content-Type,Authorization"
	}

	allowOrigin := cfg.AllowOriginFunc
	if allowOrigin == nil {
		allowed := make(map[string]struct{}, len(cfg.AllowOrigins))
		for _, o := range cfg.AllowOrigins {
			allowed[o] = struct{}{}
		}
		allowOrigin = func(origin []byte) bool {
			if len(allowed) == 0 {
				return true
			}
			_, ok := allowed[string(origin)]
			return ok
		}
	}

	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strconv.FormatInt(int64(cfg.MaxAge/time.Second), 10)
	}

	return func(ctx *Context) {
		if ctx == nil || ctx.fastCtx == nil {
			if ctx != nil {
				ctx.Next()
			}
			return
		}

		origin := ctx.fastCtx.Request.Header.Peek("Origin")
		if len(origin) == 0 {
			ctx.Next()
			return
		}
		if !allowOrigin(origin) {
			ctx.Next()
			return
		}

		ctx.Header("Access-Control-Allow-Origin", string(origin))
		if cfg.AllowCredentials {
			ctx.Header("Access-Control-Allow-Credentials", "true")
		}
		if cfg.ExposeHeaders != "" {
			ctx.Header("Access-Control-Expose-Headers", cfg.ExposeHeaders)
		}

		if bytesEq(ctx.fastCtx.Method(), "OPTIONS") && len(ctx.fastCtx.Request.Header.Peek("Access-Control-Request-Method")) != 0 {
			ctx.Header("Access-Control-Allow-Methods", cfg.AllowMethods)
			ctx.Header("Access-Control-Allow-Headers", cfg.AllowHeaders)
			if maxAge != "" {
				ctx.Header("Access-Control-Max-Age", maxAge)
			}
			ctx.Status(http.StatusNoContent)
			ctx.Abort()
			return
		}

		ctx.Next()
	}
}

func bytesEq(b []byte, s string) bool {
	if len(b) != len(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if b[i] != s[i] {
			return false
		}
	}
	return true
}

type TimeoutConfig struct {
	Timeout time.Duration
}

// TimeoutContext sets ctx.Context() deadline for cooperative cancellation.
// It doesn't forcibly stop handlers; handlers must observe ctx.Done().
func TimeoutContext(cfg TimeoutConfig) HandlerFunc {
	if cfg.Timeout <= 0 {
		return func(ctx *Context) { ctx.Next() }
	}

	return func(ctx *Context) {
		base := ctx.Context()
		newCtx, cancel := context.WithTimeout(base, cfg.Timeout)
		defer cancel()
		ctx.SetContext(newCtx)
		ctx.Next()
	}
}

func RequestLogger() HandlerFunc {
	return func(ctx *Context) {
		start := time.Now()
		ctx.Next()

		logger := ctx.Logger()
		if logger == nil {
			return
		}

		method := ""
		path := ""
		if ctx.fastCtx != nil {
			method = string(ctx.fastCtx.Method())
			path = string(ctx.fastCtx.Path())
		}

		status := http.StatusOK
		if ctx.Writer != nil {
			status = ctx.Writer.Status()
		}
		logger.Infof("request %s %s -> %d (%s)", method, path, status, time.Since(start))
	}
}

func Recovery() HandlerFunc {
	return func(ctx *Context) {
		defer func() {
			if rec := recover(); rec != nil {
				if logger := ctx.Logger(); logger != nil {
					logger.Errorf("panic recovered: %v\n%s", rec, string(debug.Stack()))
				}

				if ctx.Writer != nil && !ctx.Writer.Written() {
					ctx.Status(http.StatusInternalServerError)
				}
				ctx.Abort()
			}
		}()

		ctx.Next()
	}
}
