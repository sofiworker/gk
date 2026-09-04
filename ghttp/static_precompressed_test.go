package ghttp

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// ===========================================================================
// 静态预压缩测试。关键不变量:
//   - 未开启选项时行为与该能力不存在时完全一致(零成本保证);
//   - 命中变体时 Content-Type 取【原始】扩展名,而非 .br/.gz 的类型;
//   - 无论命中与否,开启后都必须发 Vary: Accept-Encoding(共享缓存正确性)。
// Static pre-compression tests. Key invariants: zero cost when unused, the
// Content-Type comes from the ORIGINAL extension (not .br/.gz), and Vary:
// Accept-Encoding is emitted whenever the feature is on (shared-cache correctness).
// ===========================================================================

// 变体内容用可辨识的字面量而非真实压缩数据:测试断言的是"选中了哪个文件"与响应头,
// 与压缩算法本身无关;真实 br/gzip 字节反而让失败信息不可读。
// Variant contents are recognizable literals rather than real compressed bytes: the
// assertions concern which file was chosen and the headers, not the codec itself.
const (
	precompPlainJS  = "console.log('plain');"
	precompGzipJS   = "GZIP-BYTES-FOR-APP-JS"
	precompBrotliJS = "BROTLI-BYTES-FOR-APP-JS"
	precompOnlyGz   = "GZIP-BYTES-FOR-ONLY-GZ"
	precompNoVar    = "console.log('no variant');"
	precompIndexRaw = "<h1>index</h1>"
	precompIndexBr  = "BROTLI-BYTES-FOR-INDEX-HTML"
)

// precompFS 构造含各类变体组合的虚拟文件系统:
// app.js 同时有 .gz/.br;only-gz.js 仅有 .gz;plain.js 无变体;index.html 仅有 .br。
// precompFS builds a virtual FS covering the variant combinations.
func precompFS() fstest.MapFS {
	return fstest.MapFS{
		"app.js":          {Data: []byte(precompPlainJS)},
		"app.js.gz":       {Data: []byte(precompGzipJS)},
		"app.js.br":       {Data: []byte(precompBrotliJS)},
		"only-gz.js":      {Data: []byte("console.log('only gz');")},
		"only-gz.js.gz":   {Data: []byte(precompOnlyGz)},
		"plain.js":        {Data: []byte(precompNoVar)},
		"index.html":      {Data: []byte(precompIndexRaw)},
		"index.html.br":   {Data: []byte(precompIndexBr)},
		"css/site.css":    {Data: []byte("body{margin:0}")},
		"css/site.css.br": {Data: []byte("BROTLI-BYTES-FOR-SITE-CSS")},
	}
}

// servePrecomp 挂载 precompFS 到 /assets/ 并发一次请求。acceptEncoding 为空表示【不发】
// Accept-Encoding 头(区别于发一个空值头)。
// servePrecomp mounts precompFS under /assets/ and issues one request. An empty
// acceptEncoding means the header is NOT sent at all.
func servePrecomp(t *testing.T, method, path, acceptEncoding string, opts ...StaticOption) *httptest.ResponseRecorder {
	t.Helper()
	m := New()
	if err := StaticFS(m, "/assets/", precompFS(), opts...); err != nil {
		t.Fatalf("StaticFS: %v", err)
	}
	r := httptest.NewRequest(method, path, nil)
	if acceptEncoding != "" {
		r.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, r)
	return rec
}

// assertVaryAcceptEncoding 断言 Vary 头声明了 Accept-Encoding。开启预压缩后无论命中与否
// 都必须声明:少了它,共享缓存可能把 br 响应喂给不支持 br 的客户端。
// assertVaryAcceptEncoding asserts Vary declares Accept-Encoding — required on hit
// AND miss, else a shared cache may hand a br body to a client without br support.
func assertVaryAcceptEncoding(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, v := range rec.Header().Values("Vary") {
		if strings.Contains(strings.ToLower(v), "accept-encoding") {
			return
		}
	}
	t.Errorf("Vary=%v, want it to contain Accept-Encoding", rec.Header().Values("Vary"))
}

