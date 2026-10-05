package ghttp

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"
)

// benchWriter 是可复用的 http.ResponseWriter：丢弃 body，header 每次迭代清空，
// 避免 httptest.ResponseRecorder 的分配污染框架开销。
// benchWriter is a reusable http.ResponseWriter that discards the body and clears headers
// per iteration, keeping httptest.ResponseRecorder allocations out of the measurement.
type benchWriter struct {
	h      http.Header
	status int
	n      int
}

func newBenchWriter() *benchWriter { return &benchWriter{h: make(http.Header, 8)} }

func (w *benchWriter) Header() http.Header { return w.h }

func (w *benchWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}

func (w *benchWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.n += len(p)
	return len(p), nil
}

func (w *benchWriter) reset() {
	clear(w.h)
	w.status = 0
	w.n = 0
}

// benchBody 是可重置的 request body，不产生每次迭代的分配。
// benchBody is a resettable request body with no per-iteration allocation.
type benchBody struct {
	data []byte
	off  int
}

func (b *benchBody) Read(p []byte) (int, error) {
	if b.off >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.off:])
	b.off += n
	return n, nil
}

func (b *benchBody) Close() error { return nil }

func (b *benchBody) reset() { b.off = 0 }

// benchServe 对 handler 重复发起同一请求。body 非 nil 时每次迭代重置并重新挂到请求上
// （框架的 limitBody 会替换 Raw.Body）。
// benchServe fires the same request at h repeatedly. When body is non-nil it is reset and
// re-attached each iteration (the framework's limitBody replaces Raw.Body).
func benchServe(b *testing.B, h http.Handler, method, target string, body *benchBody, hdr http.Header, wantStatus int) {
	b.Helper()
	u, err := url.ParseRequestURI(target)
	if err != nil {
		b.Fatal(err)
	}
	r := &http.Request{Method: method, URL: u, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: hdr, Body: http.NoBody, Host: "example.com", RequestURI: target}
	if r.Header == nil {
		r.Header = http.Header{}
	}
	if body != nil {
		r.ContentLength = int64(len(body.data))
	}
	w := newBenchWriter()

	// 预热并校验状态码，避免基准测错路径
	// warm up and verify the status so the benchmark does not measure the wrong path
	if body != nil {
		body.reset()
		r.Body = body
	}
	h.ServeHTTP(w, r)
	if wantStatus != 0 && w.status != wantStatus {
		b.Fatalf("status = %d, want %d", w.status, wantStatus)
	}

	b.ReportAllocs()
	for b.Loop() {
		w.reset()
		if body != nil {
			body.reset()
			r.Body = body
		}
		h.ServeHTTP(w, r)
	}
}

type benchPing struct {
	Message string `json:"message"`
	OK      bool   `json:"ok"`
}

type benchUser struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

const benchUserJSON = `{"name":"Alice","email":"alice@example.com","age":30}`

func benchMustServer(b *testing.B, mws []Middleware, routes ...Route) *Server {
	b.Helper()
	s := NewServer()
	if len(mws) > 0 {
		s.Use(mws...)
	}
	if err := s.Register(routes...); err != nil {
		b.Fatal(err)
	}
	return s
}

func benchPingGet(path string) Route {
	return Get(path, func(context.Context, RequestOf[NoDataType]) (benchPing, error) {
		return benchPing{Message: "pong", OK: true}, nil
	})
}

var benchRawPayload = []byte(`{"message":"pong","ok":true}`)

func benchRawGet(path string) Route {
	return Raw(http.MethodGet, path, func(_ context.Context, _ *Request, resp *Response) error {
		_, err := resp.Write(benchRawPayload)
		return err
	})
}

// BenchmarkSimpleGET: 无参数 typed handler 返回小 struct。
func BenchmarkSimpleGET(b *testing.B) {
	s := benchMustServer(b, nil, benchPingGet("/ping"))
	benchServe(b, s, http.MethodGet, "/ping", nil, nil, 200)
}

// BenchmarkRawGET: Raw handler 写固定字节，框架最低开销对照。
func BenchmarkRawGET(b *testing.B) {
	s := benchMustServer(b, nil, benchRawGet("/ping"))
	benchServe(b, s, http.MethodGet, "/ping", nil, nil, 200)
}

// BenchmarkStdlibMux: 同等逻辑的 http.ServeMux 对照。
func BenchmarkStdlibMux(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(benchRawPayload)
	})
	benchServe(b, mux, http.MethodGet, "/ping", nil, nil, 200)
}

// BenchmarkGETWithPath: 读取一个路径参数。
func BenchmarkGETWithPath(b *testing.B) {
	s := benchMustServer(b, nil, Get("/users/:id", func(_ context.Context, req RequestOf[NoDataType]) (benchPing, error) {
		id, err := req.PathValue("id").Int64()
		if err != nil {
			return benchPing{}, err
		}
		return benchPing{Message: "user", OK: id > 0}, nil
	}))
	benchServe(b, s, http.MethodGet, "/users/12345", nil, nil, 200)
}

