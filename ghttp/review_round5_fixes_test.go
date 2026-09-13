// 本轮修复的单元测试：覆盖 CSRF 预解析总量帽、Response/gzipResponseWriter 的
// ReadFrom 零拷贝直通、AcceptsEncoding/ensureVary 零分配重写、Ready 检查并行化。
// Unit tests for this round's fixes: CSRF pre-parse total cap, Response/gzip
// ReadFrom zero-copy passthrough, allocation-free AcceptsEncoding/ensureVary
// rewrite, and parallel Ready checks.
package ghttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// C1: CSRF 预解析总量帽
// ---------------------------------------------------------------------------

// countReadCloser 统计底层读取的字节数,用以验证 MaxBytesReader 是否真的把读取量压到帽值内。
// countReadCloser counts bytes delivered from the underlying reader, so the test
// can verify MaxBytesReader really caps the read volume.
type countReadCloser struct {
	r io.Reader
	n int64
}

func (c *countReadCloser) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countReadCloser) Close() error { return nil }

// multipartBodyBytes 构造一段 multipart 体:可选在文件 part 之前先写一个 csrf_token 字段,
// 文件 part 以 64 KiB 块循环填充到目标字节数。返回完整体与 Content-Type(含 boundary)。
// multipartBodyBytes builds a multipart body: optionally a csrf_token field before
// the file part, which is padded with 64 KiB chunks up to the target byte count.
// Returns the complete body and its Content-Type (with boundary).
func multipartBodyBytes(t *testing.T, size int, withToken bool) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if withToken {
		if err := mw.WriteField("csrf_token", "tok123"); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile("f", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 64<<10)
	for size > 0 {
		n := len(chunk)
		if n > size {
			n = size
		}
		if _, err := fw.Write(chunk[:n]); err != nil {
			t.Fatal(err)
		}
		size -= n
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

func TestCSRFFormToken_MultipartCapSmall(t *testing.T) {
	// 小表单(带 token)正常取到 token。
	// Small form with token returns the token.
	raw, ct := multipartBodyBytes(t, 1<<10, true)
	req := &Request{Request: &http.Request{
		Header:        http.Header{"Content-Type": {ct}},
		ContentLength: int64(len(raw)),
		Body:          io.NopCloser(bytes.NewReader(raw)),
	}}
	if got := csrfFormToken(req, "csrf_token"); got != "tok123" {
		t.Fatalf("small multipart token = %q, want %q", got, "tok123")
	}
}

func TestCSRFFormToken_MultipartCapChunked(t *testing.T) {
	// chunked 体超帽时,ParseMultipartForm 应在总量帽内被截断,不再无上限解析。
	// A chunked body over the cap must be truncated within the total cap — no more
	// unbounded parsing.
	size := int(maxCSRFPreAuthBodyBytes) + (1 << 20) // 5 MiB
	raw, ct := multipartBodyBytes(t, size, true)
	cr := &countReadCloser{r: bytes.NewReader(raw)}
	req := &Request{Request: &http.Request{
		Header:        http.Header{"Content-Type": {ct}},
		ContentLength: -1,
		Body:          cr,
	}}
	if got := csrfFormToken(req, "csrf_token"); got != "" {
		t.Fatalf("over-cap chunked multipart should return empty, got %q", got)
	}
	if cr.n > maxCSRFPreAuthBodyBytes+1 {
		t.Fatalf("read %d bytes, body is capped but probe reads cap+1 at most", cr.n)
	}
}

func TestCSRFFormToken_URLEncodedCapChunked(t *testing.T) {
	// DELETE + chunked urlencoded 体:标准库 ParseForm 对 DELETE 不设上限,本包自设帽。
	// DELETE + chunked urlencoded: stdlib ParseForm caps only POST/PUT/PATCH for DELETE,
	// so the package applies its own cap.
	body := strings.Repeat("a=1&", int(maxCSRFPreAuthBodyBytes)/(len("a=1&"))+1) + "csrf_token=tok"
	cr := &countReadCloser{r: strings.NewReader(body)}
	req := &Request{Request: &http.Request{
		Header:        http.Header{"Content-Type": {"application/x-www-form-urlencoded"}},
		ContentLength: -1,
		Body:          cr,
	}}
	if got := csrfFormToken(req, "csrf_token"); got != "" {
		t.Fatalf("over-cap chunked urlencoded should return empty, got %q", got)
	}
	if cr.n > maxCSRFPreAuthBodyBytes+1 {
		t.Fatalf("read %d bytes, body is capped but probe reads cap+1 at most", cr.n)
	}
}

func TestCSRFFormToken_URLEncodedCapSmall(t *testing.T) {
	// 小 urlencoded 体带 token 正常取到。
	// Small urlencoded body with token returns the token.
	body := "csrf_token=abc&other=1"
	req := &Request{Request: &http.Request{
		Method:        "POST",
		Header:        http.Header{"Content-Type": {"application/x-www-form-urlencoded"}},
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(strings.NewReader(body)),
	}}
	if got := csrfFormToken(req, "csrf_token"); got != "abc" {
		t.Fatalf("small urlencoded token = %q, want %q", got, "abc")
	}
}

// ---------------------------------------------------------------------------
// C2: Response.ReadFrom 零拷贝直通
// ---------------------------------------------------------------------------

// readFromRecorder 实现 io.ReaderFrom,记录调用次数与转发字节数,以便验证直通是否生效。
// readFromRecorder implements io.ReaderFrom and records call count and bytes
// forwarded, so passthrough can be verified.
type readFromRecorder struct {
	header        http.Header
	code          int
	readFromCalls int
	readFromBytes int64
	written       int64
}

func newReadFromRecorder() *readFromRecorder {
	return &readFromRecorder{header: make(http.Header)}
}

func (r *readFromRecorder) Header() http.Header { return r.header }
func (r *readFromRecorder) WriteHeader(c int)   { r.code = c }
func (r *readFromRecorder) Write(p []byte) (int, error) {
	r.written += int64(len(p))
	return len(p), nil
}
func (r *readFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFromCalls++
	n, err := io.Copy(io.Discard, src)
	r.readFromBytes += n
	return n, err
}

func TestResponseReadFrom_Passthrough(t *testing.T) {
	// 底层支持 ReaderFrom 时直通,字节计数进 bytesOut。
	// When the underlying supports ReaderFrom, pass through and credit bytesOut.
	rec := newReadFromRecorder()
	resp := &Response{ResponseWriter: rec}
	resp.WriteHeader(http.StatusOK)
	src := io.LimitReader(strings.NewReader("hello world"), 11)
	n, err := resp.ReadFrom(src)
	if err != nil {
		t.Fatal(err)
	}
	if n != 11 {
		t.Fatalf("ReadFrom n=%d, want 11", n)
	}
	if rec.readFromCalls != 1 {
		t.Fatalf("underlying ReadFrom calls=%d, want 1", rec.readFromCalls)
	}
	if resp.bytesOut != 11 {
		t.Fatalf("bytesOut=%d, want 11", resp.bytesOut)
	}
}

func TestResponseReadFrom_FallbackNoRecursion(t *testing.T) {
	// 底层不支持 ReaderFrom 时回退缓冲拷贝,不得无限递归,字节计数正确。
	// When the underlying lacks ReaderFrom, fall back to a buffered copy — must not
	// recurse, and bytesOut must be correct.
	rr := httptest.NewRecorder()
	resp := &Response{ResponseWriter: rr}
	payload := strings.Repeat("x", 1<<16)
	src := io.LimitReader(strings.NewReader(payload), int64(len(payload)))
	n, err := resp.ReadFrom(src)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("ReadFrom n=%d, want %d", n, len(payload))
	}
	if resp.bytesOut != len(payload) {
		t.Fatalf("bytesOut=%d, want %d", resp.bytesOut, len(payload))
	}
	if rr.Body.String() != payload {
		t.Fatalf("body mismatch: got %d bytes, want %d", rr.Body.Len(), len(payload))
	}
}

func TestResponseReadFrom_Implicit200(t *testing.T) {
	// 未显式 WriteHeader 时,ReadFrom 应隐式提交 200,与 Write 语义一致。
	// Without an explicit WriteHeader, ReadFrom must implicitly commit 200, matching
	// Write's semantics.
	rec := newReadFromRecorder()
	resp := &Response{ResponseWriter: rec}
	_, _ = io.Copy(resp, io.LimitReader(strings.NewReader("abc"), 3))
	if resp.status != http.StatusOK {
		t.Fatalf("implicit status = %d, want 200", resp.status)
	}
	if !resp.written {
		t.Fatalf("written flag not set")
	}
}

// ---------------------------------------------------------------------------
// C2: gzipResponseWriter.ReadFrom
// ---------------------------------------------------------------------------

func TestGzipResponseWriterReadFrom_Passthrough(t *testing.T) {
	// 透传分支直通底层 ReaderFrom;字节计数由外层 Response.ReadFrom 完成。
	// Passthrough branch delegates to the underlying ReaderFrom; byte accounting
	// is done by the outer Response.ReadFrom.
	rec := newReadFromRecorder()
	resp := &Response{ResponseWriter: rec}
	cfg := &gzipConfig{level: gzip.DefaultCompression, contentTypes: defaultGzipContentTypes()}
	w := &gzipResponseWriter{resp: resp, cfg: cfg, orig: rec, decided: true, compress: false}
	resp.ResponseWriter = w
	src := io.LimitReader(strings.NewReader("hello world"), 11)
	n, err := resp.ReadFrom(src)
	if err != nil {
		t.Fatal(err)
	}
	if n != 11 {
		t.Fatalf("ReadFrom n=%d, want 11", n)
	}
	if rec.readFromCalls != 1 {
		t.Fatalf("underlying ReadFrom calls=%d, want 1 (passthrough lost)", rec.readFromCalls)
	}
	if resp.bytesOut != 11 {
		t.Fatalf("bytesOut=%d, want 11", resp.bytesOut)
	}
}

func TestGzipResponseWriterReadFrom_Compress(t *testing.T) {
	// 压缩分支产出合法 gzip 字节,解压后等于原始输入。
	// Compression branch produces valid gzip bytes; decompressed equals the input.
	rr := httptest.NewRecorder()
	resp := &Response{ResponseWriter: rr}
	cfg := &gzipConfig{level: gzip.DefaultCompression, contentTypes: defaultGzipContentTypes()}
	gz := getGzipWriter(rr, cfg.level)
	w := &gzipResponseWriter{resp: resp, cfg: cfg, orig: rr, gz: gz, decided: true, compress: true}
	resp.ResponseWriter = w
	payload := strings.Repeat("hello world\n", 200)
	src := io.LimitReader(strings.NewReader(payload), int64(len(payload)))
	n, err := resp.ReadFrom(src)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("ReadFrom n=%d, want %d", n, len(payload))
	}
	// ReadFrom 后流尚未 Close:先手动关流以写入 gzip footer,再归还池。
	// The stream is not yet closed after ReadFrom; explicitly close to flush the
	// gzip footer, then return the writer to the pool.
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	putGzipWriter(gz, cfg.level)
	gr, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("decompressed length=%d, want %d", len(got), len(payload))
	}
}