// --- 默认关闭:零成本 ----------------------------------------------------------
// --- Off by default: zero cost ------------------------------------------------

// TestStaticPrecompressed_DisabledByDefault 验证不传选项时完全不做变体替换:即便客户端
// 明确接受 br/gzip 且变体就在"磁盘"上,也照原样服务原始文件,且不写 Content-Encoding、
// 不写 Vary。这是"不用则零成本"的行为保证。
func TestStaticPrecompressed_DisabledByDefault(t *testing.T) {
	rec := servePrecomp(t, http.MethodGet, "/assets/app.js", "br, gzip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != precompPlainJS {
		t.Errorf("body=%q, want the original file %q", got, precompPlainJS)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("Content-Encoding=%q, want none when pre-compression is off", ce)
	}
	if vary := rec.Header().Values("Vary"); len(vary) != 0 {
		t.Errorf("Vary=%v, want none when pre-compression is off", vary)
	}
}

// --- 开启:变体选择 ------------------------------------------------------------
// --- Enabled: variant selection -----------------------------------------------

// TestStaticPrecompressed_VariantSelection 表驱动覆盖启用后的变体选择与回退。
func TestStaticPrecompressed_VariantSelection(t *testing.T) {
	cases := []struct {
		name           string
		path           string
		acceptEncoding string
		opts           []StaticOption
		wantBody       string
		wantEncoding   string // "" 表示不应设置 Content-Encoding
	}{
		{
			name:           "br preferred over gzip when both acceptable and both exist",
			path:           "/assets/app.js",
			acceptEncoding: "br, gzip",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       precompBrotliJS,
			wantEncoding:   "br",
		},
		{
			name:           "gzip chosen when only gzip is acceptable",
			path:           "/assets/app.js",
			acceptEncoding: "gzip",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       precompGzipJS,
			wantEncoding:   "gzip",
		},
		{
			name:           "gzip chosen when br is acceptable but only .gz exists",
			path:           "/assets/only-gz.js",
			acceptEncoding: "br, gzip",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       precompOnlyGz,
			wantEncoding:   "gzip",
		},
		{
			name:           "identity only falls back to the original",
			path:           "/assets/app.js",
			acceptEncoding: "identity",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       precompPlainJS,
			wantEncoding:   "",
		},
		{
			name:           "absent Accept-Encoding falls back to the original",
			path:           "/assets/app.js",
			acceptEncoding: "",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       precompPlainJS,
			wantEncoding:   "",
		},
		{
			name:           "no variant on disk falls back to the original",
			path:           "/assets/plain.js",
			acceptEncoding: "br, gzip",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       precompNoVar,
			wantEncoding:   "",
		},
		{
			name:           "explicit refusal via q=0 falls back to the original",
			path:           "/assets/app.js",
			acceptEncoding: "br;q=0, gzip;q=0",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       precompPlainJS,
			wantEncoding:   "",
		},
		{
			name:           "wildcard Accept-Encoding picks the highest-priority variant",
			path:           "/assets/app.js",
			acceptEncoding: "*",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       precompBrotliJS,
			wantEncoding:   "br",
		},
		{
			name:           "nested path variant is honored",
			path:           "/assets/css/site.css",
			acceptEncoding: "br",
			opts:           []StaticOption{WithPrecompressed()},
			wantBody:       "BROTLI-BYTES-FOR-SITE-CSS",
			wantEncoding:   "br",
		},
		{
			name:           "WithPrecompressedEncodings(gzip) enables gzip only",
			path:           "/assets/app.js",
			acceptEncoding: "br, gzip",
			opts:           []StaticOption{WithPrecompressedEncodings("gzip")},
			wantBody:       precompGzipJS,
			wantEncoding:   "gzip",
		},
		{
			name:           "WithPrecompressedEncodings(gzip,br) honors the given priority",
			path:           "/assets/app.js",
			acceptEncoding: "br, gzip",
			opts:           []StaticOption{WithPrecompressedEncodings("gzip", "br")},
			wantBody:       precompGzipJS,
			wantEncoding:   "gzip",
		},
		{
			name:           "WithPrecompressedEncodings(br) still serves gzip-less files raw",
			path:           "/assets/only-gz.js",
			acceptEncoding: "br, gzip",
			opts:           []StaticOption{WithPrecompressedEncodings("br")},
			wantBody:       "console.log('only gz');",
			wantEncoding:   "",
		},
		{
			name:           "aliases brotli and gz are recognized",
			path:           "/assets/app.js",
			acceptEncoding: "gzip",
			opts:           []StaticOption{WithPrecompressedEncodings("brotli", "gz")},
			wantBody:       precompGzipJS,
			wantEncoding:   "gzip",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := servePrecomp(t, http.MethodGet, c.path, c.acceptEncoding, c.opts...)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", rec.Code)
			}
			if got := rec.Body.String(); got != c.wantBody {
				t.Errorf("body=%q, want %q", got, c.wantBody)
			}
			if got := rec.Header().Get("Content-Encoding"); got != c.wantEncoding {
				t.Errorf("Content-Encoding=%q, want %q", got, c.wantEncoding)
			}
			// 开启预压缩后,命中与未命中都必须声明 Vary。
			assertVaryAcceptEncoding(t, rec)
		})
	}
}

