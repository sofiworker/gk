package ghttp

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// S2:SSE 字段值未过滤换行导致事件注入
// ---------------------------------------------------------------------------

// TestSSE_StripsLineBreaksFromIDAndEvent 锁定 ID/Event 中的换行被剥除。
//
// SSE 协议以换行分隔字段、以空行分隔事件。若把用户可控的 ID 原样写入,
// "1\ndata: INJECTED\n\n" 就能凭空伪造一条事件——用户内容越过了协议边界。
func TestSSE_StripsLineBreaksFromIDAndEvent(t *testing.T) {
	cases := []struct {
		name       string
		msg        SSEMessage
		mustNotHit []string
		mustHit    []string
	}{
		{
			name:       "newline in ID forges an event",
			msg:        SSEMessage{ID: "1\ndata: INJECTED", Data: "real"},
			mustNotHit: []string{"\ndata:INJECTED", "\ndata: INJECTED"},
			mustHit:    []string{"data:real"},
		},
		{
			name:       "newline in Event forges an event",
			msg:        SSEMessage{Event: "ping\ndata: INJECTED", Data: "real"},
			mustNotHit: []string{"\ndata:INJECTED", "\ndata: INJECTED"},
			mustHit:    []string{"data:real"},
		},
		{
			name:       "carriage return in ID",
			msg:        SSEMessage{ID: "1\rdata: INJECTED", Data: "real"},
			mustNotHit: []string{"\rdata:INJECTED", "\rdata: INJECTED"},
			mustHit:    []string{"data:real"},
		},
		{
			name:       "CRLF in ID",
			msg:        SSEMessage{ID: "1\r\n\r\ndata: INJECTED", Data: "real"},
			mustNotHit: []string{"data:INJECTED"},
			mustHit:    []string{"data:real"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			w := NewSSEWriter(&Response{ResponseWriter: rec})
			if err := w.SendMessage(tc.msg); err != nil {
				t.Fatal(err)
			}
			got := rec.Body.String()
			for _, bad := range tc.mustNotHit {
				if strings.Contains(got, bad) {
					t.Errorf("output contains %q (protocol boundary crossed):\n%q", bad, got)
				}
			}
			for _, want := range tc.mustHit {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q:\n%q", want, got)
				}
			}
		})
	}
}

// TestSSE_MultilineDataIsFolded 锁定 Data 的换行被折成多行 data: 字段(协议要求)。
// 单个 \r、单个 \n 与 \r\n 都是合法的 SSE 行终止符,三者都必须折行而不是原样透出。
func TestSSE_MultilineDataIsFolded(t *testing.T) {
	cases := []struct {
		name string
		data string
		want []string
	}{
		{"LF", "line1\nline2", []string{"data:line1\n", "data:line2\n"}},
		{"CR", "line1\rline2", []string{"data:line1\n", "data:line2\n"}},
		{"CRLF counts as one terminator", "line1\r\nline2", []string{"data:line1\n", "data:line2\n"}},
		{"trailing newline", "line1\n", []string{"data:line1\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			w := NewSSEWriter(&Response{ResponseWriter: rec})
			if err := w.SendMessage(SSEMessage{Data: tc.data}); err != nil {
				t.Fatal(err)
			}
			got := rec.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q:\n%q", want, got)
				}
			}
			// 裸 CR 绝不能出现在输出里:它会被解析成额外的行终止符。
			if strings.Contains(got, "\r") {
				t.Errorf("output must not contain a bare CR:\n%q", got)
			}
		})
	}
	// CRLF 必须只折出两行,而不是三行(空行会终止事件)。
	rec := httptest.NewRecorder()
	w := NewSSEWriter(&Response{ResponseWriter: rec})
	if err := w.SendMessage(SSEMessage{Data: "a\r\nb"}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(rec.Body.String(), "data:"); n != 2 {
		t.Errorf("CRLF produced %d data fields, want 2 (it is one terminator, not two)", n)
	}
}

