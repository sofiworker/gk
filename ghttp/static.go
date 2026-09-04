package ghttp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

// ===========================================================================
// 静态文件服务：基于 io/fs.FS 抽象，同时支持磁盘目录（os.DirFS）与嵌入资源
// （embed.FS）。内部用 catch-all 路由 + http.ServeFileFS 实现。
//
// 关键可用性行为：
//   - 目录请求（如 /assets/）尝试服务其 index 文件（默认 index.html）；
//   - 找不到 index 时默认返回 404（不暴露目录列表，安全默认），可开启 Browsable；
//   - SPA 模式下任意未命中路径回退到根 index，支持前端路由（history 模式）。
//
// Static file serving over io/fs.FS, supporting disk dirs (os.DirFS) and
// embedded assets (embed.FS), via a catch-all route plus http.ServeFileFS.
// Usability: a directory request serves its index file (default index.html);
// missing index yields 404 by default (no directory listing — safe default,
// opt in via Browsable); SPA mode falls back any unmatched path to the root
// index for client-side routing.
// ===========================================================================

// staticParam 是静态服务 catch-all 路由使用的参数名，集中一处避免拼写漂移。
// staticParam is the catch-all parameter name used by static serving.
const staticParam = "filepath"

// staticConfig 是静态服务的可选配置，经 StaticOption 累积。
// staticConfig is the optional static-serving configuration accumulated via StaticOption.
type staticConfig struct {
	indexFile string // 目录请求服务的索引文件名 / index file for directory requests
	browsable bool   // 无索引时是否列目录 / list directory when index is absent
	spa       bool   // 未命中回退根索引 / fall back to root index on miss (SPA)
	// precompressed 是启用的预压缩变体,按优先级排列(先匹配者优先)。为空表示不查
	// 预压缩文件——此时静态服务与本能力不存在时的行为完全一致(零额外 Stat)。
	// precompressed lists the enabled pre-compressed variants in priority order
	// (first match wins). Empty means no variant lookup happens, so static serving
	// behaves exactly as if the capability did not exist (zero extra Stat calls).
	precompressed []precompressedVariant
}

// precompressedVariant 描述一种预压缩变体:内容编码名与文件名后缀。
// precompressedVariant describes one pre-compressed variant: its content coding
// name and file suffix.
type precompressedVariant struct {
	encoding string // Content-Encoding 值,如 "br" / the Content-Encoding value
	suffix   string // 文件后缀,如 ".br" / the file suffix
}

// 预压缩变体表。br 优先于 gzip:同等资源下 brotli 体积更小,且现代浏览器普遍支持。
// The pre-compressed variant table. br outranks gzip: brotli is smaller for the
// same asset and is broadly supported by modern browsers.
var (
	variantBrotli = precompressedVariant{encoding: "br", suffix: ".br"}
	variantGzip   = precompressedVariant{encoding: "gzip", suffix: ".gz"}
)

// StaticOption 以函数式选项配置静态服务。
// StaticOption configures static serving via functional options.
type StaticOption func(*staticConfig)

// WithIndexFile 设置目录请求服务的索引文件名（默认 "index.html"）。
// WithIndexFile sets the index file served for directory requests (default "index.html").
func WithIndexFile(name string) StaticOption {
	return func(c *staticConfig) { c.indexFile = name }
}

// WithBrowsable 开启目录列表：当目录无索引文件时列出条目。默认关闭（安全）。
// WithBrowsable enables directory listing when a directory has no index file.
// Off by default (safer).
func WithBrowsable() StaticOption {
	return func(c *staticConfig) { c.browsable = true }
}

// WithSPAFallback 开启 SPA 回退：任意未命中的路径改服务根索引文件，支持
// 前端 history 路由。与 Browsable 互斥（SPA 优先）。
// WithSPAFallback enables SPA fallback: any unmatched path serves the root index
// file, supporting client-side history routing. Mutually exclusive with
// Browsable (SPA wins).
func WithSPAFallback() StaticOption {
	return func(c *staticConfig) { c.spa = true }
}