// TestStaticPrecompressed_NonsenseEncodingsBehavesLikeOff 验证 WithPrecompressedEncodings
// 全部取值无效时等价于关闭:不替换变体、不写 Content-Encoding,也【不】写 Vary
// (precompressed 列表为空 ⇒ 响应不随 Accept-Encoding 而变,发 Vary 会白白降低缓存命中)。
func TestStaticPrecompressed_NonsenseEncodingsBehavesLikeOff(t *testing.T) {
	rec := servePrecomp(t, http.MethodGet, "/assets/app.js", "br, gzip",
		WithPrecompressedEncodings("nonsense", "deflate", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != precompPlainJS {
		t.Errorf("body=%q, want the original %q", got, precompPlainJS)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("Content-Encoding=%q, want none", ce)
	}
	if vary := rec.Header().Values("Vary"); len(vary) != 0 {
		t.Errorf("Vary=%v, want none (no enabled variants means the response does not vary)", vary)
	}
}

// --- Content-Type 正确性(最关键的一条)----------------------------------------
// --- Content-Type correctness (the critical property) -------------------------

// TestStaticPrecompressed_ContentTypeFromOriginalExtension 验证 Content-Type 取【原始】
// 扩展名。若交给 ServeFileFS 按 .br/.gz 推断,浏览器解压后会拿到 application/gzip 之类
// 而无法解析——这是预压缩最容易踩且后果最严重的错误,故逐项显式断言。
func TestStaticPrecompressed_ContentTypeFromOriginalExtension(t *testing.T) {
	cases := []struct {
		name           string
		path           string
		acceptEncoding string
		wantEncoding   string
		wantCTContains string
	}{
		{"js served as br", "/assets/app.js", "br", "br", "javascript"},
		{"js served as gzip", "/assets/app.js", "gzip", "gzip", "javascript"},
		{"css served as br", "/assets/css/site.css", "br", "br", "css"},
		{"html index served as br", "/assets/", "br", "br", "html"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := servePrecomp(t, http.MethodGet, c.path, c.acceptEncoding, WithPrecompressed())
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != c.wantEncoding {
				t.Fatalf("Content-Encoding=%q, want %q", got, c.wantEncoding)
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, c.wantCTContains) {
				t.Errorf("Content-Type=%q, want it to contain %q (must come from the ORIGINAL extension)", ct, c.wantCTContains)
			}
			// 显式排除变体自身的类型:这些是回归时最可能出现的错误值。
			for _, bad := range []string{"gzip", "x-gzip", "octet-stream", "brotli"} {
				if strings.Contains(ct, bad) {
					t.Errorf("Content-Type=%q must not be the variant's own type (%q)", ct, bad)
				}
			}
		})
	}
}

// --- 直接请求变体 --------------------------------------------------------------
// --- Requesting a variant directly --------------------------------------------

// TestStaticPrecompressed_DirectVariantRequestNotDoubleEncoded 验证直接请求 /app.js.br
// 或 /app.js.gz 时不再叠加 Content-Encoding。pickPrecompressed 的后缀守卫负责此事:
// 否则会声明成 "br 编码的 app.js.br",客户端解压一次后仍是压缩数据。
func TestStaticPrecompressed_DirectVariantRequestNotDoubleEncoded(t *testing.T) {
	cases := []struct {
		path     string
		wantBody string
	}{
		{"/assets/app.js.br", precompBrotliJS},
		{"/assets/app.js.gz", precompGzipJS},
		{"/assets/index.html.br", precompIndexBr},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			rec := servePrecomp(t, http.MethodGet, c.path, "br, gzip", WithPrecompressed())
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", rec.Code)
			}
			if got := rec.Body.String(); got != c.wantBody {
				t.Errorf("body=%q, want %q", got, c.wantBody)
			}
			if ce := rec.Header().Get("Content-Encoding"); ce != "" {
				t.Errorf("Content-Encoding=%q, want none for a directly requested variant", ce)
			}
		})
	}
}

