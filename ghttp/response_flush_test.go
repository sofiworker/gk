package ghttp

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// errFlushUnclassified 是一个不匹配任何框架哨兵的错误,用于确认兜底 500 分类。
var errFlushUnclassified = errors.New("ghttp: flush test unclassified failure")

// 缺陷 C(M12)回归:Flush 会隐式向底层提交 header(标准库为未 WriteHeader 的响应补
// 200),但它此前不设 written。于是 handler 先 Flush 再返回 error 时,链外的 writeError
// 读到 Written()==false,便去补写错误状态码——标准库打印
// "superfluous response.WriteHeader call" 并丢弃它,客户端实际收到 200,而
// WithErrorHook 记录的是 4xx/5xx:同一请求在两端被观测成两个状态。

// nonFlusherWriter 是一个【不实现 http.Flusher】的 ResponseWriter,用于验证"底层不支持
// 冲刷时不得标记已提交"——什么也没送出,补写错误响应仍然应当被允许。
type nonFlusherWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func newNonFlusherWriter() *nonFlusherWriter {
	return &nonFlusherWriter{header: make(http.Header)}
}

func (w *nonFlusherWriter) Header() http.Header { return w.header }

func (w *nonFlusherWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.body.Write(b)
}

func (w *nonFlusherWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}

func TestResponseFlush_MarksWrittenAndCommits200(t *testing.T) {
	r := &Response{ResponseWriter: httptest.NewRecorder()}
	if r.Written() || r.Status() != 0 {
		t.Fatal("fresh Response should be unwritten with status 0")
	}
	r.Flush()
	if !r.Written() {
		t.Error("Written() = false after Flush, want true (flushing commits the header)")
	}
	if r.Status() != http.StatusOK {
		t.Errorf("Status() = %d after Flush, want 200 (stdlib supplies an implicit 200)", r.Status())
	}
	// Flush 未写响应体,故计数必须仍为 0——BytesOut 的语义是"响应体字节数"。
	if r.BytesOut() != 0 {
		t.Errorf("BytesOut() = %d after Flush, want 0 (Flush writes no body)", r.BytesOut())
	}
}

