package ghttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sofiworker/gk/gerr"
	"github.com/sofiworker/gk/ghttp/wire"
)

// apiNoData 是不读取 body 的 typed handler 工厂。
// apiNoData builds a typed handler that never reads the body.
func apiNoData[O any](v O) Endpoint[NoDataType, O] {
	return func(context.Context, RequestOf[NoDataType]) (O, error) { return v, nil }
}

// TestAPIMethodConstructors 测试各 HTTP 方法快捷构造函数注册到正确的方法。
// TestAPIMethodConstructors tests the HTTP method shortcuts register the right method.
func TestAPIMethodConstructors(t *testing.T) {
	h := apiNoData("ok")
	tests := []struct {
		route  Route
		method string
	}{
		{Get("/x", h), http.MethodGet},
		{Post("/x", h), http.MethodPost},
		{Put("/x", h), http.MethodPut},
		{Patch("/x", h), http.MethodPatch},
		{Delete("/x", h), http.MethodDelete},
		{Head("/x", h), http.MethodHead},
		{Options("/x", h), http.MethodOptions},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			if tt.route.Method != tt.method || tt.route.Err() != nil {
				t.Fatalf("route = %+v", tt.route)
			}
			rec := coreServe(t, tt.route, tt.method, "/x", "")
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d", rec.Code)
			}
		})
	}
}

// TestAPIConstructorErrors 测试构造期错误。
// TestAPIConstructorErrors tests construction-time errors.
func TestAPIConstructorErrors(t *testing.T) {
	tests := []struct {
		name  string
		route Route
	}{
		{"nil endpoint", Get[NoDataType, string]("/x", nil)},
		{"bad body type", Post("/x", func(context.Context, RequestOf[int]) (string, error) { return "", nil })},
		{"bad result type", Get("/x", func(context.Context, RequestOf[NoDataType]) (chan int, error) { return nil, nil })},
		{"nil action", HandleAction[NoDataType](http.MethodPost, "/x", nil)},
		{"bad action body", HandleAction(http.MethodPost, "/x", func(context.Context, RequestOf[string]) error { return nil })},
		{"nil procedure", HandleProcedure(http.MethodPost, "/x", nil)},
		{"nil raw", Raw(http.MethodGet, "/x", nil)},
		{"empty path", Raw(http.MethodGet, "", coreText("x"))},
		{"bad method", Raw("GE T", "/x", coreText("x"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.route.Err() == nil {
				t.Fatal("expected construction error")
			}
			if err := NewServer().Register(tt.route); err == nil {
				t.Fatal("expected Register error")
			}
		})
	}
}

// TestAPIActionAndProcedure 测试 Action/Procedure：成功 204，失败走错误链，Action 可读取 body。
// TestAPIActionAndProcedure tests Action/Procedure: 204 on success, errors go through the
// error chain, and actions can read the body.
func TestAPIActionAndProcedure(t *testing.T) {
	var got string
	action := HandleAction(http.MethodPost, "/a", func(ctx context.Context, req RequestOf[coreUser]) error {
		u, err := req.Data(ctx)
		got = u.Name
		return err
	})
	proc := HandleProcedure(http.MethodPost, "/p", func(context.Context) error { return ErrForbidden })
	s := coreNewServer(t, nil, action, proc)

	rec := coreDo(s, http.MethodPost, "/a", `{"name":"neo"}`, "Content-Type", "application/json")
	if rec.Code != http.StatusNoContent || got != "neo" || rec.Body.Len() != 0 {
		t.Errorf("action: code=%d got=%q body=%q", rec.Code, got, rec.Body.String())
	}
	if rec = coreDo(s, http.MethodPost, "/a", `{"bad":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("action bad body: code=%d", rec.Code)
	}
	if rec = coreDo(s, http.MethodPost, "/p", ""); rec.Code != http.StatusForbidden {
		t.Errorf("procedure error: code=%d", rec.Code)
	}

	err := NewServer().Register(HandleAction(http.MethodPost, "/x",
		func(context.Context, RequestOf[NoDataType]) error { return nil }, WithOutput(TextOutput())))
	if err == nil {
		t.Error("WithOutput on an action should fail")
	}
}

// TestCodecInputs 测试 XMLInput、InputFunc 与 JSONInput 的成功与错误映射。
// TestCodecInputs tests XMLInput, InputFunc and JSONInput success and error mapping.
func TestCodecInputs(t *testing.T) {
	echo := func(ctx context.Context, req RequestOf[coreUser]) (string, error) {
		u, err := req.Data(ctx)
		return u.Name, err
	}
	custom := InputFunc[coreUser](func(_ context.Context, req *Request) (coreUser, error) {
		return coreUser{Name: req.Raw.Header.Get("X-Name")}, nil
	})
	s := coreNewServer(t, nil,
		Post("/xml", echo, WithInput(XMLInput[coreUser]())),
		Post("/json", echo, WithInput(JSONInput[coreUser]())),
		Post("/custom", echo, WithInput[coreUser](custom)),
	)

	tests := []struct {
		name   string
		path   string
		body   string
		header []string
		status int
		body2  string
	}{
		{"xml ok", "/xml", `<coreUser><name>neo</name></coreUser>`, []string{"Content-Type", "application/xml"}, 200, `"neo"`},
		{"xml text/xml", "/xml", `<u><name>t</name></u>`, []string{"Content-Type", "text/xml; charset=utf-8"}, 200, `"t"`},
		{"xml wrong type", "/xml", `<u/>`, []string{"Content-Type", "application/json"}, 415, ""},
		{"xml malformed", "/xml", `<u><name>`, []string{"Content-Type", "application/xml"}, 400, ""},
		{"xml empty", "/xml", ``, nil, 400, ""},
		{"json ok", "/json", `{"name":"j"}`, nil, 200, `"j"`},
		{"custom", "/custom", ``, []string{"X-Name", "c"}, 200, `"c"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := coreDo(s, http.MethodPost, tt.path, tt.body, tt.header...)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.status, rec.Body.String())
			}
			if tt.body2 != "" && rec.Body.String() != tt.body2 {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.body2)
			}
		})
	}
}