// --- 目录索引 / SPA 回退 -------------------------------------------------------
// --- Directory index / SPA fallback -------------------------------------------

// TestStaticPrecompressed_DirectoryIndex 验证目录请求也走预压缩:/ → index.html.br,
// 且 Content-Type 保持 HTML。
func TestStaticPrecompressed_DirectoryIndex(t *testing.T) {
	rec := servePrecomp(t, http.MethodGet, "/assets/", "br, gzip", WithPrecompressed())
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != precompIndexBr {
		t.Errorf("body=%q, want the compressed index %q", got, precompIndexBr)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "br" {
		t.Errorf("Content-Encoding=%q, want br", ce)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "html") {
		t.Errorf("Content-Type=%q, want it to contain html", ct)
	}
	assertVaryAcceptEncoding(t, rec)

	// 不接受任何变体时目录索引回退到原始 index.html。
	raw := servePrecomp(t, http.MethodGet, "/assets/", "identity", WithPrecompressed())
	if got := raw.Body.String(); got != precompIndexRaw {
		t.Errorf("identity: body=%q, want the raw index %q", got, precompIndexRaw)
	}
	if ce := raw.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("identity: Content-Encoding=%q, want none", ce)
	}
}

// TestStaticPrecompressed_SPAFallback 验证 SPA 回退与预压缩叠加:未命中路径服务压缩后的
// 根 index,Content-Encoding 为 br 且 Content-Type 仍是 HTML(前端路由页面必须能被解析)。
func TestStaticPrecompressed_SPAFallback(t *testing.T) {
	rec := servePrecomp(t, http.MethodGet, "/assets/some/client/route", "br, gzip",
		WithSPAFallback(), WithPrecompressed())
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != precompIndexBr {
		t.Errorf("body=%q, want the compressed index %q", got, precompIndexBr)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "br" {
		t.Errorf("Content-Encoding=%q, want br", ce)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "html") {
		t.Errorf("Content-Type=%q, want it to contain html", ct)
	}
	assertVaryAcceptEncoding(t, rec)
}

// --- HEAD ---------------------------------------------------------------------

// TestStaticPrecompressed_HeadRequest 验证 HEAD 与 GET 的头一致但无响应体:客户端据 HEAD
// 头决定是否下载,头不一致会让它按错误的编码解析后续 GET。
func TestStaticPrecompressed_HeadRequest(t *testing.T) {
	head := servePrecomp(t, http.MethodHead, "/assets/app.js", "br, gzip", WithPrecompressed())
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d, want 200", head.Code)
	}
	if head.Body.Len() != 0 {
		t.Errorf("HEAD body len=%d, want 0", head.Body.Len())
	}
	if ce := head.Header().Get("Content-Encoding"); ce != "br" {
		t.Errorf("HEAD Content-Encoding=%q, want br", ce)
	}
	assertVaryAcceptEncoding(t, head)

	get := servePrecomp(t, http.MethodGet, "/assets/app.js", "br, gzip", WithPrecompressed())
	if h, g := head.Header().Get("Content-Type"), get.Header().Get("Content-Type"); h != g {
		t.Errorf("HEAD Content-Type=%q, want it to match GET's %q", h, g)
	}
}