// TestSSE_CommentStripsLineBreaks 锁定注释同样被过滤。
func TestSSE_CommentStripsLineBreaks(t *testing.T) {
	rec := httptest.NewRecorder()
	w := NewSSEWriter(&Response{ResponseWriter: rec})
	if err := w.Comment("keepalive\n\ndata: INJECTED"); err != nil {
		t.Fatal(err)
	}
	got := rec.Body.String()
	// 关键不是 "data: INJECTED" 这串字符是否出现,而是它是否位于【行首】——只有行首才会被
	// 解析成字段。剥除换行后它被压到注释行内部,整行以 ':' 开头,客户端整行忽略。
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "data:") {
			t.Errorf("a comment must not be able to start a data line:\n%q", got)
		}
	}
	if !strings.HasPrefix(got, ":") {
		t.Errorf("the comment must stay one single line starting with ':':\n%q", got)
	}
}

// TestStripSSELineBreaks 直测剥除函数。
func TestStripSSELineBreaks(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a\nb", "ab"},
		{"a\rb", "ab"},
		{"a\r\nb", "ab"},
		{"\n\n", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := stripSSELineBreaks(tc.in); got != tc.want {
			t.Errorf("stripSSELineBreaks(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// S3:CORS 通配源 + 凭据
// ---------------------------------------------------------------------------

// TestCORS_AlwaysVariesOnOrigin 锁定所有分支都发 Vary: Origin。
//
// 缺 Vary 会让共享缓存(CDN)把针对源 A 的响应连同其 ACAO 一起回给源 B,
// 造成跨源缓存污染。被拒的源也必须发:它同样是"响应随 Origin 而变"的证据。
func TestCORS_AlwaysVariesOnOrigin(t *testing.T) {
	cases := []struct {
		name   string
		cfg    CORSConfig
		origin string
	}{
		{"wildcard", CORSConfig{AllowOrigins: []string{"*"}}, "https://a.example"},
		{"allowed origin", CORSConfig{AllowOrigins: []string{"https://a.example"}}, "https://a.example"},
		{"rejected origin", CORSConfig{AllowOrigins: []string{"https://a.example"}}, "https://evil.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			s.Use(CORS(tc.cfg))
			if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			if vary := rec.Header().Values("Vary"); !containsString(vary, "Origin") {
				t.Errorf("Vary=%v, want it to include Origin (else a shared cache poisons cross-origin)", vary)
			}
		})
	}
}

// TestCORS_PreflightRequiresRequestMethodHeader 锁定预检判定同时看方法与请求头。
//
// 只按 method==OPTIONS 判定会把普通的 OPTIONS 探测(如 curl -X OPTIONS)误当预检
// 短路掉,业务的 OPTIONS 路由再也拿不到请求。
func TestCORS_PreflightRequiresRequestMethodHeader(t *testing.T) {
	reached := false
	s := New()
	s.Use(CORS(CORSConfig{AllowOrigins: []string{"https://a.example"}}))
	if err := s.RawHandle(http.MethodOptions, "/x", func(_ context.Context, _ *Request, resp *Response) error {
		reached = true
		resp.WriteHeader(http.StatusNoContent)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 无 Access-Control-Request-Method:不是预检,必须落到业务 handler。
	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "https://a.example")
	s.ServeHTTP(httptest.NewRecorder(), req)
	if !reached {
		t.Error("a plain OPTIONS probe must reach the route, not be short-circuited as preflight")
	}

	// 带 Access-Control-Request-Method:才是预检,由中间件应答。
	reached = false
	req2 := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req2.Header.Set("Origin", "https://a.example")
	req2.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, req2)
	if reached {
		t.Error("a real preflight must be answered by the middleware")
	}
	if rec2.Code != http.StatusNoContent {
		t.Errorf("preflight status=%d, want 204", rec2.Code)
	}
}

// ---------------------------------------------------------------------------
// S5:panic 导致 in-flight 计数永久泄漏
// ---------------------------------------------------------------------------

// TestMetrics_InFlightReleasedOnPanic 锁定 panic 后 in-flight 归零。
//
// 计数原先在 next 返回后才自减,panic 会跳过它。每次 panic 永久 +1,in-flight
// 单调上涨,基于它的告警与自动扩缩容全部失效——比丢一次埋点严重得多。
func TestMetrics_InFlightReleasedOnPanic(t *testing.T) {
	reg := NewMetrics()
	s := New()
	s.Use(reg.Middleware())
	if err := s.RawHandle(http.MethodGet, "/p", func(context.Context, *Request, *Response) error {
		panic("boom")
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))
	}
	body := scrapeMetrics(t, s, reg)
	// in-flight 按路由带标签。panic 的那条路由必须归零;抓取自身那条会是 1(正在处理中)。
	found := false
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "http_requests_in_flight{") || !strings.Contains(line, `route="/p"`) {
			continue
		}
		found = true
		if !strings.HasSuffix(line, " 0") {
			t.Errorf("in-flight leaked after panics: %q", line)
		}
	}
	if !found {
		t.Errorf("no in-flight sample for the panicking route:\n%s", extractLines(body, "in_flight"))
	}
}