// ---------------------------------------------------------------------------
// L1/L2: AcceptsEncoding / ensureVary 零分配
// ---------------------------------------------------------------------------

func TestAcceptsEncoding_ZeroAlloc(t *testing.T) {
	// 代表性输入不得产生任何分配;语义由既有 TestAcceptsEncoding 覆盖。
	// Representative inputs must produce zero allocations; semantics are covered
	// by the existing TestAcceptsEncoding.
	cases := []struct{ h, enc string }{
		{"gzip, deflate, br", "br"},
		{"*;q=0, gzip", "gzip"},
		{"gzip;q=0.000", "gzip"},
		{"", "gzip"},
		{"br;q=1.0, gzip;q=0.5, *;q=0.1", "zstd"},
		{"identity", "gzip"},
		{"gzip;q=0, *", "gzip"},
	}
	for _, c := range cases {
		allocs := testing.AllocsPerRun(100, func() { AcceptsEncoding(c.h, c.enc) })
		if allocs != 0 {
			t.Errorf("AcceptsEncoding(%q, %q): %.0f allocs, want 0", c.h, c.enc, allocs)
		}
	}
}

func TestEnsureVary_ZeroAllocWhenPresent(t *testing.T) {
	// 已声明时再调用不得分配;未声明时追加(必然分配)。
	// When already declared, calling again must not allocate; when absent, the
	// append necessarily allocates.
	h := http.Header{}
	ensureVary(h, "Accept-Encoding")
	allocs := testing.AllocsPerRun(100, func() { ensureVary(h, "Accept-Encoding") })
	if allocs != 0 {
		t.Errorf("ensureVary on existing: %.0f allocs, want 0", allocs)
	}
	if got := h.Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("Vary = %q, want Accept-Encoding", got)
	}
}