// --- File 单文件入口 -----------------------------------------------------------
// --- The File single-route entry ----------------------------------------------

// TestStaticPrecompressed_FileEntry 验证 File 入口(签名 File(m, urlPath, name, fsys,
// opts...))同样支持预压缩,且 Content-Type 仍取原始名。
func TestStaticPrecompressed_FileEntry(t *testing.T) {
	m := New()
	if err := File(m, "/app.js", "app.js", precompFS(), WithPrecompressed()); err != nil {
		t.Fatalf("File: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	r.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != precompBrotliJS {
		t.Errorf("body=%q, want the br variant %q", got, precompBrotliJS)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "br" {
		t.Errorf("Content-Encoding=%q, want br", ce)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type=%q, want it to contain javascript", ct)
	}
	assertVaryAcceptEncoding(t, rec)
}

// TestStaticPrecompressed_FileEntryWithoutOption 验证 File 不传选项时不做变体替换
// (与 StaticFS 一致的零成本保证)。
func TestStaticPrecompressed_FileEntryWithoutOption(t *testing.T) {
	m := New()
	if err := File(m, "/app.js", "app.js", precompFS()); err != nil {
		t.Fatalf("File: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	r.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, r)

	if got := rec.Body.String(); got != precompPlainJS {
		t.Errorf("body=%q, want the original %q", got, precompPlainJS)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Errorf("Content-Encoding=%q, want none", ce)
	}
}

// --- 真实 gzip 数据的端到端解码 ------------------------------------------------
// --- End-to-end decode of real gzip data --------------------------------------

// TestStaticPrecompressed_RealGzipVariantDecodes 用【真实】 gzip 字节做一次闭环:标准
// gzip.Reader 必须能解出原文。前面各例用字面量断言"选了哪个文件",这一例补上"选出来的
// 东西确实是客户端能按声明的 Content-Encoding 解开的"。
func TestStaticPrecompressed_RealGzipVariantDecodes(t *testing.T) {
	const original = "export const answer = 42;\n"
	var buf strings.Builder
	zw := gzip.NewWriter(&stringWriterAdapter{&buf})
	if _, err := zw.Write([]byte(original)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	fsys := fstest.MapFS{
		"lib.js":    {Data: []byte(original)},
		"lib.js.gz": {Data: []byte(buf.String())},
	}

	m := New()
	if err := StaticFS(m, "/assets/", fsys, WithPrecompressed()); err != nil {
		t.Fatalf("StaticFS: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/assets/lib.js", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("Content-Encoding=%q, want gzip", ce)
	}
	zr, err := gzip.NewReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("gzip.NewReader on the served body: %v", err)
	}
	defer func() { _ = zr.Close() }()
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if string(got) != original {
		t.Errorf("decoded body=%q, want %q", got, original)
	}
}

// stringWriterAdapter 把 strings.Builder 适配成 io.Writer(Builder 已满足,包一层只为
// 让 gzip.NewWriter 的目标类型显式)。
// stringWriterAdapter adapts strings.Builder to io.Writer.
type stringWriterAdapter struct{ b *strings.Builder }

func (w *stringWriterAdapter) Write(p []byte) (int, error) { return w.b.Write(p) }

// --- pickPrecompressed 直测 ----------------------------------------------------
// --- pickPrecompressed unit tests ---------------------------------------------

// TestPickPrecompressed 直测变体挑选:未开启时立即返回(不做任何 Stat),后缀守卫、
// Accept-Encoding 缺失、变体不存在等分支逐一覆盖。
func TestPickPrecompressed(t *testing.T) {
	fsys := precompFS()
	cases := []struct {
		name           string
		name0          string // 原始文件名(fs 相对)
		acceptEncoding string
		cfg            staticConfig
		wantOK         bool
		wantEncoding   string
		wantName       string
	}{
		{
			name:           "disabled returns immediately",
			name0:          "app.js",
			acceptEncoding: "br, gzip",
			cfg:            staticConfig{},
		},
		{
			name:           "br wins by configured priority",
			name0:          "app.js",
			acceptEncoding: "br, gzip",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli, variantGzip}},
			wantOK:         true,
			wantEncoding:   "br",
			wantName:       "app.js.br",
		},
		{
			name:           "gzip wins when listed first",
			name0:          "app.js",
			acceptEncoding: "br, gzip",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantGzip, variantBrotli}},
			wantOK:         true,
			wantEncoding:   "gzip",
			wantName:       "app.js.gz",
		},
		{
			name:           "skips an unacceptable encoding and takes the next",
			name0:          "app.js",
			acceptEncoding: "gzip",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli, variantGzip}},
			wantOK:         true,
			wantEncoding:   "gzip",
			wantName:       "app.js.gz",
		},
		{
			name:           "skips a missing variant file and takes the next",
			name0:          "only-gz.js",
			acceptEncoding: "br, gzip",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli, variantGzip}},
			wantOK:         true,
			wantEncoding:   "gzip",
			wantName:       "only-gz.js.gz",
		},
		{
			name:           "no variant exists at all",
			name0:          "plain.js",
			acceptEncoding: "br, gzip",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli, variantGzip}},
		},
		{
			name:           "empty Accept-Encoding never substitutes",
			name0:          "app.js",
			acceptEncoding: "",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli, variantGzip}},
		},
		{
			name:           "suffix guard: a .br name is not re-encoded",
			name0:          "app.js.br",
			acceptEncoding: "br, gzip",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli, variantGzip}},
		},
		{
			name:           "suffix guard: a .gz name is not re-encoded",
			name0:          "app.js.gz",
			acceptEncoding: "br, gzip",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli, variantGzip}},
		},
		{
			name:           "suffix guard only covers enabled variants",
			name0:          "app.js.br",
			acceptEncoding: "gzip",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantGzip}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			if c.acceptEncoding != "" {
				r.Header.Set("Accept-Encoding", c.acceptEncoding)
			}
			v, vname, ok := pickPrecompressed(&Request{Request: r}, fsys, c.name0, c.cfg)
			if ok != c.wantOK {
				t.Fatalf("ok=%v, want %v (variant=%q name=%q)", ok, c.wantOK, v.encoding, vname)
			}
			if !c.wantOK {
				return
			}
			if v.encoding != c.wantEncoding {
				t.Errorf("encoding=%q, want %q", v.encoding, c.wantEncoding)
			}
			if vname != c.wantName {
				t.Errorf("variant name=%q, want %q", vname, c.wantName)
			}
		})
	}
}