// TestCodecOutputs 测试 JSON/XML/Text/OutputFunc 输出及其 Content-Type。
// TestCodecOutputs tests JSON/XML/Text/OutputFunc outputs and their Content-Type.
func TestCodecOutputs(t *testing.T) {
	custom := OutputFunc[int](func(_ context.Context, resp *Response, v int) error {
		resp.WriteHeader(http.StatusAccepted)
		_, err := fmt.Fprintf(resp, "n=%d", v)
		return err
	})
	s := coreNewServer(t, nil,
		Get("/json", apiNoData(coreUser{Name: "a"}), WithOutput(JSONOutput[coreUser]())),
		Get("/xml", apiNoData(coreUser{Name: "a"}), WithOutput(XMLOutput[coreUser]())),
		Get("/text", apiNoData("hi"), WithOutput(TextOutput())),
		Get("/custom", apiNoData(7), WithOutput[int](custom)),
		Get("/xmlbad", apiNoData(map[string]int{"a": 1}), WithOutput(XMLOutput[map[string]int]())),
	)
	tests := []struct {
		path, ct, body string
		status         int
	}{
		{"/json", "application/json; charset=utf-8", `{"name":"a"}`, 200},
		{"/xml", "application/xml; charset=utf-8", `<coreUser><name>a</name></coreUser>`, 200},
		{"/text", "text/plain; charset=utf-8", "hi", 200},
		{"/custom", "", "n=7", 202},
		{"/xmlbad", "application/json; charset=utf-8", "", 500},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := coreDo(s, http.MethodGet, tt.path, "")
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if tt.ct != "" && rec.Header().Get("Content-Type") != tt.ct {
				t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
			}
			if tt.body != "" && rec.Body.String() != tt.body {
				t.Errorf("body = %q", rec.Body.String())
			}
		})
	}
}

