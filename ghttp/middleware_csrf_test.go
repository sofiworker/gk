package ghttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ===========================================================================
// CSRF 中间件测试。核心断言分三层:
//   1. 端到端(经 Server.ServeHTTP):验证状态码、错误体 code、cookie 属性;
//   2. 中间件直调(不经 mux):验证返回的 error 本体可 errors.Is 到哨兵;
//   3. 纯函数(isSafeMethod / checkCSRFOrigin / newCSRFToken / csrfFormToken)。
// CSRF middleware tests at three layers: end-to-end via Server.ServeHTTP,
// direct middleware invocation to inspect the returned error, and pure helpers.
// ===========================================================================

// csrfObserver 是端到端测试的一次观测结果:除响应外还捕获错误链分类出的状态码与原始
// error,使 "403 + csrf_token_invalid" 与 "errors.Is(err, ErrCSRFTokenInvalid)" 能在
// 同一次请求里同时断言。
// csrfObserver is one end-to-end observation: besides the response it captures the
// status and raw error classified by the error chain, so "403 + csrf_token_invalid"
// and "errors.Is(err, ErrCSRFTokenInvalid)" can be asserted on the same request.
type csrfObserver struct {
	rec       *httptest.ResponseRecorder
	hookErr   error
	hookCalls int
	ctxToken  string
	reached   bool
}

// newCSRFServer 装一台只挂 CSRF 中间件的 Server,并在 /guard 上注册全部方法的裸终端。
// 终端记录 context 中的 token 与是否被到达,供"是否放行"与"token 是否下传"两类断言复用。
// newCSRFServer builds a Server with only the CSRF middleware, registering a bare
// terminal for every method on /guard that records the context token and whether it
// was reached.
func newCSRFServer(t *testing.T, cfg CSRFConfig, obs *csrfObserver) *Server {
	t.Helper()
	s := New(WithErrorHook(func(_ *http.Request, _ int, err error) {
		obs.hookErr = err
		obs.hookCalls++
	}))
	s.Use(CSRF(cfg))
	terminal := RawHandlerFunc(func(ctx context.Context, _ *Request, resp *Response) error {
		obs.reached = true
		obs.ctxToken = CSRFTokenFromContext(ctx)
		resp.WriteHeader(http.StatusOK)
		return nil
	})
	for _, m := range []string{
		http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace,
		http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
	} {
		if err := s.RawHandle(m, "/guard", terminal); err != nil {
			t.Fatalf("RawHandle %s: %v", m, err)
		}
	}
	return s
}

// csrfRequest 是一次 CSRF 端到端请求的描述。
// csrfRequest describes one end-to-end CSRF request.
type csrfRequest struct {
	method  string
	body    string
	headers map[string]string
	cookies map[string]string
}

// serveCSRF 执行一次 csrfRequest 并返回观测结果。
// serveCSRF performs one csrfRequest and returns the observation.
func serveCSRF(t *testing.T, cfg CSRFConfig, in csrfRequest) *csrfObserver {
	t.Helper()
	obs := &csrfObserver{}
	s := newCSRFServer(t, cfg, obs)
	var r *http.Request
	if in.body != "" {
		r = httptest.NewRequest(in.method, "/guard", strings.NewReader(in.body))
	} else {
		r = httptest.NewRequest(in.method, "/guard", nil)
	}
	for k, v := range in.headers {
		r.Header.Set(k, v)
	}
	for k, v := range in.cookies {
		r.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	obs.rec = httptest.NewRecorder()
	s.ServeHTTP(obs.rec, r)
	return obs
}

// assertCSRFRejected 断言一次请求被 CSRF 拒绝:403 + 规范错误体 code + 哨兵可 Is +
// 终端未被到达(必须验证"未到达",否则 handler 已执行副作用后才 403 等于防护失效)。
// assertCSRFRejected asserts a request was rejected: 403, the canonical error code,
// an errors.Is-able sentinel, and — critically — that the terminal was NOT reached.
func assertCSRFRejected(t *testing.T, obs *csrfObserver) {
	t.Helper()
	if obs.rec.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403 (body=%s)", obs.rec.Code, obs.rec.Body.String())
	}
	if want := `"code":"csrf_token_invalid"`; !strings.Contains(obs.rec.Body.String(), want) {
		t.Errorf("body=%s, want it to contain %s", obs.rec.Body.String(), want)
	}
	if !errors.Is(obs.hookErr, ErrCSRFTokenInvalid) {
		t.Errorf("hook err=%v, want errors.Is(err, ErrCSRFTokenInvalid)", obs.hookErr)
	}
	if obs.reached {
		t.Error("rejected request must not reach the handler (side effects would already have run)")
	}
}