// TestPickPrecompressed_IgnoresDirectoryVariant 验证同名目录不会被当成变体文件。
// 构造 app.js.gz/ 为目录:若实现只判 Stat 成功,就会把一个目录交给 ServeFileFS。
func TestPickPrecompressed_IgnoresDirectoryVariant(t *testing.T) {
	fsys := fstest.MapFS{
		"app.js":                {Data: []byte("PLAIN")},
		"app.js.gz/placeholder": {Data: []byte("x")}, // 使 app.js.gz 成为一个目录
	}
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	cfg := staticConfig{precompressed: []precompressedVariant{variantGzip}}
	if _, vname, ok := pickPrecompressed(&Request{Request: r}, fsys, "app.js", cfg); ok {
		t.Errorf("picked %q, want no variant (a directory must never be served as one)", vname)
	}
}

// --- serveStaticFile 直测 ------------------------------------------------------
// --- serveStaticFile unit tests -----------------------------------------------

// TestServeStaticFile 直测 serveStaticFile 的三条路径:命中变体、开启但未命中(仍发
// Vary)、完全关闭(不发 Vary)。绕过路由以确认头是由本函数而非路由层写入的。
func TestServeStaticFile(t *testing.T) {
	fsys := precompFS()
	cases := []struct {
		name           string
		file           string
		acceptEncoding string
		cfg            staticConfig
		wantBody       string
		wantEncoding   string
		wantVary       bool
	}{
		{
			name:           "hit writes encoding and Vary",
			file:           "app.js",
			acceptEncoding: "br",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli}},
			wantBody:       precompBrotliJS,
			wantEncoding:   "br",
			wantVary:       true,
		},
		{
			name:           "enabled but missed still writes Vary",
			file:           "plain.js",
			acceptEncoding: "br",
			cfg:            staticConfig{precompressed: []precompressedVariant{variantBrotli}},
			wantBody:       precompNoVar,
			wantEncoding:   "",
			wantVary:       true,
		},
		{
			name:           "disabled writes neither",
			file:           "app.js",
			acceptEncoding: "br",
			cfg:            staticConfig{},
			wantBody:       precompPlainJS,
			wantEncoding:   "",
			wantVary:       false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/"+c.file, nil)
			r.Header.Set("Accept-Encoding", c.acceptEncoding)
			rec := httptest.NewRecorder()
			serveStaticFile(&Response{ResponseWriter: rec}, &Request{Request: r}, fsys, c.file, c.cfg)

			if got := rec.Body.String(); got != c.wantBody {
				t.Errorf("body=%q, want %q", got, c.wantBody)
			}
			if got := rec.Header().Get("Content-Encoding"); got != c.wantEncoding {
				t.Errorf("Content-Encoding=%q, want %q", got, c.wantEncoding)
			}
			gotVary := len(rec.Header().Values("Vary")) > 0
			if gotVary != c.wantVary {
				t.Errorf("has Vary=%v, want %v (Vary=%v)", gotVary, c.wantVary, rec.Header().Values("Vary"))
			}
		})
	}
}