// WithPrecompressed 开启预压缩变体服务:当客户端 Accept-Encoding 接受 br 或 gzip 且
// 磁盘上存在 <file>.br / <file>.gz 时,直接服务该变体并置 Content-Encoding,把压缩成本
// 从请求期挪到构建期。
//
// 与 Gzip 中间件的分工:Gzip 在【请求期】动态压缩任意响应(含 API JSON),本能力只服务
// 【构建期】已压好的静态资源。二者可同时挂载——预压缩命中时会置 Content-Encoding,
// Gzip 中间件据此跳过二次压缩。
//
// 默认关闭:未调用本选项时不产生任何额外 fs.Stat,静态服务的行为与开销完全不变。
//
// WithPrecompressed enables pre-compressed variant serving: when the client's
// Accept-Encoding admits br or gzip and a <file>.br / <file>.gz exists on disk, that
// variant is served with Content-Encoding set, moving compression cost from request
// time to build time.
//
// Division of labor with the Gzip middleware: Gzip compresses arbitrary responses
// (including API JSON) at REQUEST time, while this only serves assets compressed at
// BUILD time. Both may be mounted together — a pre-compressed hit sets
// Content-Encoding, on which the Gzip middleware skips recompression.
//
// Off by default: without this option no extra fs.Stat occurs and static serving's
// behavior and cost are unchanged.
func WithPrecompressed() StaticOption {
	return func(c *staticConfig) {
		c.precompressed = []precompressedVariant{variantBrotli, variantGzip}
	}
}

// WithPrecompressedEncodings 精确指定启用的预压缩编码及优先级(取值 "br" / "gzip")。
// 用于只构建了 gzip 变体、或希望 gzip 优先的部署。未识别的编码名被忽略;全部无效时
// 等价于不开启预压缩。
// WithPrecompressedEncodings selects exactly which pre-compressed encodings are
// enabled and in what priority ("br" / "gzip"). Use it where only gzip variants are
// built, or where gzip should win. Unrecognized names are ignored; if none is valid
// the result equals leaving pre-compression off.
func WithPrecompressedEncodings(encodings ...string) StaticOption {
	return func(c *staticConfig) {
		out := make([]precompressedVariant, 0, len(encodings))
		for _, e := range encodings {
			switch strings.ToLower(strings.TrimSpace(e)) {
			case "br", "brotli":
				out = append(out, variantBrotli)
			case "gzip", "gz":
				out = append(out, variantGzip)
			}
		}
		c.precompressed = out
	}
}

// Static 把磁盘目录 dir 挂载到 prefix 下提供文件服务。prefix 须首尾均为 "/"
// （如 "/assets/"）；dir 为空时用当前工作目录。为 GET/HEAD 注册 catch-all 路由。
// Static mounts the on-disk directory dir under prefix. prefix must start and end
// with "/" (e.g. "/assets/"); an empty dir uses the working directory. Registers
// catch-all routes for GET/HEAD.
func Static(m *Server, prefix, dir string, opts ...StaticOption) error {
	if dir == "" {
		dir = "."
	}
	return StaticFS(m, prefix, os.DirFS(dir), opts...)
}

// StaticFS 把任意 fs.FS 挂载到 prefix 下，支持 embed.FS 等只读文件系统。
// StaticFS mounts an arbitrary fs.FS under prefix, supporting read-only file
// systems such as embed.FS.
func StaticFS(m *Server, prefix string, fsys fs.FS, opts ...StaticOption) error {
	if !strings.HasPrefix(prefix, "/") || !strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("%w: static prefix %q must start and end with '/'", ErrInvalidParam, prefix)
	}
	if fsys == nil {
		return fmt.Errorf("%w: static fsys must not be nil", ErrInvalidParam)
	}
	cfg := staticConfig{indexFile: "index.html"}
	for _, opt := range opts {
		opt(&cfg)
	}

	handler := func(ctx context.Context, req *Request, resp *Response) error {
		return serveStatic(resp, req, fsys, cfg)
	}
	pattern := prefix + "{" + staticParam + "...}"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if err := m.RawHandle(method, pattern, handler); err != nil {
			return fmt.Errorf("ghttp: register static %s %s: %w", method, pattern, err)
		}
	}
	return nil
}