// BenchmarkGETWithQuery: 读取 3 个查询参数。
func BenchmarkGETWithQuery(b *testing.B) {
	s := benchMustServer(b, nil, Get("/search", func(_ context.Context, req RequestOf[NoDataType]) (benchPing, error) {
		q, _ := req.QueryValue("q").String()
		page := req.QueryValue("page").IntOr(1)
		verbose := req.QueryValue("verbose").BoolOr(false)
		return benchPing{Message: q, OK: verbose && page > 0}, nil
	}))
	benchServe(b, s, http.MethodGet, "/search?q=golang&page=2&verbose=true", nil, nil, 200)
}

// BenchmarkPOSTJSON: 小 JSON body 解码 + 输出。
func BenchmarkPOSTJSON(b *testing.B) {
	s := benchMustServer(b, nil, Post("/users", func(ctx context.Context, req RequestOf[benchUser]) (benchUser, error) {
		u, err := req.Data(ctx)
		if err != nil {
			return benchUser{}, err
		}
		u.Age++
		return u, nil
	}))
	hdr := http.Header{"Content-Type": {"application/json"}}
	benchServe(b, s, http.MethodPost, "/users", &benchBody{data: []byte(benchUserJSON)}, hdr, 200)
}

func benchEarlyServer(b *testing.B, eager bool) *Server {
	return benchMustServer(b, nil, Post("/users", func(ctx context.Context, req RequestOf[benchUser]) (benchUser, error) {
		if eager {
			if _, err := req.Data(ctx); err != nil {
				return benchUser{}, err
			}
		}
		if !req.HeaderValue("X-Token").Exists() {
			return benchUser{}, HTTPError{Status: http.StatusForbidden, Message: "forbidden"}
		}
		return req.Data(ctx)
	}))
}

// BenchmarkEarlyReturn: header 校验失败，lazy 不解码 body。
func BenchmarkEarlyReturn(b *testing.B) {
	hdr := http.Header{"Content-Type": {"application/json"}}
	benchServe(b, benchEarlyServer(b, false), http.MethodPost, "/users", &benchBody{data: []byte(benchUserJSON)}, hdr, 403)
}

// BenchmarkEarlyReturnEager: 先解码 body 再校验 header（对照 lazy 收益）。
func BenchmarkEarlyReturnEager(b *testing.B) {
	hdr := http.Header{"Content-Type": {"application/json"}}
	benchServe(b, benchEarlyServer(b, true), http.MethodPost, "/users", &benchBody{data: []byte(benchUserJSON)}, hdr, 403)
}

// BenchmarkMiddlewareStack: Recovery + RequestID + SecureHeaders + Observe（空回调）栈。
func BenchmarkMiddlewareStack(b *testing.B) {
	mws := []Middleware{Recovery(), RequestID(), SecureHeaders(), Observe(func(RequestInfo) {})}
	s := benchMustServer(b, mws, benchPingGet("/ping"))
	benchServe(b, s, http.MethodGet, "/ping", nil, nil, 200)
}