// TestCodecTypeMismatch 测试 WithInput/WithOutput 类型不匹配与 nil 值在注册期报错。
// TestCodecTypeMismatch tests WithInput/WithOutput type mismatches and nils fail at registration.
func TestCodecTypeMismatch(t *testing.T) {
	h := func(context.Context, RequestOf[coreUser]) (string, error) { return "", nil }
	tests := []Route{
		Post("/a", h, WithInput(XMLInput[struct{ X int }]())),
		Post("/b", h, WithOutput(JSONOutput[int]())),
		Post("/c", h, WithInput[coreUser](nil)),
		Post("/d", h, WithOutput[string](nil)),
		Raw(http.MethodGet, "/e", coreText("x"), WithInput(JSONInput[coreUser]())),
	}
	for i, r := range tests {
		if err := NewServer().Register(r); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
	if err := NewServer().With(WithOutput(TextOutput())).Register(Get("/x", apiNoData("x"))); err == nil {
		t.Error("server-level WithOutput should fail")
	}
}

// TestReplyDispatch 测试 typed handler 返回各响应类型（值与指针）时按其语义写出。
// TestReplyDispatch tests typed handlers returning reply types (values and pointers).
func TestReplyDispatch(t *testing.T) {
	modTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	file := func() FileReply {
		return FileReply{Reader: strings.NewReader("0123456789"), Name: "a.txt", ModTime: modTime}
	}
	s := coreNewServer(t, nil,
		Get("/reply", apiNoData(Reply[coreUser]{Body: coreUser{Name: "r"}, Status: http.StatusCreated,
			Headers: http.Header{"X-A": {"1", "2"}}, Cookies: []*http.Cookie{{Name: "s", Value: "v"}, nil}})),
		Get("/reply-default", apiNoData(Reply[int]{Body: 1})),
		Get("/reply-204", apiNoData(Reply[coreUser]{Status: http.StatusNoContent, Body: coreUser{Name: "x"}})),
		Get("/reply-bad", apiNoData(Reply[int]{Status: 42})),
		Get("/reply-ptr", apiNoData(&Reply[int]{Body: 2, Status: http.StatusAccepted})),
		Get("/reply-nilptr", apiNoData[*Reply[int]](nil)),
		Get("/file", func(context.Context, RequestOf[NoDataType]) (FileReply, error) { return file(), nil }),
		Get("/stream", apiNoData(&StreamReply{Writer: func(w io.Writer) error {
			_, err := w.Write([]byte("s"))
			return err
		}})),
		Get("/redirect", apiNoData(RedirectReply{URL: "/to"})),
		Get("/redirect-empty", apiNoData(RedirectReply{})),
		Get("/nocontent", apiNoData(&NoContentReply{})),
	)

	tests := []struct {
		path   string
		header []string
		status int
		check  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{"/reply", nil, 201, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if got := rec.Header().Values("X-A"); !reflect.DeepEqual(got, []string{"1", "2"}) {
				t.Errorf("X-A = %v", got)
			}
			if rec.Header().Get("Set-Cookie") != "s=v" || rec.Body.String() != `{"name":"r"}` {
				t.Errorf("cookie=%q body=%q", rec.Header().Get("Set-Cookie"), rec.Body.String())
			}
		}},
		{"/reply-default", nil, 200, nil},
		{"/reply-204", nil, 204, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if rec.Body.Len() != 0 {
				t.Errorf("204 with body %q", rec.Body.String())
			}
		}},
		{"/reply-bad", nil, 500, nil},
		{"/reply-ptr", nil, 202, nil},
		{"/reply-nilptr", nil, 200, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if rec.Body.String() != "null" {
				t.Errorf("body = %q", rec.Body.String())
			}
		}},
		{"/file", nil, 200, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if rec.Body.String() != "0123456789" || !strings.Contains(rec.Header().Get("Content-Disposition"), "a.txt") {
				t.Errorf("body=%q cd=%q", rec.Body.String(), rec.Header().Get("Content-Disposition"))
			}
		}},
		{"/file", []string{"Range", "bytes=2-4"}, 206, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if rec.Body.String() != "234" {
				t.Errorf("range body = %q", rec.Body.String())
			}
		}},
		{"/file", []string{"If-Modified-Since", modTime.Add(time.Hour).Format(http.TimeFormat)}, 304, nil},
		{"/stream", nil, 200, nil},
		{"/redirect", nil, 302, func(t *testing.T, rec *httptest.ResponseRecorder) {
			if rec.Header().Get("Location") != "/to" {
				t.Errorf("Location = %q", rec.Header().Get("Location"))
			}
		}},
		{"/redirect-empty", nil, 500, nil},
		{"/nocontent", nil, 204, nil},
	}
	for _, tt := range tests {
		t.Run(tt.path+strings.Join(tt.header, ":"), func(t *testing.T) {
			rec := coreDo(s, http.MethodGet, tt.path, "", tt.header...)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.status, rec.Body.String())
			}
			if tt.check != nil {
				tt.check(t, rec)
			}
		})
	}
}

