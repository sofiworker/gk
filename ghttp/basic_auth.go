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
// 零影响。用户名/密码比较用 crypto/subtle 恒定时间比较,避免通过响应时间侧信道逐字节猜测。
// 认证通过的用户名经 context 下传,处理器用 BasicAuthUser(ctx) 读取。
// Validates HTTP Basic authentication (RFC 7617). Active only on routes that mount
// it — standard middleware cost, zero impact when unmounted. Username/password use
// crypto/subtle constant-time comparison to avoid byte-by-byte guessing via a
// response-time side channel. The authenticated username is passed down via
// context and read by handlers with BasicAuthUser(ctx).
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
				if want, exists := accounts[user]; exists && constantTimeEqual(pass, want) {
					ctx = context.WithValue(ctx, basicAuthUserKey{}, user)
					return next(ctx, req, resp)
				}
			}
			resp.Header().Set("WWW-Authenticate", challenge)
			return statusError(http.StatusUnauthorized)
		}
	}
}

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