// --- acceptsEncoding / isZeroQuality 直测 --------------------------------------
// --- acceptsEncoding / isZeroQuality unit tests -------------------------------

// TestAcceptsEncoding 表驱动锁定 Accept-Encoding 解析。最易错的两点:q=0 是显式【拒绝】
// (不能读成接受),以及仅【包含】编码名的 token(如 "xbr")不构成匹配。
func TestAcceptsEncoding(t *testing.T) {
	cases := []struct {
		name   string
		header string
		enc    string
		want   bool
	}{
		{"exact gzip", "gzip", "gzip", true},
		{"list matches gzip", "br, gzip", "gzip", true},
		{"list matches br", "br, gzip", "br", true},
		{"list does not contain deflate", "br, gzip", "deflate", false},
		{"q=0 is an explicit refusal", "gzip;q=0", "gzip", false},
		{"q=0 with spaces is a refusal", "gzip; q=0", "gzip", false},
		{"q=0.0 is a refusal", "gzip;q=0.0", "gzip", false},
		{"positive q still accepts", "gzip;q=0.5", "gzip", true},
		{"q=1 accepts", "gzip;q=1", "gzip", true},
		{"refusing gzip does not refuse br", "gzip;q=0, br", "br", true},
		{"wildcard matches anything", "*", "br", true},
		{"wildcard matches gzip too", "*", "gzip", true},
		{"wildcard with q=0 matches nothing", "*;q=0", "gzip", false},
		{"empty header matches nothing", "", "gzip", false},
		{"identity only does not match gzip", "identity", "gzip", false},
		{"case-insensitive uppercase", "GZIP", "gzip", true},
		{"case-insensitive mixed", "Br", "br", true},
		{"whitespace tolerance", " br , gzip ", "br", true},
		{"whitespace tolerance for the second token", " br , gzip ", "gzip", true},
		{"a token merely containing the name does not match", "xbr", "br", false},
		{"a token with the name as prefix does not match", "brx", "br", false},
		{"gzipx does not match gzip", "gzipx", "gzip", false},
		{"first matching token decides", "gzip;q=0, gzip", "gzip", false},
		{"earlier wildcard refusal wins over a later listing", "*;q=0, gzip", "gzip", false},
		{"non-q parameter does not refuse", "gzip;level=9", "gzip", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := acceptsEncoding(c.header, c.enc); got != c.want {
				t.Errorf("acceptsEncoding(%q, %q)=%v, want %v", c.header, c.enc, got, c.want)
			}
		})
	}
}