// TestErrorMappingKinds 测试 StatusFromError 与 ErrorResponseOf 对各类错误的映射与脱敏。
// TestErrorMappingKinds tests StatusFromError and ErrorResponseOf mapping and redaction.
func TestErrorMappingKinds(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		msg    string
		code   string
	}{
		{"nil", nil, 200, "OK", ""},
		{"http error", NotFound("no user"), 404, "no user", ""},
		{"wrapped http error", fmt.Errorf("svc: %w", Forbidden("nope")), 403, "nope", ""},
		{"invalid input", fmt.Errorf("%w: bad id", ErrInvalidInput), 400, "invalid input", ""},
		{"max bytes", fmt.Errorf("read: %w", &http.MaxBytesError{Limit: 1}), 413, "Request Entity Too Large", ""},
		{"invalid format", fmt.Errorf("x: %w", wire.ErrInvalidFormat), 400, "Bad Request", ""},
		{"gerr invalid", gerr.New("secret detail", gerr.WithKind(gerr.KindInvalid), gerr.WithCode("E1")), 400, "Bad Request", "E1"},
		{"gerr not found", gerr.New("x", gerr.WithKind(gerr.KindNotFound)), 404, "Not Found", ""},
		{"gerr conflict", gerr.New("x", gerr.WithKind(gerr.KindConflict)), 409, "Conflict", ""},
		{"gerr permission", gerr.New("x", gerr.WithKind(gerr.KindPermission)), 403, "Forbidden", ""},
		{"gerr unavailable", gerr.New("x", gerr.WithKind(gerr.KindUnavailable)), 503, "Service Unavailable", ""},
		{"gerr timeout", gerr.New("x", gerr.WithKind(gerr.KindTimeout)), 504, "Gateway Timeout", ""},
		{"gerr canceled", gerr.New("x", gerr.WithKind(gerr.KindCanceled)), 499, "client closed request", ""},
		{"gerr internal", gerr.New("x", gerr.WithKind(gerr.KindInternal)), 500, "Internal Server Error", ""},
		{"gerr no kind", gerr.New("secret"), 500, "Internal Server Error", ""},
		{"deadline", fmt.Errorf("db: %w", context.DeadlineExceeded), 504, "Gateway Timeout", ""},
		{"canceled", context.Canceled, 499, "client closed request", ""},
		{"plain", errors.New("password=hunter2"), 500, "Internal Server Error", ""},
		{"custom status", HTTPError{Status: 599}, 599, "error", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatusFromError(tt.err); got != tt.status {
				t.Fatalf("status = %d, want %d", got, tt.status)
			}
			er := ErrorResponseOf(tt.err)
			if er.Error != tt.msg || er.Code != tt.code || er.Status != tt.status {
				t.Errorf("response = %+v, want msg %q code %q", er, tt.msg, tt.code)
			}
		})
	}
	if !errors.Is(fmt.Errorf("x: %w", NotFound("a")), ErrNotFound) {
		t.Error("errors.Is should match by status")
	}
	if errors.Is(NotFound("a"), ErrForbidden) {
		t.Error("different statuses must not match")
	}
}