// benchGitHubRoutes 生成约 100 条 GitHub API 风格路由，外加一条 catch-all。
func benchGitHubRoutes() []Route {
	paths := []string{
		"/authorizations", "/authorizations/:id", "/applications/:client_id/tokens/:access_token",
		"/events", "/repos/:owner/:repo/events", "/networks/:owner/:repo/events", "/orgs/:org/events",
		"/users/:user/received_events", "/users/:user/received_events/public", "/users/:user/events",
		"/users/:user/events/public", "/users/:user/events/orgs/:org", "/feeds", "/notifications",
		"/repos/:owner/:repo/notifications", "/notifications/threads/:id",
		"/notifications/threads/:id/subscription", "/repos/:owner/:repo/stargazers", "/users/:user/starred",
		"/user/starred", "/user/starred/:owner/:repo", "/repos/:owner/:repo/subscribers",
		"/users/:user/subscriptions", "/user/subscriptions", "/repos/:owner/:repo/subscription",
		"/user/subscriptions/:owner/:repo", "/users/:user/gists", "/gists", "/gists/public", "/gists/starred",
		"/gists/:id", "/gists/:id/star", "/gists/:id/forks", "/repos/:owner/:repo/git/blobs/:sha",
		"/repos/:owner/:repo/git/blobs", "/repos/:owner/:repo/git/commits/:sha", "/repos/:owner/:repo/git/commits",
		"/repos/:owner/:repo/git/refs", "/repos/:owner/:repo/git/tags/:sha", "/repos/:owner/:repo/git/tags",
		"/repos/:owner/:repo/git/trees/:sha", "/repos/:owner/:repo/git/trees", "/issues", "/user/issues",
		"/orgs/:org/issues", "/repos/:owner/:repo/issues", "/repos/:owner/:repo/issues/:number",
		"/repos/:owner/:repo/assignees", "/repos/:owner/:repo/assignees/:assignee",
		"/repos/:owner/:repo/issues/:number/comments", "/repos/:owner/:repo/issues/:number/events",
		"/repos/:owner/:repo/labels", "/repos/:owner/:repo/labels/:name",
		"/repos/:owner/:repo/issues/:number/labels", "/repos/:owner/:repo/milestones",
		"/repos/:owner/:repo/milestones/:number", "/emojis", "/gitignore/templates", "/gitignore/templates/:name",
		"/meta", "/rate_limit", "/users/:user/orgs", "/user/orgs", "/orgs/:org", "/orgs/:org/members",
		"/orgs/:org/members/:user", "/orgs/:org/public_members", "/orgs/:org/public_members/:user",
		"/orgs/:org/teams", "/teams/:id", "/teams/:id/members", "/teams/:id/members/:user", "/teams/:id/repos",
		"/teams/:id/repos/:owner/:repo", "/user/teams", "/repos/:owner/:repo/pulls", "/repos/:owner/:repo/pulls/:number",
		"/repos/:owner/:repo/pulls/:number/commits", "/repos/:owner/:repo/pulls/:number/files",
		"/repos/:owner/:repo/pulls/:number/merge", "/repos/:owner/:repo/pulls/:number/comments",
		"/user/repos", "/users/:user/repos", "/orgs/:org/repos", "/repositories", "/repos/:owner/:repo",
		"/repos/:owner/:repo/contributors", "/repos/:owner/:repo/languages", "/repos/:owner/:repo/teams",
		"/repos/:owner/:repo/tags", "/repos/:owner/:repo/branches", "/repos/:owner/:repo/branches/:branch",
		"/repos/:owner/:repo/collaborators", "/repos/:owner/:repo/collaborators/:user",
		"/repos/:owner/:repo/comments", "/repos/:owner/:repo/commits", "/repos/:owner/:repo/commits/:sha",
		"/repos/:owner/:repo/readme", "/repos/:owner/:repo/keys", "/repos/:owner/:repo/keys/:id",
		"/repos/:owner/:repo/downloads", "/repos/:owner/:repo/forks", "/repos/:owner/:repo/hooks",
		"/repos/:owner/:repo/hooks/:id", "/search/repositories", "/search/code", "/search/issues",
		"/search/users", "/user", "/users/:user", "/users", "/user/emails", "/users/:user/followers",
		"/user/followers", "/users/:user/following", "/user/following", "/user/following/:user",
		"/users/:user/keys", "/user/keys", "/user/keys/:id",
	}
	routes := make([]Route, 0, len(paths)+1)
	for _, p := range paths {
		routes = append(routes, benchRawGet(p))
	}
	routes = append(routes, benchRawGet("/static/*filepath"))
	return routes
}

// BenchmarkRouting: 约 100 条 GitHub 风格路由下的静态/参数/catch-all 查找。
func BenchmarkRouting(b *testing.B) {
	s := benchMustServer(b, nil, benchGitHubRoutes()...)
	cases := []struct{ name, path string }{
		{"Static", "/gitignore/templates"},
		{"Param1", "/users/octocat"},
		{"Param2", "/repos/octocat/hello-world"},
		{"Param3Deep", "/repos/octocat/hello-world/issues/42/comments"},
		{"CatchAll", "/static/css/site/main.css"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			benchServe(b, s, http.MethodGet, c.path, nil, nil, 200)
		})
	}
}

// BenchmarkNotFound: 未匹配路径返回 404。
func BenchmarkNotFound(b *testing.B) {
	s := benchMustServer(b, nil, benchPingGet("/ping"))
	benchServe(b, s, http.MethodGet, "/missing", nil, nil, 404)
}

// BenchmarkMethodNotAllowed: 路径存在但方法不符返回 405。
func BenchmarkMethodNotAllowed(b *testing.B) {
	s := benchMustServer(b, nil, benchPingGet("/ping"))
	benchServe(b, s, http.MethodDelete, "/ping", nil, nil, 405)
}

// BenchmarkSimpleGETParallel: SimpleGET 的并行版本，每个 goroutine 独立请求与 writer。
func BenchmarkSimpleGETParallel(b *testing.B) {
	s := benchMustServer(b, nil, benchPingGet("/ping"))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		u, _ := url.ParseRequestURI("/ping")
		r := &http.Request{Method: http.MethodGet, URL: u, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
			Header: http.Header{}, Body: http.NoBody, Host: "example.com"}
		w := newBenchWriter()
		for pb.Next() {
			w.reset()
			s.ServeHTTP(w, r)
		}
	})
}
