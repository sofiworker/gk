package gclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryLogger struct {
	mu      sync.Mutex
	entries []string
}

func (l *memoryLogger) Errorf(format string, v ...any) {
	l.add(format, v...)
}

func (l *memoryLogger) Warnf(format string, v ...any) {
	l.add(format, v...)
}

func (l *memoryLogger) Debugf(format string, v ...any) {
	l.add(format, v...)
}

func (l *memoryLogger) add(format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, fmt.Sprintf(format, v...))
}

func (l *memoryLogger) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.entries, "\n")
}

type recordingTracer struct {
	mu         sync.Mutex
	started    int
	attributes map[string]interface{}
}

func (t *recordingTracer) StartSpan(ctx context.Context) (context.Context, func()) {
	t.mu.Lock()
	t.started++
	t.mu.Unlock()
	return ctx, func() {}
}

func (t *recordingTracer) SpanName() string {
	return "test"
}

func (t *recordingTracer) SetAttribute(key string, value interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.attributes == nil {
		t.attributes = make(map[string]interface{})
	}
	t.attributes[key] = value
}

type memoryCache struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryCache() *memoryCache {
	return &memoryCache{data: make(map[string][]byte)}
}

func (m *memoryCache) Get(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.data[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func (m *memoryCache) Set(key string, data []byte, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), data...)
}

type handlerExecutor struct {
	handler http.Handler
}

func (h *handlerExecutor) Do(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func newMockExecutor(t *testing.T, handler http.Handler) HTTPExecutor {
	t.Helper()
	return &handlerExecutor{handler: handler}
}

func TestClientRequestBuildAndDecode(t *testing.T) {
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users/9" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("q") != "go" || query.Get("lang") != "en" {
			t.Fatalf("unexpected query %v", query)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("unexpected content type %q", ct)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body error: %v", err)
		}
		if payload["name"] != "gk" {
			t.Fatalf("unexpected payload %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":   true,
			"name": payload["name"],
		})
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)
	resp, err := client.R().
		SetPathParam("id", "9").
		AddQueryParams(map[string]string{"q": "go", "lang": "en"}).
		SetBody(map[string]interface{}{"name": "gk"}).
		Post("/api/users/:id")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}

	var out struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}
	if err := resp.Decode(&out); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if !out.OK || out.Name != "gk" {
		t.Fatalf("unexpected response %+v", out)
	}
	if !strings.HasPrefix(resp.ContentType, "application/json") {
		t.Fatalf("unexpected response content type %q", resp.ContentType)
	}
}

func TestClientRetryAndMiddleware(t *testing.T) {
	var attempts int32
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"attempt": int(n)})
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
		WithRetry(&RetryConfig{
			MaxRetries:      3,
			RetryConditions: []RetryCondition{DefaultRetryCondition},
			Backoff: func(int) time.Duration {
				return 0
			},
		}),
	)

	var reqMW, respMW int32
	client.UseRequest(func(_ *Client, _ *Request) error {
		atomic.AddInt32(&reqMW, 1)
		return nil
	})
	client.UseResponse(func(_ *Client, _ *Response) error {
		atomic.AddInt32(&respMW, 1)
		return nil
	})

	resp, err := client.R().Get("/retry")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	if reqMW != 1 {
		t.Fatalf("request middleware should run once, got %d", reqMW)
	}
	if respMW != 1 {
		t.Fatalf("response middleware should run once, got %d", respMW)
	}

	var result map[string]int
	if err := resp.Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if result["attempt"] != 3 {
		t.Fatalf("unexpected decoded attempt %+v", result)
	}
}