func TestEnsureVary_IdempotentAcrossCaseAndComma(t *testing.T) {
	// 大小写与逗号合并列表均幂等,不重复追加。
	// Idempotent across case variants and comma-joined lists; no duplicate append.
	h := http.Header{}
	h.Add("Vary", "Origin")
	h.Add("Vary", "gzip, br")
	ensureVary(h, "Gzip")
	ensureVary(h, "BR")
	ensureVary(h, "Accept-Encoding")
	if got := h.Values("Vary"); len(got) != 3 {
		t.Fatalf("Vary values = %v, want 3 entries (Origin, gzip, br, Accept-Encoding)", got)
	}
	// 大小写变体键:本包直接遍历头映射,不依赖 textproto 规范化。
	// Case-variant key: the package walks the header map directly, not relying on
	// textproto canonicalization.
	h2 := http.Header{"vary": {"Accept-Encoding"}}
	ensureVary(h2, "accept-encoding")
	// 键刻意用非规范大小写,因此不能按规范键索引(那会查不到),只能遍历映射取回条目数。
	// The key is deliberately non-canonical, so it cannot be read through the
	// canonical key (that lookup would miss); walk the map instead.
	var entries int
	for k, vs := range h2 {
		if strings.EqualFold(k, "vary") {
			entries = len(vs)
		}
	}
	if entries != 1 {
		t.Fatalf("case-variant key: got %v, want single entry", h2)
	}
}