// --- 安全方法:发放 token ------------------------------------------------------
// --- Safe methods: token issuance --------------------------------------------

// TestCSRF_SafeMethodIssuesToken 验证无 cookie 的 GET 会下发 token cookie,且同一 token
// 经 context 下传。同时锁定 cookie 属性:Path=/、SameSite=Lax、且【不是】 HttpOnly——
// 双提交模式要求前端 JS 能读到 token,HttpOnly 会静默破坏整套机制,故显式断言以防回归。
func TestCSRF_SafeMethodIssuesToken(t *testing.T) {
	obs := serveCSRF(t, CSRFConfig{}, csrfRequest{method: http.MethodGet})
	if obs.rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", obs.rec.Code)
	}
	cookies := obs.rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d Set-Cookie, want exactly 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != DefaultCSRFCookieName {
		t.Errorf("cookie name=%q, want %q", c.Name, DefaultCSRFCookieName)
	}
	if c.Value == "" {
		t.Error("issued cookie value must be non-empty")
	}
	if obs.ctxToken != c.Value {
		t.Errorf("context token=%q, want it to equal the cookie value %q", obs.ctxToken, c.Value)
	}
	if c.Path != "/" {
		t.Errorf("cookie Path=%q, want %q by default", c.Path, "/")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite=%v, want Lax by default", c.SameSite)
	}
	if c.HttpOnly {
		t.Error("CSRF cookie must NOT be HttpOnly: double-submit needs front-end JS to read it")
	}
}

// TestCSRF_SafeMethodExistingCookieNotReissued 验证已带有效 cookie 的 GET 不重复下发:
// 每次都换新 token 会让并发标签页/多请求页面互相踩掉对方的 token。
func TestCSRF_SafeMethodExistingCookieNotReissued(t *testing.T) {
	const tok = "existing-token-value"
	obs := serveCSRF(t, CSRFConfig{}, csrfRequest{
		method:  http.MethodGet,
		cookies: map[string]string{DefaultCSRFCookieName: tok},
	})
	if obs.rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", obs.rec.Code)
	}
	if sc := obs.rec.Header().Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("Set-Cookie=%v, want none (a valid cookie must not be reissued)", sc)
	}
	if obs.ctxToken != tok {
		t.Errorf("context token=%q, want the existing cookie value %q", obs.ctxToken, tok)
	}
}

// TestCSRF_SafeMethodsAllIssue 验证四个安全方法都只发放不校验(HEAD/OPTIONS/TRACE 同 GET)。
func TestCSRF_SafeMethodsAllIssue(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		t.Run(method, func(t *testing.T) {
			obs := serveCSRF(t, CSRFConfig{}, csrfRequest{method: method})
			if obs.rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", obs.rec.Code)
			}
			if len(obs.rec.Result().Cookies()) != 1 {
				t.Errorf("safe method %s should issue exactly one cookie", method)
			}
		})
	}
}

// --- 非安全方法:双提交校验 ----------------------------------------------------
// --- Unsafe methods: double-submit verification -------------------------------