// TestResponseFlush_DoesNotOverrideExplicitStatus 确认显式状态码不被 Flush 篡改:
// 隐式 200 只在尚未提交时补。
func TestResponseFlush_DoesNotOverrideExplicitStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"teapot", http.StatusTeapot},
		{"created", http.StatusCreated},
		{"not found", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := &Response{ResponseWriter: rec}
			r.WriteHeader(tc.status)
			r.Flush()
			if r.Status() != tc.status {
				t.Errorf("Status() = %d after Flush, want %d preserved", r.Status(), tc.status)
			}
			if rec.Code != tc.status {
				t.Errorf("recorder code = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}

// TestResponseFlush_AfterWriteKeepsBytesOut 确认 Flush 不影响已累计的响应体字节数。
func TestResponseFlush_AfterWriteKeepsBytesOut(t *testing.T) {
	r := &Response{ResponseWriter: httptest.NewRecorder()}
	if _, err := r.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	before := r.BytesOut()
	r.Flush()
	if r.BytesOut() != before {
		t.Errorf("BytesOut() = %d after Flush, want %d unchanged", r.BytesOut(), before)
	}
	if r.Status() != http.StatusOK || !r.Written() {
		t.Errorf("status=%d written=%v, want committed 200", r.Status(), r.Written())
	}
}

// TestResponseFlush_NonFlusherDoesNotCommit 覆盖底层不支持冲刷的情形:没有任何数据被
// 送出,故不得标记已提交,否则 writeError 会放弃一个本可正常写出的错误响应。
func TestResponseFlush_NonFlusherDoesNotCommit(t *testing.T) {
	r := &Response{ResponseWriter: newNonFlusherWriter()}
	r.Flush()
	if r.Written() {
		t.Error("Written() = true after Flush on a non-Flusher writer, want false (nothing was sent)")
	}
	if r.Status() != 0 {
		t.Errorf("Status() = %d, want 0 (nothing was committed)", r.Status())
	}
}

// TestResponseFlush_ResetClearsFlushState 确认池化卫生:Flush 置上的 written/status 必须
// 被 reset 清掉,否则复用该 Response 的下一请求会被当作"已提交"而丢掉错误响应。
func TestResponseFlush_ResetClearsFlushState(t *testing.T) {
	r := &Response{ResponseWriter: httptest.NewRecorder()}
	r.Flush()
	if !r.Written() {
		t.Fatal("precondition: Flush should have marked written")
	}
	r.reset()
	if r.Written() || r.Status() != 0 || r.BytesOut() != 0 || r.ResponseWriter != nil {
		t.Errorf("after reset: written=%v status=%d bytesOut=%d writer=%v, want fully cleared",
			r.Written(), r.Status(), r.BytesOut(), r.ResponseWriter)
	}
}

// TestFlushThenErrorKeepsClientAndHookConsistent 是本缺陷的端到端断言:handler 先 Flush
// 再返回 error 时,客户端收到的状态与 hook 观测到的状态必须一致,且标准库不得打印
// superfluous 警告。
func TestFlushThenErrorKeepsClientAndHookConsistent(t *testing.T) {
	var mu sync.Mutex
	var hookStatus int
	s := New(WithErrorHook(func(_ *http.Request, status int, _ error) {
		mu.Lock()
		defer mu.Unlock()
		hookStatus = status
	}))
	if err := s.RawHandle(http.MethodGet, "/flush", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Flush()
		return ErrInvalidInput
	}); err != nil {
		t.Fatal(err)
	}

	// 捕获标准库经 log 打印的 "superfluous response.WriteHeader call" 警告。
	var logBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prev)

	srv := httptest.NewServer(s)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/flush")
	if err != nil {
		t.Fatal(err)
	}
	clientStatus := resp.StatusCode
	_ = resp.Body.Close()
	// 关闭服务器以确保 net/http 的任何延迟日志都已落盘,再读日志缓冲。
	srv.Close()

	// 已 Flush 即已提交:客户端只能收到隐式 200,补写不可能生效。
	if clientStatus != http.StatusOK {
		t.Errorf("client status = %d, want 200 (the flush already committed the header)", clientStatus)
	}
	mu.Lock()
	got := hookStatus
	mu.Unlock()
	if got != clientStatus {
		t.Errorf("hook status = %d but client saw %d; the two observations must agree", got, clientStatus)
	}
	if logged := logBuf.String(); strings.Contains(logged, "superfluous") {
		t.Errorf("stdlib logged a superfluous WriteHeader warning: %q", strings.TrimSpace(logged))
	}
}

// TestFlushThenErrorSuppressesErrorBody 断言已提交响应不再被补写错误体:writeError 只
// 记录、不改写。
func TestFlushThenErrorSuppressesErrorBody(t *testing.T) {
	s := New()
	if err := s.RawHandle(http.MethodGet, "/flush", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Flush()
		return ErrInvalidInput
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/flush", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "invalid_input") {
		t.Errorf("body = %q, want no error body appended after commit", rec.Body.String())
	}
}

// TestFlushThenErrorPreservesStreamedBody 确认已冲刷出去的流式内容不被错误体污染——
// 这正是 SSE 等场景依赖的行为。
func TestFlushThenErrorPreservesStreamedBody(t *testing.T) {
	s := New()
	if err := s.RawHandle(http.MethodGet, "/stream", func(_ context.Context, _ *Request, resp *Response) error {
		if _, err := resp.WriteString("chunk-1\n"); err != nil {
			return err
		}
		resp.Flush()
		return ErrInvalidInput
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "chunk-1\n" {
		t.Errorf("body = %q, want exactly the streamed chunk", got)
	}
}

// TestErrorHookReportsClassifiedStatusWhenUncommitted 对照组:响应【未】提交时,钩子仍
// 须收到分类结果(即将真正写出的那个码),该路径不受已提交分支的改动影响。
func TestErrorHookReportsClassifiedStatusWhenUncommitted(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"invalid input maps to 400", ErrInvalidInput, http.StatusBadRequest},
		{"unsupported media maps to 415", ErrUnsupportedMediaType, http.StatusUnsupportedMediaType},
		{"unclassified maps to 500", errFlushUnclassified, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hookStatus int
			s := New(WithErrorHook(func(_ *http.Request, status int, _ error) { hookStatus = status }))
			if err := s.RawHandle(http.MethodGet, "/fail", func(_ context.Context, _ *Request, _ *Response) error {
				return tc.err
			}); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fail", nil))
			if rec.Code != tc.wantStatus {
				t.Errorf("client status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if hookStatus != tc.wantStatus {
				t.Errorf("hook status = %d, want %d", hookStatus, tc.wantStatus)
			}
			if hookStatus != rec.Code {
				t.Errorf("hook saw %d but client saw %d; the two must agree", hookStatus, rec.Code)
			}
		})
	}
}

// TestErrorHookReportsDeliveredStatusWhenCommitted 覆盖已提交分支的一般形态(不限于
// Flush):handler 显式写了一个状态码后再返回一个分类到别处的 error,钩子必须报出客户端
// 实际收到的那个码,而不是错误分类出的码。
func TestErrorHookReportsDeliveredStatusWhenCommitted(t *testing.T) {
	var hookStatus int
	var hookErr error
	s := New(WithErrorHook(func(_ *http.Request, status int, err error) {
		hookStatus, hookErr = status, err
	}))
	if err := s.RawHandle(http.MethodGet, "/late", func(_ context.Context, _ *Request, resp *Response) error {
		resp.WriteHeader(http.StatusAccepted)
		return ErrInvalidInput
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/late", nil))

	if rec.Code != http.StatusAccepted {
		t.Errorf("client status = %d, want 202 (a committed response must not be rewritten)", rec.Code)
	}
	if hookStatus != rec.Code {
		t.Errorf("hook saw %d but client saw %d; the two must agree", hookStatus, rec.Code)
	}
	// 状态码口径改变,但错误本身必须原样送达,否则根因丢失。
	if !errors.Is(hookErr, ErrInvalidInput) {
		t.Errorf("hook err = %v, want ErrInvalidInput passed through unchanged", hookErr)
	}
	// 分类语义仍可从 err 取回,故可观测性不降级。
	if got := HTTPStatus(hookErr); got != http.StatusBadRequest {
		t.Errorf("HTTPStatus(err) = %d, want 400 still recoverable from the error", got)
	}
}

// TestFlushPoolHygieneAcrossRequests 覆盖池化复用:一个 Flush 过的请求之后,复用同一
// 池化对象的下一请求仍须能正常写出错误响应(即 written 未泄漏)。
func TestFlushPoolHygieneAcrossRequests(t *testing.T) {
	s := New()
	if err := s.RawHandle(http.MethodGet, "/flushed", func(_ context.Context, _ *Request, resp *Response) error {
		resp.Flush()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RawHandle(http.MethodGet, "/failing", func(_ context.Context, _ *Request, resp *Response) error {
		if resp.Written() {
			t.Error("/failing saw a pre-committed Response (written leaked through the pool)")
		}
		return ErrInvalidInput
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/flushed", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("/flushed status = %d, want 200", rec.Code)
		}
		rec = httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/failing", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("/failing status = %d, want 400 (a fresh Response must still be writable)", rec.Code)
		}
	}
}
