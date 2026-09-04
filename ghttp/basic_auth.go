package ghttp

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

// ===========================================================================
// BasicAuth 中间件 / BasicAuth middleware
//
// 校验 HTTP Basic 认证(RFC 7617)。仅在挂载了该中间件的路由生效,是标准中间件成本;未挂载
// 零影响。用户名/密码比较用 crypto/subtle 恒定时间比较,避免通过响应时间侧信道逐字节猜测;
// 未知用户也走一次同样的比较,使"用户名是否存在"不可由耗时区分(防用户名枚举)。
// 认证通过的用户名经 context 下传,处理器用 BasicAuthUser(ctx) 读取。
// Validates HTTP Basic authentication (RFC 7617). Active only on routes that mount
// it — standard middleware cost, zero impact when unmounted. Username/password use
// crypto/subtle constant-time comparison to avoid byte-by-byte guessing via a
// response-time side channel, and an unknown user runs the same comparison so that
// "does this username exist" cannot be told apart by timing (anti-enumeration). The
// authenticated username is passed down via context and read by handlers with
// BasicAuthUser(ctx).
// ===========================================================================

// basicAuthUserKey 是认证用户名在 context 中的私有键类型,避免跨包碰撞。
// basicAuthUserKey is the private context key type for the authenticated
// username, avoiding cross-package collisions.
type basicAuthUserKey struct{}

// BasicAuth 返回一个校验 Authorization: Basic 的中间件。accounts 为 用户名→密码 映射;
// 校验失败时返回 401 并设置 WWW-Authenticate: Basic realm="<realm>",提示客户端弹出认证。
// realm 为空时用 "Restricted"。成功时把用户名写入 context 供 BasicAuthUser 读取。
// BasicAuth returns a middleware validating Authorization: Basic. accounts maps
// username→password; on failure it returns 401 and sets WWW-Authenticate: Basic
// realm="<realm>" to prompt the client. An empty realm uses "Restricted". On
// success it stores the username in context for BasicAuthUser.
func BasicAuth(realm string, accounts map[string]string) Middleware {
	if realm == "" {
		realm = "Restricted"
	}
	// realm 可能含 " 需转义,注册期算好 challenge 头,请求期直接用。
	// realm may contain " needing escaping; precompute the challenge header once.
	challenge := `Basic realm="` + strings.ReplaceAll(realm, `"`, `\"`) + `"`

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			user, pass, ok := parseBasicAuth(req.Header.Get("Authorization"))
			if ok {
				// 用户不存在时仍对 dummy 值做一次【同样的】恒定时间比较,而不是 map miss
				// 后立即 401:短路会让"未知用户"明显快于"已知用户",攻击者据此枚举出有效
				// 用户名(密码可以慢慢爆破,用户名却是免费送的)。
				//
				// want 取 dummy 使两条路径的工作量一致;exists 与比较结果一起参与最终判定,
				// 既避免编译器把"结果未被使用"的比较优化掉,也防止未来重构误删这次比较。
				//
				// An unknown user still runs the SAME constant-time compare against a dummy
				// instead of returning 401 right after the map miss: short-circuiting makes
				// "unknown user" measurably faster than "known user", letting an attacker
				// enumerate valid usernames (passwords can be brute-forced slowly, but
				// usernames would be handed over for free).
				//
				// Falling back to the dummy equalizes the work on both paths, and folding
				// exists into the final decision alongside the comparison result keeps the
				// compare from being elided as an unused result — and from being dropped by
				// a future refactor.
				want, exists := accounts[user]
				if !exists {
					want = basicAuthDummyPassword
				}
				if constantTimeEqual(pass, want) && exists {
					ctx = context.WithValue(ctx, basicAuthUserKey{}, user)
					return next(ctx, req, resp)
				}
			}
			resp.Header().Set("WWW-Authenticate", challenge)
			return statusError(http.StatusUnauthorized)
		}
	}
}

// basicAuthDummyPassword 是未知用户走恒定时间比较时的对照值。它只需存在且不为空:
// 比较的目的是消耗与已知用户相同的工作量,而非"可能匹配"——它绝不会被判为认证成功
// (exists=false 单独否决)。
// basicAuthDummyPassword is the reference value an unknown user is compared against.
// It only needs to exist and be non-empty: the comparison exists to spend the same
// work as a known user, not to possibly match — it can never authenticate, since
// exists=false vetoes on its own.
const basicAuthDummyPassword = "x"

// BasicAuthUser 返回 BasicAuth 中间件在认证成功后写入 context 的用户名;不存在时返回空串。
// BasicAuthUser returns the username stored in context by BasicAuth on success, or
// "" if absent.
func BasicAuthUser(ctx context.Context) string {
	u, _ := ctx.Value(basicAuthUserKey{}).(string)
	return u
}

// parseBasicAuth 解析 "Basic <base64(user:pass)>" 头,返回用户名、密码与是否解析成功。
// scheme 名大小写不敏感(RFC 7617)。
// parseBasicAuth parses a "Basic <base64(user:pass)>" header, returning the
// username, password, and whether parsing succeeded. The scheme name is
// case-insensitive (RFC 7617).
func parseBasicAuth(header string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(header[len(prefix):])
	if err != nil {
		return "", "", false
	}
	creds := string(decoded)
	i := strings.IndexByte(creds, ':')
	if i < 0 {
		return "", "", false
	}
	return creds[:i], creds[i+1:], true
}

// constantTimeEqual 恒定时间比较两个字符串,防止通过比较耗时侧信道推断密码。
// constantTimeEqual compares two strings in constant time to prevent inferring
// the password via a comparison-time side channel.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