// TestCSRF_UnsafeMethodHeaderMatches 验证 header 与 cookie 一致时放行,并把 token 下传。
func TestCSRF_UnsafeMethodHeaderMatches(t *testing.T) {
	const tok = "match-me-abc123"
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			obs := serveCSRF(t, CSRFConfig{}, csrfRequest{
				method:  method,
				headers: map[string]string{DefaultCSRFHeaderName: tok},
				cookies: map[string]string{DefaultCSRFCookieName: tok},
			})
			if obs.rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200 (body=%s)", obs.rec.Code, obs.rec.Body.String())
			}
			if !obs.reached {
				t.Error("matching token should reach the handler")
			}
			if obs.ctxToken != tok {
				t.Errorf("context token=%q, want %q", obs.ctxToken, tok)
			}
		})
	}
}

// TestCSRF_UnsafeMethodRejected 表驱动覆盖三类拒绝:完全无 cookie、有 cookie 但未提交
// token、提交的 token 与 cookie 不一致。三者都必须是 403 + csrf_token_invalid,不向客户端
// 区分原因(区分即泄露防护细节)。
func TestCSRF_UnsafeMethodRejected(t *testing.T) {
	const tok = "cookie-token-xyz"
	cases := []struct {
		name string
		in   csrfRequest
	}{
		{
			name: "no cookie at all",
			in: csrfRequest{
				method:  http.MethodPost,
				headers: map[string]string{DefaultCSRFHeaderName: tok},
			},
		},
		{
			name: "cookie present but nothing submitted",
			in: csrfRequest{
				method:  http.MethodPost,
				cookies: map[string]string{DefaultCSRFCookieName: tok},
			},
		},
		{
			name: "header differs from cookie",
			in: csrfRequest{
				method:  http.MethodPost,
				headers: map[string]string{DefaultCSRFHeaderName: "some-other-token"},
				cookies: map[string]string{DefaultCSRFCookieName: tok},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertCSRFRejected(t, serveCSRF(t, CSRFConfig{}, c.in))
		})
	}
}

// TestCSRF_ConstantTimeCompareRejectsPrefixAndLength 验证恒定时间比较的正确性边界:
// 真 token 的前缀、以及长度不同的 token 都必须被拒。subtle.ConstantTimeCompare 对
// 长度不等的入参直接返回 0,这里锁定该语义——若有人改成 strings.HasPrefix 之类,
// 前缀 token 就会被误放行。
func TestCSRF_ConstantTimeCompareRejectsPrefixAndLength(t *testing.T) {
	const tok = "abcdefghijklmnopqrstuvwxyz012345"
	cases := []struct {
		name string
		sent string
	}{
		{"prefix of the real token", tok[:len(tok)-1]},
		{"shorter by a lot", tok[:4]},
		{"longer than the real token", tok + "X"},
		{"same length but different last byte", tok[:len(tok)-1] + "9"},
		{"empty string is not a match", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertCSRFRejected(t, serveCSRF(t, CSRFConfig{}, csrfRequest{
				method:  http.MethodPost,
				headers: map[string]string{DefaultCSRFHeaderName: c.sent},
				cookies: map[string]string{DefaultCSRFCookieName: tok},
			}))
		})
	}
}

// TestCSRF_MiddlewareReturnsSentinelDirectly 直调中间件链(绕过 mux 与错误链),验证
// 中间件【本身】返回的 error 就包装了 ErrCSRFTokenInvalid,而不是依赖 mux 事后归类。
func TestCSRF_MiddlewareReturnsSentinelDirectly(t *testing.T) {
	called := false
	h := CSRF(CSRFConfig{})(func(context.Context, *Request, *Response) error {
		called = true
		return nil
	})
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	err := h(context.Background(), &Request{Request: r}, &Response{ResponseWriter: httptest.NewRecorder()})
	if !errors.Is(err, ErrCSRFTokenInvalid) {
		t.Errorf("err=%v, want errors.Is(err, ErrCSRFTokenInvalid)", err)
	}
	if called {
		t.Error("next must not be invoked when verification fails")
	}
	// 中间件不自行写响应:状态码由统一错误链决定,以便 WithErrorRenderer 生效。
	if status, _ := classifyError(err); status != http.StatusForbidden {
		t.Errorf("classifyError status=%d, want 403", status)
	}
}