// TestIsZeroQuality 表驱动锁定 q=0 判定。把 q=0.5 误判为零会让本可压缩的响应退化为原文。
func TestIsZeroQuality(t *testing.T) {
	cases := []struct {
		params string
		want   bool
	}{
		{"q=0", true},
		{"q=0.", true},
		{"q=0.0", true},
		{"q=0.00", true},
		{"q=0.000", true},
		{" q=0 ", true},
		{"Q=0", true}, // 参数名大小写不敏感
		{"level=9;q=0", true},
		{"q=0.5", false},
		{"q=0.001", false},
		{"q=1", false},
		{"q=1.0", false},
		{"", false},
		{"level=9", false},
		{"charset=utf-8", false},
		{"q", false},    // 无 "=" 不构成参数
		{"q=", false},   // 空值不是 0
		{"qq=0", false}, // 参数名必须恰为 q
	}
	for _, c := range cases {
		t.Run(c.params, func(t *testing.T) {
			if got := isZeroQuality(c.params); got != c.want {
				t.Errorf("isZeroQuality(%q)=%v, want %v", c.params, got, c.want)
			}
		})
	}
}

// --- 与既有静态语义的兼容 ------------------------------------------------------
// --- Compatibility with existing static semantics -----------------------------

// TestStaticPrecompressed_MissStillNotFound 验证预压缩不改变未命中语义:不存在的文件
// 仍是 404,不会因为"试着找 .br"而变成别的状态。
func TestStaticPrecompressed_MissStillNotFound(t *testing.T) {
	rec := servePrecomp(t, http.MethodGet, "/assets/nope.js", "br, gzip", WithPrecompressed())
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

// TestStaticPrecompressed_VariantOnlyIsNotReachableAsOriginal 验证只有变体、没有原始文件
// 时不伪造原始资源:请求 /only-br.js 仍是 404。预压缩只做【替换】而非【创造】资源,否则
// 会绕过"文件是否存在"这一层判断。
func TestStaticPrecompressed_VariantOnlyIsNotReachableAsOriginal(t *testing.T) {
	fsys := fstest.MapFS{"only-br.js.br": {Data: []byte("BR-ONLY")}}
	m := New()
	if err := StaticFS(m, "/assets/", fsys, WithPrecompressed()); err != nil {
		t.Fatalf("StaticFS: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/assets/only-br.js", nil)
	r.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404 (a variant alone must not synthesize the original)", rec.Code)
	}
}