func TestClientCache(t *testing.T) {
	var hits int32
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"hit": int(current)})
	}))

	cache := newMemoryCache()
	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
		WithCache(cache),
	)

	resp1, err := client.R().UseCache("", time.Minute).Get("/cache")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	var first map[string]int
	if err := resp1.Decode(&first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	resp2, err := client.R().UseCache("", time.Minute).Get("/cache")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	var second map[string]int
	if err := resp2.Decode(&second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	if hits != 1 {
		t.Fatalf("cache not used, hits=%d", hits)
	}
	if first["hit"] != second["hit"] {
		t.Fatalf("cached content mismatch %+v vs %+v", first, second)
	}
}

func TestClientDefaultsAndAuth(t *testing.T) {
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/users/7" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("lang"); got != "zh-CN" {
			t.Fatalf("unexpected query lang %q", got)
		}
		if got := r.Header.Get("X-Client"); got != "gclient" {
			t.Fatalf("unexpected header %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("unexpected authorization %q", got)
		}
		if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "abc" {
			t.Fatalf("unexpected cookie %+v err=%v", cookie, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)
	client.SetHeader("X-Client", "gclient")
	client.SetQueryParam("lang", "zh-CN")
	client.SetPathParam("id", "7")
	client.SetCookie(&http.Cookie{Name: "session", Value: "abc"})
	client.SetAuthToken("token-1")

	resp, err := client.R().Get("/v1/users/:id")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
}

func TestRequestSetResultAndResultError(t *testing.T) {
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "gk"})
		case "/fail":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "bad request"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)

	var okResult struct {
		Name string `json:"name"`
	}
	okResp, err := client.R().
		SetResult(&okResult).
		Get("/ok")
	if err != nil {
		t.Fatalf("ok request failed: %v", err)
	}
	if okResp.Result() != &okResult {
		t.Fatalf("result pointer mismatch")
	}
	if okResult.Name != "gk" {
		t.Fatalf("unexpected result %+v", okResult)
	}

	var errResult struct {
		Message string `json:"message"`
	}
	errResp, err := client.R().
		SetResultError(&errResult).
		Get("/fail")
	if err != nil {
		t.Fatalf("fail request should not return transport error: %v", err)
	}
	if !errResp.IsFailure() {
		t.Fatalf("expected failure response")
	}
	if errResp.ResultError() != &errResult {
		t.Fatalf("result error pointer mismatch")
	}
	if errResult.Message != "bad request" {
		t.Fatalf("unexpected error result %+v", errResult)
	}
}