// --- 表单字段回退 --------------------------------------------------------------
// --- Form field fallback ------------------------------------------------------

// TestCSRF_FormFieldFallbackURLEncoded 验证 urlencoded 表单字段可作为 header 的回退,
// 支持无 JS 的原生表单提交。
func TestCSRF_FormFieldFallbackURLEncoded(t *testing.T) {
	const tok = "form-token-1"
	obs := serveCSRF(t, CSRFConfig{}, csrfRequest{
		method:  http.MethodPost,
		body:    url.Values{DefaultCSRFFieldName: {tok}, "name": {"alice"}}.Encode(),
		headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		cookies: map[string]string{DefaultCSRFCookieName: tok},
	})
	if obs.rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (body=%s)", obs.rec.Code, obs.rec.Body.String())
	}
	if !obs.reached {
		t.Error("urlencoded form token should reach the handler")
	}
}

// TestCSRF_FormFieldFallbackMultipart 验证 multipart 表单同样可回退取 token。
// 这条不能省:标准库的 ParseForm 对 multipart 体【不解析】却把 PostForm 置为非 nil,
// 若实现只调 ParseForm,PostFormValue 会认定已解析而永不补解析,multipart 提交恒被误拒。
func TestCSRF_FormFieldFallbackMultipart(t *testing.T) {
	const tok = "multipart-token-2"
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField(DefaultCSRFFieldName, tok); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := w.WriteField("name", "bob"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	obs := serveCSRF(t, CSRFConfig{}, csrfRequest{
		method:  http.MethodPost,
		body:    buf.String(),
		headers: map[string]string{"Content-Type": w.FormDataContentType()},
		cookies: map[string]string{DefaultCSRFCookieName: tok},
	})
	if obs.rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (body=%s)", obs.rec.Code, obs.rec.Body.String())
	}
	if !obs.reached {
		t.Error("multipart form token should reach the handler")
	}
}

// TestCSRF_FormFallbackNotTriggeredForJSON 验证 JSON 体绝不被当表单解析:无 header 的
// JSON POST 必须直接被拒,且请求体保持【未被消耗】——一旦解析,下游 typed 解码将读到空体。
func TestCSRF_FormFallbackNotTriggeredForJSON(t *testing.T) {
	const tok = "json-token-3"
	const body = `{"csrf_token":"json-token-3","name":"carol"}`
	obs := serveCSRF(t, CSRFConfig{}, csrfRequest{
		method:  http.MethodPost,
		body:    body,
		headers: map[string]string{"Content-Type": "application/json"},
		cookies: map[string]string{DefaultCSRFCookieName: tok},
	})
	assertCSRFRejected(t, obs)

	// 独立断言体未被消耗:直调 csrfFormToken 后请求体仍可完整读出。
	r := httptest.NewRequest(http.MethodPost, "/guard", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	req := &Request{Request: r}
	if got := csrfFormToken(req, DefaultCSRFFieldName); got != "" {
		t.Errorf("csrfFormToken on JSON = %q, want \"\"", got)
	}
	rest, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(rest) != body {
		t.Errorf("JSON body was consumed: remaining=%q, want the full %q", rest, body)
	}
	if req.PostForm.Get(DefaultCSRFFieldName) != "" || req.MultipartForm != nil {
		t.Error("a JSON request must not have its form parsed at all")
	}
}

// TestCSRFFormToken 表驱动直测 csrfFormToken 的 Content-Type 分派与字段名。
func TestCSRFFormToken(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		field       string
		want        string
	}{
		{"urlencoded hit", "application/x-www-form-urlencoded", "csrf_token=A&x=1", "csrf_token", "A"},
		{"urlencoded with charset param", "application/x-www-form-urlencoded; charset=utf-8", "csrf_token=B", "csrf_token", "B"},
		{"urlencoded custom field", "application/x-www-form-urlencoded", "_tok=C", "_tok", "C"},
		{"urlencoded field absent", "application/x-www-form-urlencoded", "other=1", "csrf_token", ""},
		{"json is never parsed", "application/json", `{"csrf_token":"D"}`, "csrf_token", ""},
		{"text/plain is never parsed", "text/plain", "csrf_token=E", "csrf_token", ""},
		{"no content type", "", "csrf_token=F", "csrf_token", ""},
		{"malformed urlencoded body", "application/x-www-form-urlencoded", "%zz=1", "csrf_token", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(c.body))
			if c.contentType != "" {
				r.Header.Set("Content-Type", c.contentType)
			}
			if got := csrfFormToken(&Request{Request: r}, c.field); got != c.want {
				t.Errorf("csrfFormToken = %q, want %q", got, c.want)
			}
		})
	}
}