// TestMetrics_PanicCountedAs500 锁定 panic 被计成 500 而不是消失或计成 200。
func TestMetrics_PanicCountedAs500(t *testing.T) {
	reg := NewMetrics()
	s := New()
	s.Use(reg.Middleware())
	if err := s.RawHandle(http.MethodGet, "/p", func(context.Context, *Request, *Response) error {
		panic("boom")
	}); err != nil {
		t.Fatal(err)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))

	body := scrapeMetrics(t, s, reg)
	if !strings.Contains(body, `code="500"`) {
		t.Errorf("a panic must be counted as 500, got:\n%s", extractLines(body, "by_code"))
	}
	if strings.Contains(body, `code="200"`) {
		t.Errorf("a panic must never be counted as 200:\n%s", extractLines(body, "by_code"))
	}
}

// TestMetrics_ErrorStatusInferred 锁定返回错误时按错误映射的状态码计数。
func TestMetrics_ErrorStatusInferred(t *testing.T) {
	reg := NewMetrics()
	s := New()
	s.Use(reg.Middleware())
	if err := s.RawHandle(http.MethodGet, "/e", func(context.Context, *Request, *Response) error {
		return ErrInvalidInput
	}); err != nil {
		t.Fatal(err)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/e", nil))

	body := scrapeMetrics(t, s, reg)
	if !strings.Contains(body, `code="400"`) {
		t.Errorf("a returned error must be counted with its mapped status, got:\n%s", extractLines(body, "by_code"))
	}
}

// ---------------------------------------------------------------------------
// M6:Prometheus 输出格式与标签基数
// ---------------------------------------------------------------------------

// TestMetrics_ForgedMethodCollapsesToOther 锁定方法标签基数有界。
//
// 直接把 req.Method 当标签值意味着任意客户端可用随机方法名无限增长指标基数,
// 撑爆抓取端内存——一条无需认证的 DoS 路径。
func TestMetrics_ForgedMethodCollapsesToOther(t *testing.T) {
	reg := NewMetrics()
	s := New()
	s.Use(reg.Middleware())
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 用裸报文发出非标准方法,绕过 httptest 的校验。
	for _, m := range []string{"FOO", "BAR", "XYZZY"} {
		raw := m + " /x HTTP/1.1\r\nHost: svc.example\r\n\r\n"
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
		if err != nil {
			t.Fatal(err)
		}
		s.ServeHTTP(httptest.NewRecorder(), req)
	}
	body := scrapeMetrics(t, s, reg)
	for _, m := range []string{"FOO", "BAR", "XYZZY"} {
		if strings.Contains(body, `method="`+m+`"`) {
			t.Errorf("forged method %q became a label value (unbounded cardinality):\n%s", m, extractLines(body, "method="))
		}
	}
	if !strings.Contains(body, `method="OTHER"`) {
		t.Errorf("forged methods must collapse to OTHER:\n%s", extractLines(body, "method="))
	}
}

// TestNormalizeMetricsMethod 直测方法归一。
func TestNormalizeMetricsMethod(t *testing.T) {
	for _, m := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
		http.MethodConnect, http.MethodTrace,
	} {
		if got := normalizeMetricsMethod(m); got != m {
			t.Errorf("normalizeMetricsMethod(%q)=%q, want it unchanged", m, got)
		}
	}
	for _, m := range []string{"FOO", "", "get", "PROPFIND", strings.Repeat("A", 100)} {
		if got := normalizeMetricsMethod(m); got != "OTHER" {
			t.Errorf("normalizeMetricsMethod(%q)=%q, want OTHER", m, got)
		}
	}
}

