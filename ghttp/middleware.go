package ghttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

// MiddlewareFunc is the standard net/http middleware signature.
type MiddlewareFunc func(http.Handler) http.Handler

// HandlerMiddlewareFunc is a lightweight middleware signature for common
// handler-function wrapping.
type HandlerMiddlewareFunc func(http.HandlerFunc) http.HandlerFunc

// HandlerMiddleware adapts a HandlerMiddlewareFunc into the standard
// MiddlewareFunc shape.
func HandlerMiddleware(fn HandlerMiddlewareFunc) MiddlewareFunc {
	if fn == nil {
		return func(next http.Handler) http.Handler {
			return next
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(fn(next.ServeHTTP))
	}
}

// RequestID adds a unique X-Request-ID header to every response.
func RequestID() MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				b := make([]byte, 16)
				rand.Read(b)
				id = hex.EncodeToString(b)
			}
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r)
		})
	}
}

// CORSConfig configures CORS middleware.
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	MaxAge           int
}

// CORS returns a CORS middleware.
func CORS(cfg CORSConfig) MiddlewareFunc {
	allowMethods := joinStrings(cfg.AllowMethods)
	allowHeaders := joinStrings(cfg.AllowHeaders)
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = itoa(cfg.MaxAge)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			for _, allowed := range cfg.AllowOrigins {
				if allowed == "*" || allowed == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}

			if allowMethods != "" {
				w.Header().Set("Access-Control-Allow-Methods", allowMethods)
			}
			if allowHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
			}
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if maxAge != "" {
				w.Header().Set("Access-Control-Max-Age", maxAge)
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger logs each request.
func RequestLogger() MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			if logger := loggerFromRequest(r); logger != nil {
				logger.Infof("%s %s %s", r.Method, r.URL.Path, time.Since(start))
			}
		})
	}
}

// Recoverer catches panics and returns 500.
func Recoverer() MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if logger := loggerFromRequest(r); logger != nil {
						logger.Errorf("panic: %v", rec)
					}
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func loggerFromRequest(r *http.Request) Logger {
	server, _ := r.Context().Value(serverContextKey{}).(*Server)
	if server == nil {
		return nil
	}
	return server.logger
}

// Timeout adds a timeout to the request context.
func Timeout(d time.Duration) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			ctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func joinStrings(s []string) string {
	if len(s) == 0 {
		return ""
	}
	r := s[0]
	for i := 1; i < len(s); i++ {
		r += ", " + s[i]
	}
	return r
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