// --- 同源校验 ------------------------------------------------------------------
// --- Origin / Referer checks --------------------------------------------------

// TestCheckCSRFOrigin 表驱动直测 checkCSRFOrigin。httptest.NewRequest 的默认 Host 是
// example.com 且【不带 TLS】,故请求自身 scheme 为 http,"http://example.com" 为同源。
func TestCheckCSRFOrigin(t *testing.T) {
	trusted := map[string]bool{
		"https://app.example.com": true,
		"https://partner.io":      true,
	}
	cases := []struct {
		name    string
		origin  string
		referer string
		wantErr bool
	}{
		{name: "same-origin Origin passes", origin: "http://example.com"},
		// 原断言是 "ignores scheme mismatch (host compared)" 且期望放行,把 M5 的漏洞写成了
		// 契约。只比 host 会让 http://example.com 这种中间人可任意改写的明文页面被判为 https
		// 站点的同源来源,同源校验这道第二道防线就此失效。本请求自身是明文(无 TLS),故
		// https:// 来源属于跨 scheme,必须拒绝。
		{name: "cross-scheme Origin is rejected (scheme is part of the origin)", origin: "https://example.com", wantErr: true},
		{name: "same-origin Origin case-insensitive host", origin: "http://EXAMPLE.com"},
		{name: "cross-site Origin not trusted is rejected", origin: "https://evil.com", wantErr: true},
		{name: "trusted Origin passes", origin: "https://app.example.com"},
		{name: "trusted Origin with trailing slash passes", origin: "https://app.example.com/"},
		{name: "trusted Origin case-insensitive passes", origin: "https://APP.example.com"},
		// 原断言是 "Origin null is treated as absent" 且期望放行,同样把 M5 的漏洞写成了契约。
		// "头缺失" 与 "头存在且值为 null" 不是一回事:前者是 curl 之类不带 cookie 的非浏览器
		// 客户端,后者是浏览器【主动声明】的不透明来源——沙箱 iframe、data:/blob: 文档正是
		// 攻击者可构造的上下文。放行等于对这些来源完全关闭第二道防线,故必须拒绝。
		{name: "Origin null (opaque origin) is rejected, not treated as absent", origin: "null", wantErr: true},
		{name: "no Origin but same-origin Referer passes", referer: "http://example.com/page"},
		{name: "no Origin and cross-site Referer is rejected", referer: "https://evil.com/page", wantErr: true},
		{name: "no Origin and trusted Referer passes", referer: "https://partner.io/checkout"},
		{name: "malformed Referer is rejected", referer: "://not a url", wantErr: true},
		{name: "hostless Referer is rejected", referer: "/relative/path", wantErr: true},
		{name: "both absent passes (non-browser client)"},
		{name: "Origin wins over a cross-site Referer", origin: "http://example.com", referer: "https://evil.com/p"},
		{name: "malformed Origin is rejected", origin: "http://[bad", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/x", nil)
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if c.referer != "" {
				r.Header.Set("Referer", c.referer)
			}
			err := checkCSRFOrigin(&Request{Request: r}, trusted)
			if c.wantErr {
				if !errors.Is(err, ErrCSRFTokenInvalid) {
					t.Errorf("err=%v, want errors.Is(err, ErrCSRFTokenInvalid)", err)
				}
				return
			}
			if err != nil {
				t.Errorf("err=%v, want nil", err)
			}
		})
	}
}