func TestClientSetContextPropagatesToRequest(t *testing.T) {
	ctx := context.WithValue(context.Background(), "trace_id", "trace-1")
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Context().Value("trace_id"); got != "trace-1" {
			t.Fatalf("unexpected context value %v", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)
	client.SetContext(ctx)

	if _, err := client.R().Get("/ctx"); err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

func TestMultipartUploadAndSaveResponseToFile(t *testing.T) {
	tmpDir := t.TempDir()
	uploadFile := filepath.Join(tmpDir, "upload.txt")
	if err := os.WriteFile(uploadFile, []byte("hello multipart"), 0o644); err != nil {
		t.Fatalf("write upload file: %v", err)
	}

	downloadFile := filepath.Join(tmpDir, "download.txt")
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("unexpected content type %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if got := r.FormValue("name"); got != "gk" {
			t.Fatalf("unexpected multipart field %q", got)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read form file: %v", err)
		}
		if string(data) != "hello multipart" {
			t.Fatalf("unexpected file content %q", string(data))
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="download.txt"`)
		_, _ = w.Write([]byte("saved"))
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)
	client.SetResponseSaveDirectory(tmpDir)

	resp, err := client.R().
		SetMultipartFormData(map[string]string{"name": "gk"}).
		SetFile("file", uploadFile).
		SetResponseSaveToFile(true).
		Post("/upload")
	if err != nil {
		t.Fatalf("multipart request failed: %v", err)
	}
	if resp.String() != "saved" {
		t.Fatalf("unexpected response body %q", resp.String())
	}

	data, err := os.ReadFile(downloadFile)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "saved" {
		t.Fatalf("unexpected saved file body %q", string(data))
	}
}

func TestClientConvenienceMethods(t *testing.T) {
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ping" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("pong"))
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)

	if client.NewRequest() == nil {
		t.Fatalf("expected new request")
	}
	if client.Executor() == nil {
		t.Fatalf("expected executor")
	}
	if client.HTTPClient() == nil {
		t.Fatalf("expected http client")
	}

	resp, err := client.Get("/ping")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if resp.String() != "pong" {
		t.Fatalf("unexpected response body %q", resp.String())
	}
}

func TestClientCloneAndMustMethods(t *testing.T) {
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Clone") != "1" {
			t.Fatalf("unexpected cloned header %q", r.Header.Get("X-Clone"))
		}
		_, _ = w.Write([]byte("ok"))
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)
	client.SetHeader("X-Clone", "1")
	client.SetQueryParam("lang", "zh")

	clone := client.Clone()
	if clone == nil {
		t.Fatalf("expected clone")
	}
	if clone.BaseURL() != client.BaseURL() {
		t.Fatalf("unexpected clone base url")
	}
	if clone == client {
		t.Fatalf("clone should be a different instance")
	}

	resp := clone.MustGet("/ping")
	if resp.String() != "ok" {
		t.Fatalf("unexpected response %q", resp.String())
	}
}

func TestClientDoAndValueHelpers(t *testing.T) {
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Values("Accept"); len(got) != 2 {
			t.Fatalf("unexpected accept headers %+v", got)
		}
		if got := r.Header.Get("User-Agent"); got != "gclient-test" {
			t.Fatalf("unexpected user agent %q", got)
		}
		query := r.URL.Query()
		if got := query["lang"]; len(got) != 2 {
			t.Fatalf("unexpected query values %+v", got)
		}
		if got := query["tag"]; len(got) != 2 {
			t.Fatalf("unexpected tag values %+v", got)
		}
		_, _ = w.Write([]byte("ok"))
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)
	client.SetHeaderValues(map[string][]string{"Accept": {"application/json", "text/plain"}})
	client.SetUserAgent("gclient-test")
	client.AddQueryParamsFromValues(url.Values{"lang": []string{"zh", "en"}})

	httpReq, err := http.NewRequest(http.MethodGet, "http://example.com/do?tag=go&tag=http", nil)
	if err != nil {
		t.Fatalf("new http request failed: %v", err)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("client do failed: %v", err)
	}
	if resp.String() != "ok" {
		t.Fatalf("unexpected response %q", resp.String())
	}
}

func TestClientDebugDumpAndRoundTrip(t *testing.T) {
	logger := &memoryLogger{}
	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))),
	)
	client.SetLogger(logger).SetDebug(true)

	httpResp, err := client.RoundTrip(httptest.NewRequest(http.MethodPost, "http://example.com/debug?q=1", strings.NewReader(`{"name":"gk"}`)))
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if httpResp == nil || httpResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected http response %+v", httpResp)
	}
	logs := logger.joined()
	if !strings.Contains(logs, "request dump") || !strings.Contains(logs, "response dump") {
		t.Fatalf("unexpected debug logs %q", logs)
	}
}

func TestClientConfigSettersAndStream(t *testing.T) {
	tracer := &recordingTracer{}
	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("q") != "1" {
				t.Fatalf("unexpected query %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("stream"))
		}))),
	)

	client.SetProxy("http://127.0.0.1:8080")
	client.SetProxyFunc(func(*http.Request) (*url.URL, error) {
		return url.Parse("http://127.0.0.1:8081")
	})
	client.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13})
	client.SetMaxRedirects(3)
	client.SetFollowRedirects(false)
	client.AddRedirectHandler(func(*Response) bool { return false })

	if client.Transport() == nil {
		t.Fatalf("expected transport")
	}
	if client.HTTPClient().CheckRedirect == nil {
		t.Fatalf("expected redirect policy")
	}
	if client.config.TLSConfig == nil || client.config.TLSConfig.MinVersion != tls.VersionTLS13 {
		t.Fatalf("unexpected tls config")
	}
	if transport, ok := client.Transport().(*http.Transport); !ok || transport.Proxy == nil {
		t.Fatalf("expected proxy transport")
	}

	httpResp, err := client.R().SetTracer(tracer).SetQueryParam("q", "1").GetStream("/stream")
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		t.Fatalf("read stream body failed: %v", err)
	}
	if string(body) != "stream" {
		t.Fatalf("unexpected stream body %q", string(body))
	}
	if tracer.started != 1 {
		t.Fatalf("expected tracer start once, got %d", tracer.started)
	}
}

func TestClientAndRequestResponseUnwrapper(t *testing.T) {
	type envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}

	clientUnwrapper := func(resp *Response, out interface{}) error {
		var env envelope
		if err := resp.JSON(&env); err != nil {
			return err
		}
		return json.Unmarshal(env.Data, out)
	}

	requestUnwrapper := func(resp *Response, out interface{}) error {
		var env struct {
			Payload json.RawMessage `json:"payload"`
		}
		if err := resp.JSON(&env); err != nil {
			return err
		}
		return json.Unmarshal(env.Payload, out)
	}

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/client":
				_, _ = w.Write([]byte(`{"code":0,"data":{"name":"client"}}`))
			case "/request":
				_, _ = w.Write([]byte(`{"payload":{"name":"request"}}`))
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		}))),
	)
	client.SetResponseUnwrapper(clientUnwrapper)

	var clientResult struct {
		Name string `json:"name"`
	}
	if _, err := client.R().SetResult(&clientResult).Get("/client"); err != nil {
		t.Fatalf("client unwrapper request failed: %v", err)
	}
	if clientResult.Name != "client" {
		t.Fatalf("unexpected client result %+v", clientResult)
	}

	var requestResult struct {
		Name string `json:"name"`
	}
	if _, err := client.R().
		SetResponseUnwrapper(requestUnwrapper).
		SetResult(&requestResult).
		Get("/request"); err != nil {
		t.Fatalf("request unwrapper request failed: %v", err)
	}
	if requestResult.Name != "request" {
		t.Fatalf("unexpected request result %+v", requestResult)
	}
}

func TestResponseStatusCheckerWithBusinessError(t *testing.T) {
	type bizEnvelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":1001,"msg":"invalid token"}`))
		}))),
	)
	client.SetResponseStatusChecker(func(resp *Response) error {
		var env bizEnvelope
		if err := resp.JSON(&env); err != nil {
			return err
		}
		if env.Code != 0 {
			return &BusinessError{
				Code:     env.Code,
				Message:  env.Msg,
				Response: resp,
			}
		}
		return nil
	})

	var errResult bizEnvelope
	resp, err := client.R().SetResultError(&errResult).Get("/biz-error")
	if err != nil {
		t.Fatalf("transport error is not expected: %v", err)
	}
	if resp.IsSuccess() != true {
		t.Fatalf("expected http success")
	}
	if resp.IsOK() {
		t.Fatalf("expected business failure")
	}
	if resp.BusinessError() == nil {
		t.Fatalf("expected business error")
	}
	if errResult.Code != 1001 || errResult.Msg != "invalid token" {
		t.Fatalf("unexpected error result %+v", errResult)
	}
	var bizErr *BusinessError
	if !errors.As(resp.BusinessError(), &bizErr) {
		t.Fatalf("expected business error type, got %T", resp.BusinessError())
	}
}