// TestGroupOptionsAndSnapshots 测试 Group 的选项、前缀、快照与 With 语义。
// TestGroupOptionsAndSnapshots tests group options, prefixes, snapshots and With.
func TestGroupOptionsAndSnapshots(t *testing.T) {
	var trace []string
	s := NewServer()
	s.Use(coreTrace("global", &trace))
	s.With(WithMiddleware(coreTrace("server-with", &trace)))
	parent := s.Group("/api/", WithGroupMiddleware(coreTrace("parent", &trace)),
		WithGroupOptions(WithBodyLimit(4)), nil)
	child := parent.Group("v1")
	parent.Use(coreTrace("parent-late", &trace)) // 不影响已创建的子组 / does not affect the existing child
	withed := child.With(WithMiddleware(coreTrace("child-with", &trace)))
	child.Use(coreTrace("child", &trace))

	if parent.Prefix() != "/api/" || child.Prefix() != "/api/v1" {
		t.Fatalf("prefixes = %q, %q", parent.Prefix(), child.Prefix())
	}
	routeMW := WithMiddleware(coreTrace("route", &trace))
	for _, err := range []error{
		child.Register(Raw(http.MethodGet, "/c", coreText("c"), routeMW)),
		withed.Register(Raw(http.MethodGet, "/w", coreText("w"))),
		parent.Register(Raw(http.MethodPost, "/body", coreReadAll())),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		method, path, body string
		status             int
		trace              []string
	}{
		{http.MethodGet, "/api/v1/c", "", 200, []string{"global", "server-with", "parent", "child", "route"}},
		{http.MethodGet, "/api/v1/w", "", 200, []string{"global", "server-with", "parent", "child-with"}},
		{http.MethodPost, "/api/body", "12345", 413, []string{"global", "server-with", "parent", "parent-late"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			trace = nil
			rec := coreDo(s, tt.method, tt.path, tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if !reflect.DeepEqual(trace, tt.trace) {
				t.Errorf("trace = %v, want %v", trace, tt.trace)
			}
		})
	}
}

// TestRequestAccessors 测试 Sources、Request()、CookieValue、Params.ByName 与 Float64Slice。
// TestRequestAccessors tests Sources, Request(), CookieValue, Params.ByName and Float64Slice.
func TestRequestAccessors(t *testing.T) {
	var checked bool
	h := func(_ context.Context, req RequestOf[NoDataType]) (string, error) {
		src := req.Sources()
		if v, ok := src.Path("id"); !ok || v != "7" {
			t.Errorf("Path = %q %v", v, ok)
		}
		if _, ok := src.Path("nope"); ok {
			t.Error("missing path param reported present")
		}
		if got := src.Query("f"); !reflect.DeepEqual(got, []string{"1.5", "2"}) {
			t.Errorf("Query = %v", got)
		}
		if got := src.Header("X-H"); !reflect.DeepEqual(got, []string{"h"}) {
			t.Errorf("Header = %v", got)
		}
		if v, ok := src.Cookie("c"); !ok || v != "cv" {
			t.Errorf("Cookie = %q %v", v, ok)
		}
		if _, ok := src.Cookie("none"); ok {
			t.Error("missing cookie reported present")
		}
		if s, _ := req.CookieValue("c").String(); s != "cv" {
			t.Errorf("CookieValue = %q", s)
		}
		if fs, err := req.QueryValues("f").Float64Slice(); err != nil || !reflect.DeepEqual(fs, []float64{1.5, 2}) {
			t.Errorf("Float64Slice = %v %v", fs, err)
		}
		if _, err := QueryValues(req.Request(), "id").Float64Slice(); err != nil {
			t.Errorf("empty slice err = %v", err)
		}
		r := req.Request()
		if r.Route() != "/u/:id" || r.Params.ByName("id") != "7" || r.Context() != r.Raw.Context() {
			t.Errorf("Request() = route %q", r.Route())
		}
		checked = true
		return "ok", nil
	}
	rec := coreServe(t, Get("/u/:id", h), http.MethodGet, "/u/7?f=1.5&f=2", "", "X-H", "h", "Cookie", "c=cv")
	if rec.Code != 200 || !checked {
		t.Fatalf("status = %d", rec.Code)
	}
	r := &Request{Raw: httptest.NewRequest(http.MethodGet, "/?f=x", nil)}
	if _, err := QueryValues(r, "f").Float64Slice(); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("Float64Slice bad err = %v", err)
	}
}

// TestResponseWriterFeatures 测试 Response 的 Unwrap、Flush 与 FlushError。
// TestResponseWriterFeatures tests Response Unwrap, Flush and FlushError.
func TestResponseWriterFeatures(t *testing.T) {
	rec := httptest.NewRecorder()
	resp := &Response{Writer: rec}
	if resp.Unwrap() != rec {
		t.Fatal("Unwrap mismatch")
	}
	if resp.StatusCode() != http.StatusOK {
		t.Error("default status should be 200")
	}
	resp.Flush()
	if !rec.Flushed || !resp.Written() || rec.Code != http.StatusOK {
		t.Errorf("flushed=%v written=%v code=%d", rec.Flushed, resp.Written(), rec.Code)
	}
	if err := http.NewResponseController(resp).Flush(); err != nil {
		t.Errorf("ResponseController.Flush = %v", err)
	}

	type plainWriter struct{ http.ResponseWriter }
	resp2 := &Response{Writer: plainWriter{httptest.NewRecorder()}}
	if err := resp2.FlushError(); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("FlushError = %v, want ErrNotSupported", err)
	}
}