// TestCSRF_OriginCheckEndToEnd 端到端验证同源校验先于双提交生效:即便 token 完全正确,
// 不可信来源仍被拒;把该来源加入 TrustedOrigins 后同一请求放行。
func TestCSRF_OriginCheckEndToEnd(t *testing.T) {
	const tok = "origin-e2e-token"
	in := csrfRequest{
		method: http.MethodPost,
		headers: map[string]string{
			DefaultCSRFHeaderName: tok,
			"Origin":              "https://attacker.example",
		},
		cookies: map[string]string{DefaultCSRFCookieName: tok},
	}
	assertCSRFRejected(t, serveCSRF(t, CSRFConfig{}, in))

	obs := serveCSRF(t, CSRFConfig{TrustedOrigins: []string{"https://attacker.example"}}, in)
	if obs.rec.Code != http.StatusOK {
		t.Errorf("trusted origin: status=%d, want 200 (body=%s)", obs.rec.Code, obs.rec.Body.String())
	}
	if !obs.reached {
		t.Error("trusted origin should reach the handler")
	}
}

// TestCSRF_CrossSiteRefererEndToEnd 端到端验证仅带跨站 Referer(浏览器降级场景)被拒。
func TestCSRF_CrossSiteRefererEndToEnd(t *testing.T) {
	const tok = "referer-e2e-token"
	assertCSRFRejected(t, serveCSRF(t, CSRFConfig{}, csrfRequest{
		method: http.MethodPost,
		headers: map[string]string{
			DefaultCSRFHeaderName: tok,
			"Referer":             "https://evil.example/attack.html",
		},
		cookies: map[string]string{DefaultCSRFCookieName: tok},
	}))
}

// --- Skip / 自定义名 / cookie 属性 ---------------------------------------------
// --- Skip / custom names / cookie attributes ----------------------------------

// TestCSRF_SkipExemptsRequest 验证 Skip 返回 true 时完全跳过校验:连 token 都不需要。
// 用于 Authorization 头认证的端点——浏览器不会自动附带该头,本就不受 CSRF 威胁。
func TestCSRF_SkipExemptsRequest(t *testing.T) {
	cfg := CSRFConfig{Skip: func(req *Request) bool {
		return strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ")
	}}
	obs := serveCSRF(t, cfg, csrfRequest{
		method:  http.MethodPost,
		headers: map[string]string{"Authorization": "Bearer api-token"},
	})
	if obs.rec.Code != http.StatusOK {
		t.Fatalf("skipped request: status=%d, want 200 (body=%s)", obs.rec.Code, obs.rec.Body.String())
	}
	if !obs.reached {
		t.Error("Skip=true should reach the handler with no token at all")
	}
	// Skip 也跳过发放:不应下发 cookie。
	if sc := obs.rec.Header().Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("Set-Cookie=%v, want none for a skipped request", sc)
	}

	// 同一配置下不满足 Skip 条件的请求仍被拦。
	assertCSRFRejected(t, serveCSRF(t, cfg, csrfRequest{method: http.MethodPost}))
}