func TestJSONEnvelopeHelpersAndMustOK(t *testing.T) {
	unwrapper, checker := JSONEnvelopeHandlers(JSONEnvelopeConfig{})

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/ok":
				_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"name":"gk"}}`))
			case "/fail":
				_, _ = w.Write([]byte(`{"code":1002,"msg":"denied","data":null}`))
			default:
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
		}))),
	)
	client.SetResponseUnwrapper(unwrapper)
	client.SetResponseStatusChecker(checker)

	var okResult struct {
		Name string `json:"name"`
	}
	okResp, err := client.R().SetResult(&okResult).Get("/ok")
	if err != nil {
		t.Fatalf("ok request failed: %v", err)
	}
	if err := okResp.OK(); err != nil {
		t.Fatalf("expected ok response, got %v", err)
	}
	if okResp.MustOK() != okResp {
		t.Fatalf("must ok should return same response")
	}
	if okResult.Name != "gk" {
		t.Fatalf("unexpected ok result %+v", okResult)
	}

	failResp, err := client.R().Get("/fail")
	if err != nil {
		t.Fatalf("fail request transport error: %v", err)
	}
	if failResp.OK() == nil {
		t.Fatalf("expected business error")
	}
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatalf("expected MustOK panic")
		}
	}()
	failResp.MustOK()
}

func TestRequestLevelProxyOverrideAndPipeline(t *testing.T) {
	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Stage") != "pipeline" {
				t.Fatalf("unexpected stage header %q", r.Header.Get("X-Stage"))
			}
			if r.URL.Query().Get("lang") != "zh" {
				t.Fatalf("unexpected query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte("ok"))
		}))),
	)

	client.SetProxy("http://127.0.0.1:8080")

	var reqStepRan bool
	pipeline := client.NewPipeline(
		func(req *Request) error {
			req.SetHeader("X-Stage", "pipeline")
			req.SetQueryParam("lang", "zh")
			reqStepRan = true
			return nil
		},
		func(req *Request) error {
			req.DisableProxy()
			return nil
		},
	)

	req, err := pipeline.Request()
	if err != nil {
		t.Fatalf("pipeline request failed: %v", err)
	}
	if !reqStepRan {
		t.Fatalf("expected pipeline step to run")
	}
	if !req.disableProxy {
		t.Fatalf("expected request proxy override")
	}

	resp, err := req.Get("/pipeline")
	if err != nil {
		t.Fatalf("pipeline execute failed: %v", err)
	}
	if resp.String() != "ok" {
		t.Fatalf("unexpected response %q", resp.String())
	}
}

func TestSubClientAndStepHelpers(t *testing.T) {
	type result struct {
		Name string `json:"name"`
	}

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("X-App"); got != "sub" {
				t.Fatalf("unexpected header %q", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tk-1" {
				t.Fatalf("unexpected auth %q", got)
			}
			if got := r.URL.Query().Get("lang"); got != "zh" {
				t.Fatalf("unexpected query %q", got)
			}
			if got := r.URL.Path; got != "/users/9" {
				t.Fatalf("unexpected path %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"gk"}`))
		}))),
	)

	sub := client.SubClient(
		WithHeader("X-App", "sub"),
		WithQuery("lang", "zh"),
		WithBearerToken("tk-1"),
	)
	if sub == nil {
		t.Fatalf("expected sub client")
	}

	var out result
	resp, err := sub.NewPipeline(
		WithPathParam("id", "9"),
		WithResult(&out),
	).Execute(http.MethodGet, "/users/{id}")
	if err != nil {
		t.Fatalf("pipeline execute failed: %v", err)
	}
	if out.Name != "gk" {
		t.Fatalf("unexpected result %+v", out)
	}
	if !resp.IsSuccess() {
		t.Fatalf("expected success")
	}
}