// TestStrictPathOption 测试 WithStrictPath 拒绝空段而默认放行。
// TestStrictPathOption tests WithStrictPath rejects empty segments while the default allows them.
func TestStrictPathOption(t *testing.T) {
	route := Raw(http.MethodGet, "/a/*rest", coreText("ok"))
	lenient := coreNewServer(t, nil, route)
	strict := coreNewServer(t, []ServerOption{WithStrictPath()}, route)
	for _, tt := range []struct {
		s      *Server
		path   string
		status int
	}{
		{lenient, "/a//b", 200},
		{strict, "/a//b", 400},
		{strict, "/a/b/", 200},
		{lenient, "/a/./b", 400},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = tt.path
		rec := httptest.NewRecorder()
		tt.s.ServeHTTP(rec, req)
		if rec.Code != tt.status {
			t.Errorf("%s: status = %d, want %d", tt.path, rec.Code, tt.status)
		}
	}
}

// TestPathHelpers 测试模板转换、前缀拼接与请求路径校验。
// TestPathHelpers tests template conversion, prefix joining and request path validation.
func TestPathHelpers(t *testing.T) {
	norm := []struct {
		in, want string
		ok       bool
	}{
		{"/users/{id}", "/users/:id", true},
		{"/files/{path...}", "/files/*path", true},
		{"/a/{x}/b/{y}", "/a/:x/b/:y", true},
		{"/plain", "/plain", true},
		{"/a/{x", "", false},
		{"/a/x}", "", false},
		{"/a/{}", "", false},
		{"/a{b}", "", false},
		{"/a/{b}c", "", false},
		{"/a/{b:c}", "", false},
	}
	for _, tt := range norm {
		got, err := normalizeRoutePath(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("normalizeRoutePath(%q) = %q, %v", tt.in, got, err)
		}
		if err != nil && !errors.Is(err, ErrInvalidRoutePath) {
			t.Errorf("error %v should wrap ErrInvalidRoutePath", err)
		}
	}

	join := []struct{ prefix, rel, want string }{
		{"/api/", "/users", "/api/users"},
		{"/api", "users/", "/api/users/"},
		{"", "/x", "/x"},
		{"/api", "", "/api"},
		{"/api", "/", "/api/"},
	}
	for _, tt := range join {
		if got := joinPaths(tt.prefix, tt.rel); got != tt.want {
			t.Errorf("joinPaths(%q, %q) = %q, want %q", tt.prefix, tt.rel, got, tt.want)
		}
	}

	valid := []struct {
		path         string
		strict, want bool
	}{
		{"/", false, true},
		{"/a/b/", false, true},
		{"", false, false},
		{"a", false, false},
		{"/a/../b", false, false},
		{"/..", false, false},
		{"/a/.", false, false},
		{"/a/.b", false, true},
		{"/a\x00", false, false},
		{"/a//b", false, true},
		{"/a//b", true, false},
	}
	for _, tt := range valid {
		if got := validRequestPath(tt.path, tt.strict); got != tt.want {
			t.Errorf("validRequestPath(%q, %v) = %v", tt.path, tt.strict, got)
		}
	}

	if err := NewServer().Register(Raw(http.MethodGet, "/f/{p...}", coreText("x"))); err != nil {
		t.Errorf("template route rejected: %v", err)
	}
}

// TestStaticWithServer 冒烟测试：Static 路由与 typed 路由共存并经过错误链。
// TestStaticWithServer is a smoke test: Static routes coexist with typed routes and use the error chain.
func TestStaticWithServer(t *testing.T) {
	fsys := fstest.MapFS{"a.txt": {Data: []byte("A")}}
	s := NewServer()
	if err := s.Register(Static("/assets", fsys)...); err != nil {
		t.Fatal(err)
	}
	if rec := coreDo(s, http.MethodGet, "/assets/a.txt", ""); rec.Code != 200 || rec.Body.String() != "A" {
		t.Errorf("static: %d %q", rec.Code, rec.Body.String())
	}
	if rec := coreDo(s, http.MethodGet, "/assets/missing", ""); rec.Code != 404 || coreErrBody(t, rec).Status != 404 {
		t.Errorf("missing: %d %q", rec.Code, rec.Body.String())
	}
}