// serveStatic 是静态请求的核心逻辑：规范化名字→目录索引/SPA 回退→ServeFileFS。
// serveStatic is the core static-request logic: normalize name → directory index
// / SPA fallback → ServeFileFS.
func serveStatic(resp *Response, req *Request, fsys fs.FS, cfg staticConfig) error {
	// catch-all 值形如 "/sub/app.css" 或目录 "/sub/"（含前导/尾随 "/"）；去掉首尾斜杠
	// 得到 fs.FS 相对名（fs.ValidPath 拒绝带斜杠的名字）。
	// The catch-all value looks like "/sub/app.css" or a directory "/sub/"; trim
	// leading/trailing "/" to get the fs.FS-relative name (fs.ValidPath rejects
	// names with slashes at the ends).
	name := strings.Trim(req.Params.Get(staticParam), "/")
	if name == "" {
		name = "." // 目录根 / directory root
	}
	if name != "." && !fs.ValidPath(name) {
		http.Error(resp, "400 invalid path", http.StatusBadRequest)
		return nil
	}

	info, err := fs.Stat(fsys, name)
	switch {
	case err == nil && info.IsDir():
		// 目录请求：优先服务索引文件；无索引且不可浏览则 404 或 SPA 回退。
		// Directory request: prefer the index file; without it, 404 or SPA fallback
		// (unless Browsable).
		idx := joinFSPath(name, cfg.indexFile)
		if _, ierr := fs.Stat(fsys, idx); ierr == nil {
			serveStaticFile(resp, req, fsys, idx, cfg)
			return nil
		}
		if cfg.browsable {
			http.ServeFileFS(resp.ResponseWriter, req.Request, fsys, name)
			return nil
		}
		return staticMiss(resp, req, fsys, cfg)

	case errors.Is(err, fs.ErrNotExist):
		return staticMiss(resp, req, fsys, cfg)

	case err != nil:
		http.Error(resp, "500 internal error", http.StatusInternalServerError)
		return nil
	}

	// 普通文件：直接服务。
	// Regular file: serve directly.
	serveStaticFile(resp, req, fsys, name, cfg)
	return nil
}

// serveStaticFile 服务一个已确认存在的文件,在开启预压缩且客户端接受时改服务其压缩变体。
// serveStaticFile serves a file already known to exist, substituting a compressed
// variant when pre-compression is enabled and the client accepts it.
func serveStaticFile(resp *Response, req *Request, fsys fs.FS, name string, cfg staticConfig) {
	if v, vname, ok := pickPrecompressed(req, fsys, name, cfg); ok {
		h := resp.Header()
		// Content-Type 必须按【原始】文件名判定:变体名以 .br/.gz 结尾,若交给
		// ServeFileFS 推断会得到 application/x-gzip 之类,客户端解压后无法正确解析。
		// The Content-Type must come from the ORIGINAL name: a variant ends in
		// .br/.gz, and letting ServeFileFS infer it would yield something like
		// application/x-gzip, which the client cannot parse after decompressing.
		if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
			h.Set("Content-Type", ct)
		}
		h.Set("Content-Encoding", v.encoding)
		// 同一 URL 会因 Accept-Encoding 产生不同响应体,必须声明 Vary,否则共享缓存
		// 可能把 br 响应喂给不支持 br 的客户端。经 ensureVary 幂等追加:与 Gzip 中间件
		// 同挂时对方可能已声明。
		// One URL yields different bodies per Accept-Encoding, so Vary must be
		// declared or a shared cache could hand a br response to a client without br.
		// Appended idempotently via ensureVary: the Gzip middleware may have already
		// declared it when both are mounted.
		ensureVary(h, "Accept-Encoding")
		http.ServeFileFS(resp.ResponseWriter, req.Request, fsys, vname)
		return
	}
	if len(cfg.precompressed) > 0 {
		// 开启了预压缩但本次未命中变体:响应仍随 Accept-Encoding 而变,同样需要 Vary。
		// Pre-compression is on but no variant matched: the response still varies by
		// Accept-Encoding, so Vary is still required.
		ensureVary(resp.Header(), "Accept-Encoding")
	}
	http.ServeFileFS(resp.ResponseWriter, req.Request, fsys, name)
}