func TestRequestLevelRedirectOverride(t *testing.T) {
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/end", http.StatusFound)
		case "/end":
			_, _ = w.Write([]byte("done"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer redirectSrv.Close()

	client := NewClient()
	client.SetFollowRedirects(true)

	resp, err := client.R().Get(redirectSrv.URL + "/start")
	if err != nil {
		t.Fatalf("follow redirect request failed: %v", err)
	}
	if resp.String() != "done" {
		t.Fatalf("unexpected redirect body %q", resp.String())
	}

	noFollowResp, err := client.R().DisableRedirects().Get(redirectSrv.URL + "/start")
	if err != nil {
		t.Fatalf("disable redirect request failed: %v", err)
	}
	if noFollowResp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", noFollowResp.StatusCode)
	}
	if loc := noFollowResp.HeaderGet("Location"); loc != "/end" {
		t.Fatalf("unexpected location %q", loc)
	}
}

func TestRequestRedirectHandlerStopsRedirect(t *testing.T) {
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/end", http.StatusFound)
		case "/end":
			_, _ = w.Write([]byte("done"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer redirectSrv.Close()

	client := NewClient()

	resp, err := client.R().
		AddRedirectHandler(func(resp *Response) bool {
			return false
		}).
		Get(redirectSrv.URL + "/start")
	if err != nil {
		t.Fatalf("redirect handler request failed: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
}

func TestEndpointExecuteAndRequestBuilders(t *testing.T) {
	type result struct {
		Name string `json:"name"`
	}

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method %s", r.Method)
			}
			if r.URL.Path != "/users/7" {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			if r.Header.Get("X-Endpoint") != "1" {
				t.Fatalf("unexpected header %q", r.Header.Get("X-Endpoint"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"gk"}`))
		}))),
	)

	ep := client.NewEndpoint(
		http.MethodPost,
		"/users/{id}",
		WithHeader("X-Endpoint", "1"),
		WithPathParam("id", "7"),
	)

	req, err := ep.Request()
	if err != nil {
		t.Fatalf("endpoint request failed: %v", err)
	}
	if req.Method != http.MethodPost || req.URL != "/users/{id}" {
		t.Fatalf("unexpected request method/url %s %s", req.Method, req.URL)
	}

	var out result
	resp, err := ep.Execute(WithResult(&out))
	if err != nil {
		t.Fatalf("endpoint execute failed: %v", err)
	}
	if out.Name != "gk" || !resp.IsSuccess() {
		t.Fatalf("unexpected endpoint result %+v resp=%+v", out, resp)
	}
}