// TestMetrics_ExpositionFormatIsValid 锁定输出符合 Prometheus 文本格式。
//
// 同一 family 的样本必须连续、HELP/TYPE 各只出现一次、同 family 的标签键集合一致。
// 交错输出会让抓取端直接丢弃整份指标——指标看似"有"其实全无。
func TestMetrics_ExpositionFormatIsValid(t *testing.T) {
	reg := NewMetrics()
	s := New()
	s.Use(reg.Middleware())
	for _, p := range []string{"/a", "/b"} {
		path := p
		if err := s.RawHandle(http.MethodGet, path, func(_ context.Context, _ *Request, resp *Response) error {
			resp.WriteHeader(http.StatusOK)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.RawHandle(http.MethodPost, path, func(context.Context, *Request, *Response) error {
			return ErrInvalidInput
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"/a", "/b"} {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, p, nil))
	}

	body := scrapeMetrics(t, s, reg)

	// HELP/TYPE 每个 family 只能出现一次。
	helpSeen := map[string]int{}
	typeSeen := map[string]int{}
	// 记录每个 family 首次与末次出现的行号,确认样本连续。
	first := map[string]int{}
	last := map[string]int{}
	for i, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "# HELP "):
			helpSeen[strings.Fields(line)[2]]++
		case strings.HasPrefix(line, "# TYPE "):
			typeSeen[strings.Fields(line)[2]]++
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			name := line
			if i := strings.IndexAny(name, "{ "); i >= 0 {
				name = name[:i]
			}
			// 直方图的 _bucket/_sum/_count 属于其基础 family。
			base := name
			for _, suffix := range []string{"_bucket", "_sum", "_count"} {
				base = strings.TrimSuffix(base, suffix)
			}
			if _, ok := first[base]; !ok {
				first[base] = i
			}
			last[base] = i
		}
	}
	for name, n := range helpSeen {
		if n != 1 {
			t.Errorf("family %q has %d HELP lines, want exactly 1", name, n)
		}
	}
	for name, n := range typeSeen {
		if n != 1 {
			t.Errorf("family %q has %d TYPE lines, want exactly 1", name, n)
		}
	}
	// 检查 family 区间不重叠(等价于样本连续)。
	for a := range first {
		for b := range first {
			if a == b {
				continue
			}
			if first[a] < first[b] && last[a] > first[b] {
				t.Errorf("families %q and %q interleave: a scraper drops the whole payload", a, b)
			}
		}
	}
	if len(helpSeen) == 0 {
		t.Fatal("no metrics emitted")
	}
}

// TestMetrics_PerStatusUsesOwnFamily 锁定按状态计数不再挂在 http_requests_total 上。
//
// 同一 family 的样本必须有相同的标签键集合。把 code 只加在部分样本上是非法暴露格式;
// 因此按状态的计数独立成 http_requests_by_code_total。
func TestMetrics_PerStatusUsesOwnFamily(t *testing.T) {
	reg := NewMetrics()
	s := New()
	s.Use(reg.Middleware())
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	body := scrapeMetrics(t, s, reg)
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "http_requests_total{") && strings.Contains(line, "code=") {
			t.Errorf("http_requests_total must not carry a code label (inconsistent label keys): %q", line)
		}
	}
	if !strings.Contains(body, "http_requests_by_code_total{") {
		t.Errorf("per-status counts must live in their own family:\n%s", body)
	}
	// 直方图必须带 _count,否则抓取端算不出平均值。
	if !strings.Contains(body, "http_request_duration_seconds_count") {
		t.Errorf("a histogram must expose _count:\n%s", body)
	}
}