// TestCSRF_CustomNames 验证自定义 CookieName / HeaderName / FieldName 全部生效,
// 且默认名在自定义后【不再】被接受(否则等于留了一条旁路)。
func TestCSRF_CustomNames(t *testing.T) {
	const tok = "custom-names-token"
	cfg := CSRFConfig{
		CookieName: "_xsrf",
		HeaderName: "X-App-CSRF",
		FieldName:  "_xsrf_field",
	}

	t.Run("custom cookie is issued on a safe method", func(t *testing.T) {
		obs := serveCSRF(t, cfg, csrfRequest{method: http.MethodGet})
		cookies := obs.rec.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != "_xsrf" {
			t.Fatalf("cookies=%v, want exactly one named _xsrf", cookies)
		}
	})

	t.Run("custom header is accepted", func(t *testing.T) {
		obs := serveCSRF(t, cfg, csrfRequest{
			method:  http.MethodPost,
			headers: map[string]string{"X-App-CSRF": tok},
			cookies: map[string]string{"_xsrf": tok},
		})
		if obs.rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 (body=%s)", obs.rec.Code, obs.rec.Body.String())
		}
	})

	t.Run("custom form field is accepted", func(t *testing.T) {
		obs := serveCSRF(t, cfg, csrfRequest{
			method:  http.MethodPost,
			body:    url.Values{"_xsrf_field": {tok}}.Encode(),
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
			cookies: map[string]string{"_xsrf": tok},
		})
		if obs.rec.Code != http.StatusOK {
			t.Errorf("status=%d, want 200 (body=%s)", obs.rec.Code, obs.rec.Body.String())
		}
	})

	t.Run("default names are no longer a bypass", func(t *testing.T) {
		assertCSRFRejected(t, serveCSRF(t, cfg, csrfRequest{
			method:  http.MethodPost,
			headers: map[string]string{DefaultCSRFHeaderName: tok},
			cookies: map[string]string{DefaultCSRFCookieName: tok},
		}))
	})
}

// TestCSRF_CookieAttributes 验证 Secure / CookieMaxAge / CookieDomain / CookiePath /
// SameSite 都如实反映到下发的 cookie 上。
func TestCSRF_CookieAttributes(t *testing.T) {
	cfg := CSRFConfig{
		CookiePath:   "/admin",
		CookieDomain: "example.com",
		CookieMaxAge: 3600,
		Secure:       true,
		SameSite:     http.SameSiteStrictMode,
	}
	obs := serveCSRF(t, cfg, csrfRequest{method: http.MethodGet})
	cookies := obs.rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Path != "/admin" {
		t.Errorf("Path=%q, want /admin", c.Path)
	}
	if c.Domain != "example.com" {
		t.Errorf("Domain=%q, want example.com", c.Domain)
	}
	if c.MaxAge != 3600 {
		t.Errorf("MaxAge=%d, want 3600", c.MaxAge)
	}
	if !c.Secure {
		t.Error("Secure=false, want true")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite=%v, want Strict", c.SameSite)
	}
	// 属性也须真的出现在原始头里(Set-Cookie 序列化正确)。
	raw := obs.rec.Header().Get("Set-Cookie")
	for _, want := range []string{"Path=/admin", "Domain=example.com", "Max-Age=3600", "Secure", "SameSite=Strict"} {
		if !strings.Contains(raw, want) {
			t.Errorf("Set-Cookie=%q, want it to contain %q", raw, want)
		}
	}
}

// --- 纯函数 --------------------------------------------------------------------
// --- Pure helpers -------------------------------------------------------------

// TestCSRFDefaultNameConstants 锁定三个默认名:它们是与前端约定的公开契约,改动即破坏
// 所有已部署的客户端,故用字面量而非再引用常量本身来断言。
func TestCSRFDefaultNameConstants(t *testing.T) {
	cases := []struct{ got, want, what string }{
		{DefaultCSRFCookieName, "csrf_token", "DefaultCSRFCookieName"},
		{DefaultCSRFHeaderName, "X-CSRF-Token", "DefaultCSRFHeaderName"},
		{DefaultCSRFFieldName, "csrf_token", "DefaultCSRFFieldName"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s=%q, want %q", c.what, c.got, c.want)
		}
	}
}