// pickPrecompressed 选出可服务的预压缩变体:按配置优先级找到第一个"客户端接受 + 文件
// 存在"的变体。未开启预压缩时立即返回,不做任何 Stat。
// pickPrecompressed selects a servable pre-compressed variant: the first one in
// configured priority that the client accepts and that exists on disk. With
// pre-compression off it returns immediately, performing no Stat.
func pickPrecompressed(req *Request, fsys fs.FS, name string, cfg staticConfig) (precompressedVariant, string, bool) {
	if len(cfg.precompressed) == 0 {
		return precompressedVariant{}, "", false
	}
	// 变体自身被直接请求(如 /app.js.gz)时不再叠加编码,否则会双重声明 Content-Encoding。
	// A variant requested directly (e.g. /app.js.gz) gets no extra coding, which
	// would otherwise double-declare Content-Encoding.
	for _, v := range cfg.precompressed {
		if strings.HasSuffix(name, v.suffix) {
			return precompressedVariant{}, "", false
		}
	}
	ae := req.Header.Get("Accept-Encoding")
	if ae == "" {
		return precompressedVariant{}, "", false
	}
	for _, v := range cfg.precompressed {
		if !acceptsEncoding(ae, v.encoding) {
			continue
		}
		vname := name + v.suffix
		vi, err := fs.Stat(fsys, vname)
		if err != nil || vi.IsDir() {
			continue
		}
		return v, vname, true
	}
	return precompressedVariant{}, "", false
}

// acceptsEncoding 报告 Accept-Encoding 头是否接受编码 enc。它按 token 切分并识别
// "q=0" 表示的显式拒绝(如 "gzip;q=0"),避免把拒绝误读为接受。
// acceptsEncoding reports whether an Accept-Encoding header admits coding enc. It
// splits on tokens and honors an explicit refusal expressed as "q=0" (e.g.
// "gzip;q=0"), so a refusal is not misread as acceptance.
func acceptsEncoding(header, enc string) bool {
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		token = strings.TrimSpace(token)
		if !strings.EqualFold(token, enc) && token != "*" {
			continue
		}
		if isZeroQuality(params) {
			return false
		}
		return true
	}
	return false
}

// isZeroQuality 报告参数串是否声明 q=0(权重为零即"不接受")。
// isZeroQuality reports whether the parameter string declares q=0 (zero weight
// meaning "not acceptable").
func isZeroQuality(params string) bool {
	for _, p := range strings.Split(params, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "q") {
			continue
		}
		switch strings.TrimSpace(v) {
		case "0", "0.", "0.0", "0.00", "0.000":
			return true
		}
	}
	return false
}

// staticMiss 处理未命中：SPA 模式回退到根索引，否则 404。
// staticMiss handles a miss: SPA mode falls back to the root index; otherwise 404.
func staticMiss(resp *Response, req *Request, fsys fs.FS, cfg staticConfig) error {
	if cfg.spa {
		if _, err := fs.Stat(fsys, cfg.indexFile); err == nil {
			serveStaticFile(resp, req, fsys, cfg.indexFile, cfg)
			return nil
		}
	}
	http.Error(resp, "404 not found", http.StatusNotFound)
	return nil
}

// joinFSPath 拼接 fs.FS 路径（始终用 "/"，与 io/fs 契约一致）。dir 为 "." 时返回 elem。
// joinFSPath joins fs.FS paths (always "/", per the io/fs contract). When dir is
// ".", returns elem alone.
func joinFSPath(dir, elem string) string {
	if dir == "." || dir == "" {
		return elem
	}
	return dir + "/" + elem
}

// File 把单个请求路径映射到 fsys 中的文件 name（如 /favicon.ico → "assets/favicon.ico"）。
// fsys 为 nil 时用当前工作目录。为 GET/HEAD 注册精确路径。opts 支持与 StaticFS 相同的
// 选项,其中 WithPrecompressed 对本入口同样生效(如 /app.js 命中 app.js.br)。
// File maps a single request path to the file name within fsys. A nil fsys uses
// the working directory. Registers the exact path for GET/HEAD. opts accepts the
// same options as StaticFS, and WithPrecompressed applies here too (e.g. /app.js
// serving app.js.br).
func File(m *Server, urlPath, name string, fsys fs.FS, opts ...StaticOption) error {
	if fsys == nil {
		fsys = os.DirFS(".")
	}
	if name == "" || !fs.ValidPath(name) {
		return fmt.Errorf("%w: static file name %q invalid", ErrInvalidParam, name)
	}
	cfg := staticConfig{indexFile: "index.html"}
	for _, opt := range opts {
		opt(&cfg)
	}
	handler := func(ctx context.Context, req *Request, resp *Response) error {
		serveStaticFile(resp, req, fsys, name, cfg)
		return nil
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if err := m.RawHandle(method, urlPath, handler); err != nil {
			return fmt.Errorf("ghttp: register file %s %s: %w", method, urlPath, err)
		}
	}
	return nil
}
