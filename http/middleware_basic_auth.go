package ghttp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// baKey 是 context 中保存认证用户名的键。
// baKey is the context key for the authenticated user name.
type baKey struct{}

// BasicAuthDefaultRealm 是默认的 realm。
// BasicAuthDefaultRealm is the default realm.
const BasicAuthDefaultRealm = "Restricted"

type baConfig struct{ realm string }

// BasicAuthOption 配置 BasicAuth 中间件。
// BasicAuthOption configures the BasicAuth middleware.
type BasicAuthOption func(*baConfig)

// WithBasicAuthRealm 设置 realm；其中的引号、反斜杠与控制字符会被剥离。
// WithBasicAuthRealm sets the realm; quotes, backslashes and control characters are stripped.
func WithBasicAuthRealm(realm string) BasicAuthOption {
	return func(c *baConfig) { c.realm = realm }
}

// baSanitizeRealm 剥离会破坏头部语法的字符。
// baSanitizeRealm strips characters that would break the header syntax.
func baSanitizeRealm(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// BasicAuth 返回 HTTP Basic 认证中间件。失败时在返回 ErrUnauthorized 前设置 WWW-Authenticate。
// BasicAuth returns an HTTP Basic auth middleware. On failure it sets WWW-Authenticate before
// returning ErrUnauthorized.
func BasicAuth(validate func(ctx context.Context, user, pass string) bool, opts ...BasicAuthOption) Middleware {
	cfg := &baConfig{realm: BasicAuthDefaultRealm}
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}
	challenge := `Basic realm="` + baSanitizeRealm(cfg.realm) + `", charset="UTF-8"`
	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			user, pass, ok := req.Raw.BasicAuth()
			if !ok || validate == nil || !validate(ctx, user, pass) {
				resp.Header().Set("WWW-Authenticate", challenge)
				return ErrUnauthorized
			}
			req.Raw = req.Raw.WithContext(context.WithValue(req.Raw.Context(), baKey{}, user))
			return next(context.WithValue(ctx, baKey{}, user), req, resp)
		}
	}
}

// BasicAuthUser 返回认证成功的用户名。
// BasicAuthUser returns the authenticated user name.
func BasicAuthUser(ctx context.Context) (string, bool) {
	u, ok := ctx.Value(baKey{}).(string)
	return u, ok
}

// BasicAuthAccounts 返回基于静态账号表的校验器，使用常量时间比较（先哈希为定长再比较）；
// 用户名不存在时同样执行一次比较以避免时序差异。
// BasicAuthAccounts returns a validator over a static account map using constant-time
// comparison (hashed to fixed length first); a missing user still triggers a comparison to
// avoid timing differences.
func BasicAuthAccounts(accounts map[string]string) func(ctx context.Context, user, pass string) bool {
	// 拷贝，避免调用方后续修改
	// Copy so later caller mutation has no effect
	m := make(map[string][32]byte, len(accounts))
	for u, p := range accounts {
		m[u] = sha256.Sum256([]byte(p))
	}
	return func(_ context.Context, user, pass string) bool {
		want, found := m[user]
		got := sha256.Sum256([]byte(pass))
		eq := subtle.ConstantTimeCompare(got[:], want[:]) == 1
		return found && eq
	}
}