// TestIsSafeMethod 表驱动锁定安全方法集合(RFC 9110)。误把 POST 判为安全等于关闭防护。
func TestIsSafeMethod(t *testing.T) {
	cases := []struct {
		method string
		want   bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodOptions, true},
		{http.MethodTrace, true},
		{http.MethodPost, false},
		{http.MethodPut, false},
		{http.MethodPatch, false},
		{http.MethodDelete, false},
		{http.MethodConnect, false},
		{"", false},
		{"get", false}, // 大小写敏感:HTTP 方法本身区分大小写
	}
	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			if got := isSafeMethod(c.method); got != c.want {
				t.Errorf("isSafeMethod(%q)=%v, want %v", c.method, got, c.want)
			}
		})
	}
}

// TestCSRFCookieValue 验证 cookie 取值:存在取值、不存在与名字不符都返回空串。
func TestCSRFCookieValue(t *testing.T) {
	cases := []struct {
		name    string
		cookies map[string]string
		lookup  string
		want    string
	}{
		{"present", map[string]string{"csrf_token": "V"}, "csrf_token", "V"},
		{"absent", map[string]string{"other": "V"}, "csrf_token", ""},
		{"no cookies at all", nil, "csrf_token", ""},
		{"name is case-sensitive", map[string]string{"CSRF_TOKEN": "V"}, "csrf_token", ""},
		{"empty value cookie", map[string]string{"csrf_token": ""}, "csrf_token", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/x", nil)
			for k, v := range c.cookies {
				r.AddCookie(&http.Cookie{Name: k, Value: v})
			}
			if got := csrfCookieValue(&Request{Request: r}, c.lookup); got != c.want {
				t.Errorf("csrfCookieValue = %q, want %q", got, c.want)
			}
		})
	}
}

// TestNewCSRFToken 验证 token 非空、互不相同、且只含 base64url 字符。
// 唯一性用 100 个样本:token 复用会让攻击者拿到一个后长期有效;字符集限制保证 token 可
// 直接放进 cookie 值、URL 与 HTML 属性而无需再转义。
func TestNewCSRFToken(t *testing.T) {
	const n = 100
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		tok := newCSRFToken()
		if tok == "" {
			t.Fatal("newCSRFToken returned an empty string")
		}
		if seen[tok] {
			t.Fatalf("newCSRFToken collided on iteration %d: %q", i, tok)
		}
		seen[tok] = true
		if idx := strings.IndexFunc(tok, func(r rune) bool {
			return !strings.ContainsRune(alphabet, r)
		}); idx >= 0 {
			t.Fatalf("token %q has non-base64url byte %q at %d", tok, tok[idx], idx)
		}
	}
	if len(seen) != n {
		t.Errorf("got %d distinct tokens, want %d", len(seen), n)
	}
}

// TestNewCSRFTokenLength 验证 token 长度与 csrfTokenLen 一致(RawURLEncoding 无填充,
// 32 字节 → 43 字符)。长度回退会直接削弱熵。
func TestNewCSRFTokenLength(t *testing.T) {
	want := (csrfTokenLen*8 + 5) / 6 // RawURLEncoding: ceil(bits/6)
	if got := len(newCSRFToken()); got != want {
		t.Errorf("token length=%d, want %d (from csrfTokenLen=%d)", got, want, csrfTokenLen)
	}
}

// TestCSRFTokenFromContext 验证未挂中间件时返回空串(不 panic),挂载后返回本次 token。
func TestCSRFTokenFromContext(t *testing.T) {
	if got := CSRFTokenFromContext(context.Background()); got != "" {
		t.Errorf("bare context token=%q, want \"\"", got)
	}
	// 类型不符的同键值也应安全降级为空串(私有键实际不可能被外部写入,此处仅锁定 API 契约)。
	if got := CSRFTokenFromContext(context.WithValue(context.Background(), csrfTokenKey{}, 42)); got != "" {
		t.Errorf("non-string context value token=%q, want \"\"", got)
	}
	if got := CSRFTokenFromContext(context.WithValue(context.Background(), csrfTokenKey{}, "T")); got != "T" {
		t.Errorf("token=%q, want T", got)
	}
}