// TestEscapePromLabel 锁定标签值转义。路由模板可含引号/反斜杠,未转义会破坏整行语法。
func TestEscapePromLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/users", "/users"},
		{`a"b`, `a\"b`},
		{`a\b`, `a\\b`},
		{"a\nb", `a\nb`},
		{"", ""},
	}
	for _, tc := range cases {
		if got := escapePromLabel(tc.in); got != tc.want {
			t.Errorf("escapePromLabel(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// S6:Logger 把被中间件短路的请求记成 200
// ---------------------------------------------------------------------------

// TestLogger_InfersStatusWhenNotWritten 锁定未提交响应时日志状态码由错误推断。
//
// Logger 原先读 resp.Status(),而未写响应时它是 0,被当成"零值即 200"。于是 CSRF 拒绝、
// 限流、鉴权失败在访问日志里全是 200——日志成了安全事件的盲区。
func TestLogger_InfersStatusWhenNotWritten(t *testing.T) {
	cases := []struct {
		name    string
		handler RawHandlerFunc
		want    int
	}{
		{
			name:    "rate limit maps to 429",
			handler: func(context.Context, *Request, *Response) error { return ErrRateLimitExceeded },
			want:    http.StatusTooManyRequests,
		},
		{
			name:    "csrf failure maps to 403",
			handler: func(context.Context, *Request, *Response) error { return ErrCSRFTokenInvalid },
			want:    http.StatusForbidden,
		},
		{
			name:    "invalid input maps to 400",
			handler: func(context.Context, *Request, *Response) error { return ErrInvalidInput },
			want:    http.StatusBadRequest,
		},
		{
			name:    "panic maps to 500",
			handler: func(context.Context, *Request, *Response) error { panic("boom") },
			want:    http.StatusInternalServerError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs []AccessLog
			s := New()
			s.Use(LoggerWith(func(l AccessLog) { logs = append(logs, l) }))
			if err := s.RawHandle(http.MethodGet, "/x", tc.handler); err != nil {
				t.Fatal(err)
			}
			s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

			if len(logs) != 1 {
				t.Fatalf("got %d log entries, want exactly 1 (a short-circuited request must still be logged)", len(logs))
			}
			if logs[0].Status != tc.want {
				t.Errorf("logged status=%d, want %d (a short-circuited request must not read as 200)",
					logs[0].Status, tc.want)
			}
			if logs[0].Status == http.StatusOK {
				t.Error("logged status must never be 200 here: the response was never written")
			}
		})
	}
}

// TestLogger_WrittenStatusWins 确认下游已提交时日志沿用真实状态码。
func TestLogger_WrittenStatusWins(t *testing.T) {
	var logs []AccessLog
	s := New()
	s.Use(LoggerWith(func(l AccessLog) { logs = append(logs, l) }))
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusCreated)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if len(logs) != 1 {
		t.Fatalf("got %d log entries, want 1", len(logs))
	}
	if logs[0].Status != http.StatusCreated {
		t.Errorf("logged status=%d, want the written 201", logs[0].Status)
	}
}

// TestLogger_ErrorIsCarried 锁定短路请求的 error 也被带进日志(否则无从归因)。
func TestLogger_ErrorIsCarried(t *testing.T) {
	var logs []AccessLog
	s := New()
	s.Use(LoggerWith(func(l AccessLog) { logs = append(logs, l) }))
	if err := s.RawHandle(http.MethodGet, "/x", func(context.Context, *Request, *Response) error {
		return ErrCSRFTokenInvalid
	}); err != nil {
		t.Fatal(err)
	}
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if len(logs) != 1 {
		t.Fatalf("got %d log entries, want 1", len(logs))
	}
	if !errors.Is(logs[0].Err, ErrCSRFTokenInvalid) {
		t.Errorf("logged Err=%v, want errors.Is(err, ErrCSRFTokenInvalid)", logs[0].Err)
	}
}

// ---------------------------------------------------------------------------
// S8:ClientIP 在首个转发头失败后继续回退到更弱的头
// ---------------------------------------------------------------------------

// TestClientIP_StopsAfterFirstPresentHeader 锁定不再穿透到更弱的头。
//
// 反代通常只写 X-Forwarded-For。攻击者自带一个 X-Real-IP,当 XFF 里全是可信代理时
// 循环会继续尝试 X-Real-IP 并采信客户端伪造值。首个存在的头就是唯一裁决者。
func TestClientIP_StopsAfterFirstPresentHeader(t *testing.T) {
	s := New(WithTrustedProxies("10.0.0.0/8"))
	var got string
	if err := s.RawHandle(http.MethodGet, "/ip", func(_ context.Context, req *Request, resp *Response) error {
		got = req.ClientIP()
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "10.1.2.3:41234"
	// XFF 只含可信代理地址:按策略"没有可信的客户端 IP",应止步于直连地址。
	req.Header.Set("X-Forwarded-For", "10.9.9.9")
	// 攻击者附带的更弱的头,必须被忽略。
	req.Header.Set("X-Real-IP", "6.6.6.6")
	s.ServeHTTP(httptest.NewRecorder(), req)

	if got == "6.6.6.6" {
		t.Error("ClientIP fell through to a weaker forged header after the first one failed")
	}
	if got != "10.1.2.3" {
		t.Errorf("ClientIP=%q, want the direct peer 10.1.2.3", got)
	}
}

// TestClientIP_FirstHeaderStillHonoured 确认加固没有破坏正常解析。
func TestClientIP_FirstHeaderStillHonoured(t *testing.T) {
	cases := []struct {
		name   string
		hdr    map[string]string
		remote string
		want   string
	}{
		{
			name:   "XFF yields the client",
			hdr:    map[string]string{"X-Forwarded-For": "1.2.3.4, 10.9.9.9"},
			remote: "10.1.2.3:1",
			want:   "1.2.3.4",
		},
		{
			name:   "X-Real-IP used when XFF absent",
			hdr:    map[string]string{"X-Real-IP": "1.2.3.4"},
			remote: "10.1.2.3:1",
			want:   "1.2.3.4",
		},
		{
			name:   "no header falls back to peer",
			hdr:    nil,
			remote: "10.1.2.3:1",
			want:   "10.1.2.3",
		},
		{
			name:   "untrusted peer ignores headers entirely",
			hdr:    map[string]string{"X-Forwarded-For": "1.2.3.4"},
			remote: "203.0.113.9:1",
			want:   "203.0.113.9",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New(WithTrustedProxies("10.0.0.0/8"))
			var got string
			if err := s.RawHandle(http.MethodGet, "/ip", func(_ context.Context, req *Request, resp *Response) error {
				got = req.ClientIP()
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/ip", nil)
			req.RemoteAddr = tc.remote
			for k, v := range tc.hdr {
				req.Header.Set(k, v)
			}
			s.ServeHTTP(httptest.NewRecorder(), req)
			if got != tc.want {
				t.Errorf("ClientIP=%q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 轻微项:Use 在服务开始后静默失效 / RequestID 字符集
// ---------------------------------------------------------------------------

// TestUse_AfterServingIsRejectedNotSilent 锁定服务开始后 Use 不再静默无效。
//
// 中间件链在首个请求时被折叠,此后追加的中间件永不生效。静默 no-op 是最坏的失败方式:
// 代码看起来装上了鉴权,实际完全没跑。
func TestUse_AfterServingIsRejectedNotSilent(t *testing.T) {
	s := New()
	if err := s.RawHandle(http.MethodGet, "/x", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 首个请求折叠中间件链。
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if !s.mux.chainBuilt.Load() {
		t.Fatal("the first request must fold the chain")
	}

	// 此后追加的中间件不得被静默接受进 m.mws(那会造成"以为装上了"的假象)。
	before := len(s.mux.mws)
	s.Use(func(next Handler) Handler { return next })
	if len(s.mux.mws) != before {
		t.Error("middleware appended after serving must not be silently accepted")
	}
}

// TestRequestID_RejectsUnsafeCharset 锁定非法字符集的请求 ID 被替换。
//
// 请求 ID 同时进响应头与访问日志:含 CR/LF 的值是响应头注入与日志行伪造的经典载体。
func TestRequestID_RejectsUnsafeCharset(t *testing.T) {
	cases := []struct {
		name     string
		id       string
		wantEcho bool
	}{
		{"clean hex", "abc123DEF", true},
		{"with dashes and dots", "req-1.2_3", true},
		{"CRLF injection", "abc\r\nX-Injected: 1", false},
		{"newline", "abc\ndef", false},
		{"space", "abc def", false},
		{"control char", "abc\x00def", false},
		{"non-ascii", "abc£def", false},
		{"empty", "", false},
		{"too long", strings.Repeat("a", DefaultMaxRequestIDLength+1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			s.Use(RequestID())
			var seen string
			if err := s.RawHandle(http.MethodGet, "/x", func(ctx context.Context, _ *Request, resp *Response) error {
				seen = RequestIDFromContext(ctx)
				resp.WriteHeader(http.StatusOK)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.id != "" {
				// 直接写 map 以绕过 Header.Set 的校验,模拟真实网络输入。键必须用规范形式,
				// 否则 Header.Get 读不到(Go 把 X-Request-ID 规范化成 X-Request-Id)。
				req.Header[http.CanonicalHeaderKey(HeaderRequestID)] = []string{tc.id}
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			echoed := rec.Header().Get(HeaderRequestID)
			if tc.wantEcho {
				if echoed != tc.id {
					t.Errorf("valid id %q was replaced by %q", tc.id, echoed)
				}
			} else {
				if echoed == tc.id {
					t.Errorf("unsafe id %q was echoed verbatim", tc.id)
				}
				if !isValidRequestID(echoed) {
					t.Errorf("the generated replacement %q is itself invalid", echoed)
				}
			}
			if seen != echoed {
				t.Errorf("context id %q differs from the echoed header %q", seen, echoed)
			}
		})
	}
}

// TestIsValidRequestID 直测校验函数。
func TestIsValidRequestID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"abc123", true},
		{"a-b_c.d", true},
		{"A", true},
		{"", false},
		{"a b", false},
		{"a\nb", false},
		{"a\rb", false},
		{"a;b", false},
		{"a/b", false},
		{strings.Repeat("a", DefaultMaxRequestIDLength), true},
		{strings.Repeat("a", DefaultMaxRequestIDLength+1), false},
	}
	for _, tc := range cases {
		if got := isValidRequestID(tc.in); got != tc.want {
			t.Errorf("isValidRequestID(%q)=%v, want %v", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

// scrapeMetrics 抓取一次指标输出。
// scrapeMetrics 在【产生数据的同一个 server】上暴露 /metrics 并抓取一次。
// 另建一个 server 抓不到任何数据:注册表按路由聚合,数据随该 server 的请求产生。
func scrapeMetrics(t *testing.T, s *Server, reg *MetricsRegistry) string {
	t.Helper()
	if err := s.RawHandle(http.MethodGet, "/__metrics", reg.Handler()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status=%d", rec.Code)
	}
	return rec.Body.String()
}

// extractLines 从指标输出里挑出含 needle 的行,便于失败信息聚焦。
func extractLines(body, needle string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