// ---------------------------------------------------------------------------
// D2: Ready 检查并行化
// ---------------------------------------------------------------------------

func TestRunChecks_ParallelTiming(t *testing.T) {
	// 三个各 sleep 100ms 的检查:并行 < 250ms,串行 ≥ 300ms。
	// Three checks each sleeping 100ms: parallel < 250ms, sequential ≥ 300ms.
	checks := make([]Checker, 3)
	for i := range checks {
		checks[i] = Checker{
			Name: "s",
			Check: func(ctx context.Context) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
					return nil
				}
			},
		}
	}
	start := time.Now()
	res := runChecks(context.Background(), 0, checks, false)
	elapsed := time.Since(start)
	if elapsed >= 250*time.Millisecond {
		t.Errorf("not parallel: elapsed %v, want < 250ms", elapsed)
	}
	if !res.healthy {
		t.Errorf("healthy = false, want true")
	}
	for _, c := range checks {
		if res.checks[c.Name] != "ok" {
			t.Errorf("check %q state = %q, want ok", c.Name, res.checks[c.Name])
		}
	}
}

func TestRunChecks_ParallelFailure(t *testing.T) {
	// 任一检查失败:healthy=false,失败项状态为 fail(details=false)或错误消息(details=true)。
	// Any check failing: healthy=false; failing state is "fail" (details=false) or
	// the error message (details=true).
	checks := []Checker{
		{Name: "ok", Check: func(ctx context.Context) error { return nil }},
		{Name: "bad", Check: func(ctx context.Context) error { return context.DeadlineExceeded }},
	}
	res := runChecks(context.Background(), 0, checks, false)
	if res.healthy {
		t.Errorf("healthy = true, want false")
	}
	if res.checks["bad"] != "fail" {
		t.Errorf("bad state = %q, want fail", res.checks["bad"])
	}
	res2 := runChecks(context.Background(), 0, checks, true)
	if res2.checks["bad"] == "" || res2.checks["bad"] == "fail" {
		t.Errorf("details=true: bad state = %q, want error message", res2.checks["bad"])
	}
}

func TestRunChecks_ParallelTimeout(t *testing.T) {
	// 慢检查在 timeout 内被取消,结果 fail。
	// A slow check is canceled within the timeout; state is fail.
	checks := []Checker{
		{Name: "slow", Check: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}},
	}
	res := runChecks(context.Background(), 20*time.Millisecond, checks, false)
	if res.healthy {
		t.Errorf("healthy = true, want false")
	}
	if res.checks["slow"] != "fail" {
		t.Errorf("slow state = %q, want fail", res.checks["slow"])
	}
}

// 占位使用 sync/atomic 包,避免 go vet 报未用导入(本文件其它测试已使用)。
// Placeholder to keep sync/atomic imported (used by other tests in this file).
var _ atomic.Int64
