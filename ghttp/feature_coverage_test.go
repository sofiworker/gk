package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 特性覆盖冒烟测试：每个特性一个代表性场景，全部通过真实 HTTP 请求验证。
// Feature coverage smoke tests: one scenario per feature, served over real HTTP.

func TestFeatureCoverage_RoutingAndHTTPMethods(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	app.MustMount(Handle(Get("/users/{id}"), PathString("id"), JSONOutput[struct {
		ID string `json:"id"`
	}](), func(_ context.Context, id string) (struct {
		ID string `json:"id"`
	}, error) {
		return struct {
			ID string `json:"id"`
		}{ID: id}, nil
	}))
	app.MustMount(Handle(Delete("/users/{id}"), NoInput(), WithStatus(http.StatusNoContent, JSONOutput[struct{}]()), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, nil
	}))
	app.MustMount(HandleHTTP(Options("/users/{id}"), NoInput(), func(w http.ResponseWriter, _ *http.Request, _ EmptyInput) error {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	app.MustMount(AllMethods(Handle(Get("/any"), NoInput(), JSONOutput[struct{}](), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, nil
	}))...)

	app.MustMount(RawOperation("PURGE", "/purge", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantAllow  string
	}{
		{"get-param", http.MethodGet, "/users/42", http.StatusOK, `{"id":"42"}`, ""},
		{"head-fallback", http.MethodHead, "/users/42", http.StatusOK, "", ""},
		{"delete-204", http.MethodDelete, "/users/42", http.StatusNoContent, "", ""},
		{"options-explicit", http.MethodOptions, "/users/42", http.StatusNoContent, "", ""},
		{"put-405", http.MethodPut, "/users/42", http.StatusMethodNotAllowed, "", "GET, HEAD"},
		{"trailing-slash", http.MethodGet, "/users/42/", http.StatusOK, `{"id":"42"}`, ""},
		{"extra-segment-404", http.MethodGet, "/users/42/x", http.StatusNotFound, "", ""},
		{"any-get", http.MethodGet, "/any", http.StatusOK, "", ""},
		{"any-head", http.MethodHead, "/any", http.StatusOK, "", ""},
		{"any-put", http.MethodPut, "/any", http.StatusOK, "", ""},
		{"custom-method", "PURGE", "/purge", http.StatusNoContent, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && strings.TrimSpace(w.Body.String()) != tc.wantBody {
				t.Fatalf("body = %q, want %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantAllow != "" && !strings.Contains(w.Header().Get("Allow"), tc.wantAllow) {
				t.Fatalf("Allow = %q, want %q", w.Header().Get("Allow"), tc.wantAllow)
			}
		})
	}
}

type coverageBindInput struct {
	ID    int
	Page  int
	Token string
	Sess  string
	IP    string
}

type coverageBindOutput struct {
	ID   int    `json:"id"`
	Page int    `json:"page"`
	Tok  string `json:"token"`
	Sess string `json:"session"`
	IP   string `json:"ip"`
}

func TestFeatureCoverage_ParamsBinding(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	input := MapInputs(
		MapInputs4(
			PathInt64("id"),
			QueryIntDefault("page", 1),
			HeaderString("X-Token"),
			CookieString("sid"),
			func(id int64, page int, token, sess string) coverageBindInput {
				return coverageBindInput{ID: int(id), Page: page, Token: token, Sess: sess}
			},
		),
		InputFunc(func(view RequestView) (string, error) {
			return view.ClientIP(), nil
		}),
		func(in coverageBindInput, ip string) coverageBindInput {
			in.IP = ip
			return in
		},
	)
	app.MustMount(Handle(Get("/users/{id}"), input, JSONOutput[coverageBindOutput](), func(_ context.Context, in coverageBindInput) (coverageBindOutput, error) {
		return coverageBindOutput{
			ID:   in.ID,
			Page: in.Page,
			Tok:  in.Token,
			Sess: in.Sess,
			IP:   in.IP,
		}, nil
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42?page=3", nil)
	req.Header.Set("X-Token", "secret")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "abc"})
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	var out coverageBindOutput
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != 42 || out.Page != 3 || out.Tok != "secret" || out.Sess != "abc" || out.IP == "" {
		t.Fatalf("output = %#v", out)
	}
}

func TestFeatureCoverage_BodyCodecs(t *testing.T) {
	type jsonInput struct {
		Name string `json:"name"`
	}
	type out struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/json"), JSONBody[jsonInput](), JSONOutput[out](), func(_ context.Context, in jsonInput) (out, error) {
		return out{Name: in.Name}, nil
	}))
	app.MustMount(Handle(Post("/form"), FormBody(), JSONOutput[out](), func(_ context.Context, values url.Values) (out, error) {
		age, _ := strconv.Atoi(values.Get("age"))
		return out{Name: values.Get("name"), Age: age}, nil
	}))
	app.MustMount(Handle(Post("/no-tag-form"), JSONBody[jsonInput](), JSONOutput[out](), func(_ context.Context, in jsonInput) (out, error) {
		return out{Name: in.Name}, nil
	}))

	jsonRec := httptest.NewRecorder()
	jsonReq := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{"name":"alice"}`))
	jsonReq.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusOK || !strings.Contains(jsonRec.Body.String(), `"alice"`) {
		t.Fatalf("json status/body = %d %s", jsonRec.Code, jsonRec.Body.String())
	}

	missingCT := httptest.NewRecorder()
	missingCTReq := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{"name":"bob"}`))
	app.ServeHTTP(missingCT, missingCTReq)
	if missingCT.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing content type status = %d, want 415", missingCT.Code)
	}

	formRec := httptest.NewRecorder()
	formReq := httptest.NewRequest(http.MethodPost, "/form", strings.NewReader("name=carol&age=28"))
	formReq.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(formRec, formReq)
	if formRec.Code != http.StatusOK || !strings.Contains(formRec.Body.String(), `"carol"`) || !strings.Contains(formRec.Body.String(), `28`) {
		t.Fatalf("form status/body = %d %s", formRec.Code, formRec.Body.String())
	}

	strictRec := httptest.NewRecorder()
	strictReq := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{"name":"a"}`))
	strictReq.Header.Set("Content-Type", "application/octet-stream")
	app.ServeHTTP(strictRec, strictReq)
	if strictRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("strict unknown content type = %d, want 415", strictRec.Code)
	}

	noTagRec := httptest.NewRecorder()
	noTagReq := httptest.NewRequest(http.MethodPost, "/no-tag-form", strings.NewReader("name=dan"))
	noTagReq.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(noTagRec, noTagReq)
	if noTagRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("no-tag form status = %d, want 415", noTagRec.Code)
	}
}

func TestFeatureCoverage_Validation(t *testing.T) {
	type input struct {
		Name string `json:"name" validate:"required"`
	}
	type output struct {
		Name string `json:"name"`
	}
	handler := func(_ context.Context, in input) (output, error) {
		return output{Name: in.Name}, nil
	}

	app := New(WithProduces(MIMEJSON), WithValidator(newDefaultValidator()))
	app.MustMount(Handle(Post("/required"), JSONBody[input](), JSONOutput[output](), handler))
	app.MustMount(Handle(Post("/skip"), JSONBody[input](), JSONOutput[output](), handler).WithoutServerValidation())
	app.MustMount(Handle(Post("/mapped"), ValidatedInput(JSONBody[input](), func(_ context.Context, in input) error {
		if in.Name == "bad" {
			return errors.New("bad name")
		}
		return nil
	},
		ValidationError(BadRequest("name is bad"))), JSONOutput[output](), handler))

	emptyRec := httptest.NewRecorder()
	emptyReq := httptest.NewRequest(http.MethodPost, "/required", strings.NewReader(`{}`))
	emptyReq.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(emptyRec, emptyReq)
	if emptyRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("required validation status = %d, want 422", emptyRec.Code)
	}

	skipRec := httptest.NewRecorder()
	skipReq := httptest.NewRequest(http.MethodPost, "/skip", strings.NewReader(`{}`))
	skipReq.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(skipRec, skipReq)
	if skipRec.Code != http.StatusOK {
		t.Fatalf("skip validation status = %d, want 200", skipRec.Code)
	}

	mappedRec := httptest.NewRecorder()
	mappedReq := httptest.NewRequest(http.MethodPost, "/mapped", strings.NewReader(`{"name":"bad"}`))
	mappedReq.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(mappedRec, mappedReq)
	if mappedRec.Code != http.StatusBadRequest || !strings.Contains(mappedRec.Body.String(), "name is bad") {
		t.Fatalf("mapped validation status/body = %d %s", mappedRec.Code, mappedRec.Body.String())
	}
}

func TestFeatureCoverage_MiddlewareGroupsCapabilities(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var order []string
	app.Use(func(c *Ctx) {
		order = append(order, "server")
		c.Next()
	})
	group := app.Group("/api", func(c *Ctx) {
		order = append(order, "group")
		c.Next()
	})

	group.MustMount(Handle(Get("/users/{id}"), PathString("id"), JSONOutput[struct {
		ID string `json:"id"`
	}](), func(_ context.Context, id string) (struct {
		ID string `json:"id"`
	}, error) {
		return struct {
			ID string `json:"id"`
		}{ID: id}, nil
	}).WithMiddleware(func(c *Ctx) {
		order = append(order, "route")
		c.Next()
	}))

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := strings.Join(order, ","); got != "server,group,route" {
		t.Fatalf("middleware order = %q", got)
	}
}

func TestFeatureCoverage_MatchedParamsRequestIDCORS(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.Use(RequestID())
	app.Use(CORS(CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet},
	}))
	app.Use(func(c *Ctx) {
		c.W.Header().Set("X-Matched", app.MatchedParams(c.R).Path("id"))
		c.Next()
	})
	app.MustMount(Handle(Get("/users/{id}"), NoInput(), JSONOutput[struct{}](), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, nil
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/9", nil)
	req.Header.Set("X-Request-ID", "req-1")
	app.ServeHTTP(w, req)
	if w.Header().Get("X-Request-ID") != "req-1" {
		t.Fatalf("request id = %q", w.Header().Get("X-Request-ID"))
	}
	if w.Header().Get("X-Matched") != "9" {
		t.Fatalf("matched param = %q", w.Header().Get("X-Matched"))
	}

	preflight := httptest.NewRecorder()
	preReq := httptest.NewRequest(http.MethodOptions, "/users/9", nil)
	preReq.Header.Set("Origin", "https://example.com")
	preReq.Header.Set("Access-Control-Request-Method", http.MethodGet)
	app.ServeHTTP(preflight, preReq)
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight = %d %q", preflight.Code, preflight.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestFeatureCoverage_LoggerAndRBAC(t *testing.T) {
	var logBuf bytes.Buffer
	app := New(WithProduces(MIMEJSON), WithLogger(NewSlogLogger(slog.New(slog.NewTextHandler(&logBuf, nil)))))
	app.Use(RequestLogger())
	authz := NewRBAC(
		staticRoles{"alice": {"admin"}},
		staticPolicies{"admin|users:read|/users": true},
	)
	app.Use(RBACMiddleware(
		authz,
		func(r *http.Request) string { return r.Header.Get("X-User") },
		func(r *http.Request) string { return "users:read" },
		func(r *http.Request) string { return r.URL.Path },
	))
	app.MustMount(Handle(Get("/users"), NoInput(), JSONOutput[struct{}](), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, nil
	}))

	okRec := httptest.NewRecorder()
	okReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	okReq.Header.Set("X-User", "alice")
	app.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("allowed status = %d", okRec.Code)
	}

	deniedRec := httptest.NewRecorder()
	deniedReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	deniedReq.Header.Set("X-User", "bob")
	app.ServeHTTP(deniedRec, deniedReq)
	if deniedRec.Code != http.StatusForbidden {
		t.Fatalf("denied status = %d", deniedRec.Code)
	}

	if !strings.Contains(logBuf.String(), "http request") {
		t.Fatalf("request log missing: %s", logBuf.String())
	}
}

type coverageCreatedResp struct {
	ID string `json:"id"`
}

func (r coverageCreatedResp) StatusCode() int { return http.StatusCreated }

func (r coverageCreatedResp) WriteResponseHeaders(h http.Header) {
	h.Set("X-Resource-ID", r.ID)
}

type coverageNoContentResp struct{}

func (coverageNoContentResp) StatusCode() int { return http.StatusNoContent }

type coverageCookieResp struct {
	Token string `json:"token"`
}

func (o coverageCookieResp) Cookies() []*http.Cookie {
	return []*http.Cookie{{Name: "session", Value: o.Token, Path: "/", HttpOnly: true}}
}

func TestFeatureCoverage_OutputAndErrors(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Post("/created"), NoInput(), JSONOutput[coverageCreatedResp](), func(context.Context, EmptyInput) (coverageCreatedResp, error) {
		return coverageCreatedResp{ID: "u-1"}, nil
	}))
	app.MustMount(Handle(Delete("/nocontent"), NoInput(), JSONOutput[coverageNoContentResp](), func(context.Context, EmptyInput) (coverageNoContentResp, error) {
		return coverageNoContentResp{}, nil
	}))
	app.MustMount(Handle(Get("/cookie"), NoInput(), JSONOutput[coverageCookieResp](), func(context.Context, EmptyInput) (coverageCookieResp, error) {
		return coverageCookieResp{Token: "t-1"}, nil
	}))

	created := httptest.NewRecorder()
	app.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/created", nil))
	if created.Code != http.StatusCreated || created.Header().Get("X-Resource-ID") != "u-1" {
		t.Fatalf("created = %d %q", created.Code, created.Header().Get("X-Resource-ID"))
	}

	noContent := httptest.NewRecorder()
	app.ServeHTTP(noContent, httptest.NewRequest(http.MethodDelete, "/nocontent", nil))
	if noContent.Code != http.StatusNoContent || noContent.Body.Len() != 0 {
		t.Fatalf("no content = %d body=%q", noContent.Code, noContent.Body.String())
	}

	cookieRec := httptest.NewRecorder()
	app.ServeHTTP(cookieRec, httptest.NewRequest(http.MethodGet, "/cookie", nil))
	cookies := cookieRec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "t-1" {
		t.Fatalf("cookies = %#v", cookies)
	}
}

func TestFeatureCoverage_EnvelopeAndErrorModels(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithEnvelope(DefaultEnvelope))

	app.MustMount(Handle(Get("/ok"), NoInput(), JSONOutput[struct {
		ID string `json:"id"`
	}](), func(context.Context, EmptyInput) (struct {
		ID string `json:"id"`
	}, error) {
		return struct {
			ID string `json:"id"`
		}{ID: "42"}, nil
	}))
	app.MustMount(Handle(Get("/notfound"), NoInput(), JSONOutput[struct{}](), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, Err(http.StatusNotFound, "user not found")
	}))
	app.MustMount(Handle(Get("/problem"), NoInput(), JSONOutput[struct{}](), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, Err(http.StatusBadRequest, "bad request")
	}).WithProblemDetails())

	okRec := httptest.NewRecorder()
	app.ServeHTTP(okRec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if okRec.Code != http.StatusOK || !strings.Contains(okRec.Body.String(), `"msg":"success"`) {
		t.Fatalf("envelope = %d %s", okRec.Code, okRec.Body.String())
	}

	notFound := httptest.NewRecorder()
	app.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/notfound", nil))
	if notFound.Code != http.StatusNotFound || !strings.Contains(notFound.Body.String(), `"code":404`) {
		t.Fatalf("envelope error = %d %s", notFound.Code, notFound.Body.String())
	}

	problem := httptest.NewRecorder()
	app.ServeHTTP(problem, httptest.NewRequest(http.MethodGet, "/problem", nil))
	if problem.Code != http.StatusBadRequest || problem.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("problem = %d %q", problem.Code, problem.Header().Get("Content-Type"))
	}
}

func TestFeatureCoverage_Negotiation(t *testing.T) {
	type out struct {
		ID string `json:"id" xml:"id"`
	}
	app := New(WithProduces(MIMEJSON, MIMEXML))
	app.MustMount(Handle(Get("/users/{id}"), PathString("id"), CodecOutput[out](MIMEJSON, MIMEXML), func(_ context.Context, id string) (out, error) {
		return out{ID: id}, nil
	}))

	xmlRec := httptest.NewRecorder()
	xmlReq := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	xmlReq.Header.Set("Accept", MIMEXML)
	app.ServeHTTP(xmlRec, xmlReq)
	if xmlRec.Header().Get("Content-Type") != MIMEXML || !strings.Contains(xmlRec.Body.String(), "<id>1</id>") {
		t.Fatalf("xml = %q %s", xmlRec.Header().Get("Content-Type"), xmlRec.Body.String())
	}

	notAcceptable := httptest.NewRecorder()
	naReq := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	naReq.Header.Set("Accept", "text/html")
	app.ServeHTTP(notAcceptable, naReq)
	if notAcceptable.Code != http.StatusNotAcceptable {
		t.Fatalf("default 406 = %d", notAcceptable.Code)
	}

	q0 := httptest.NewRecorder()
	q0Req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	q0Req.Header.Set("Accept", MIMEJSON+";q=0,"+MIMEXML)
	app.ServeHTTP(q0, q0Req)
	if q0.Header().Get("Content-Type") != MIMEXML {
		t.Fatalf("q=0 content type = %q", q0.Header().Get("Content-Type"))
	}

	lenient := New(WithProduces(MIMEJSON), WithLenientContentNegotiation())
	lenient.MustMount(Handle(Get("/ping"), NoInput(), CodecOutput[map[string]string](MIMEJSON), func(context.Context, EmptyInput) (map[string]string, error) {
		return map[string]string{"pong": "1"}, nil
	}))
	lenientRec := httptest.NewRecorder()
	lenientReq := httptest.NewRequest(http.MethodGet, "/ping", nil)
	lenientReq.Header.Set("Accept", "text/html")
	lenient.ServeHTTP(lenientRec, lenientReq)
	if lenientRec.Code != http.StatusOK || lenientRec.Header().Get("Content-Type") != MIMEJSON {
		t.Fatalf("lenient = %d %q", lenientRec.Code, lenientRec.Header().Get("Content-Type"))
	}
}

func TestFeatureCoverage_EscapeHatches(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(RawOperation(http.MethodGet, "/raw", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})))
	app.MustMount(RawOperation(http.MethodGet, "/http", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("plain"))
	})))
	app.MustMount(HandleHTTP(Get("/httpfunc/{id}"), PathString("id"), func(w http.ResponseWriter, _ *http.Request, id string) error {
		_, _ = w.Write([]byte(id))
		return nil
	}))
	app.MustMount(RedirectOperation(http.MethodGet, "/redirect", http.StatusFound, "/target"))

	raw := httptest.NewRecorder()
	app.ServeHTTP(raw, httptest.NewRequest(http.MethodGet, "/raw", nil))
	if raw.Code != http.StatusTeapot {
		t.Fatalf("raw = %d", raw.Code)
	}
	httpRec := httptest.NewRecorder()
	app.ServeHTTP(httpRec, httptest.NewRequest(http.MethodGet, "/http", nil))
	if httpRec.Code != http.StatusOK || httpRec.Body.String() != "plain" {
		t.Fatalf("http = %d %q", httpRec.Code, httpRec.Body.String())
	}
	httpFunc := httptest.NewRecorder()
	app.ServeHTTP(httpFunc, httptest.NewRequest(http.MethodGet, "/httpfunc/77", nil))
	if httpFunc.Body.String() != "77" {
		t.Fatalf("httpfunc = %q", httpFunc.Body.String())
	}
	redirect := httptest.NewRecorder()
	app.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/redirect", nil))
	if redirect.Code != http.StatusFound || redirect.Header().Get("Location") != "/target" {
		t.Fatalf("redirect = %d %q", redirect.Code, redirect.Header().Get("Location"))
	}
}

func TestFeatureCoverage_StaticAndHTMLAndSSE(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello-static"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "page.html"), []byte("<h1>{{.Title}}</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := New(WithProduces(MIMEJSON), WithRenderer(NewRenderer(tmpDir, ".html", template.FuncMap{}, false)))
	app.MustMount(StaticDirectory("/static", tmpDir))
	app.MustMount(HTMLViewOperation(http.MethodGet, "/page", http.StatusOK, "page", map[string]interface{}{"Title": "Hello"}))
	app.MustMount(SSEOperation("/events", NoInput(), func(_ context.Context, _ EmptyInput, stream *SSEWriter) error {
		return stream.WriteEvent("message", "line1\nline2")
	}))

	staticRec := httptest.NewRecorder()
	app.ServeHTTP(staticRec, httptest.NewRequest(http.MethodGet, "/static/a.txt", nil))
	if staticRec.Code != http.StatusOK || staticRec.Body.String() != "hello-static" {
		t.Fatalf("static = %d %q", staticRec.Code, staticRec.Body.String())
	}

	htmlRec := httptest.NewRecorder()
	app.ServeHTTP(htmlRec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if htmlRec.Code != http.StatusOK || htmlRec.Body.String() != "<h1>Hello</h1>" {
		t.Fatalf("html = %d %q", htmlRec.Code, htmlRec.Body.String())
	}

	sseRec := httptest.NewRecorder()
	app.ServeHTTP(sseRec, httptest.NewRequest(http.MethodGet, "/events", nil))
	if sseRec.Code != http.StatusOK || !strings.Contains(sseRec.Body.String(), "data: line1\ndata: line2") {
		t.Fatalf("sse = %d %q", sseRec.Code, sseRec.Body.String())
	}
}

func TestFeatureCoverage_WebSocket(t *testing.T) {
	app := New()
	app.MustMount(WebSocketOperation("/ws/{room}", PathString("room"), func(_ context.Context, room string, conn *WebSocketConn) error {
		var msg map[string]string
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		msg["room"] = room
		return conn.WriteJSON(msg)
	}))

	ts := httptest.NewServer(app)
	defer ts.Close()
	conn := dialTestWebSocket(t, ts.URL+"/ws/lobby")
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"hi": "there"}); err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatal(err)
	}
	if out["room"] != "lobby" || out["hi"] != "there" {
		t.Fatalf("ws echo = %#v", out)
	}
}

func TestFeatureCoverage_OpenAPI(t *testing.T) {
	app := New(
		WithProduces(MIMEJSON),
		WithOpenAPI("coverage", "1.0.0"),
		WithOpenAPIServers("https://api.example.com"),
		WithOpenAPISecurity(map[string][]string{"apiKey": {}}),
		WithEnvelope(DefaultEnvelope),
	)
	app.MustMount(Handle(Post("/users"), NoInput(), WithStatus(http.StatusCreated, JSONOutput[coverageCreatedResp]()), func(context.Context, EmptyInput) (coverageCreatedResp, error) {
		return coverageCreatedResp{ID: "u-1"}, nil
	}).Doc(Tags("users"), OperationID("createUser"), Summary("create a user")))

	doc, err := app.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	for _, want := range []string{
		`"/users"`, `"tags":["users"]`, `"operationId":"createUser"`,
		`"201"`, `"data"`, `"servers"`, `"security"`, `"apiKey"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("openapi missing %s: %s", want, text)
		}
	}
}

func TestFeatureCoverage_SetupErrors(t *testing.T) {
	t.Run("duplicate-route", func(t *testing.T) {
		app := New(WithProduces(MIMEJSON))
		app.MustMount(RawOperation(http.MethodGet, "/dup", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic on duplicate route")
			}
		}()
		app.MustMount(RawOperation(http.MethodGet, "/dup", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})))
	})

	t.Run("timeout", func(t *testing.T) {
		app := New(WithProduces(MIMEJSON))
		app.Use(Timeout(50 * time.Millisecond))
		app.MustMount(HandleNoOutput(Get("/slow"), NoInput(), func(context.Context, EmptyInput) error {
			time.Sleep(300 * time.Millisecond)
			return nil
		}))
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))
		if w.Code != http.StatusGatewayTimeout {
			t.Fatalf("timeout status = %d, want 504", w.Code)
		}
	})
}
