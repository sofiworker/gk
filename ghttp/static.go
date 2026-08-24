package ghttp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
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
}

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
			http.ServeFileFS(resp.ResponseWriter, req.Request, fsys, idx)
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
	http.ServeFileFS(resp.ResponseWriter, req.Request, fsys, name)
	return nil
}

// staticMiss 处理未命中：SPA 模式回退到根索引，否则 404。
// staticMiss handles a miss: SPA mode falls back to the root index; otherwise 404.
func staticMiss(resp *Response, req *Request, fsys fs.FS, cfg staticConfig) error {
	if cfg.spa {
		if _, err := fs.Stat(fsys, cfg.indexFile); err == nil {
			http.ServeFileFS(resp.ResponseWriter, req.Request, fsys, cfg.indexFile)
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
// fsys 为 nil 时用当前工作目录。为 GET/HEAD 注册精确路径。
// File maps a single request path to the file name within fsys. A nil fsys uses
// the working directory. Registers the exact path for GET/HEAD.
func File(m *Server, path, name string, fsys fs.FS) error {
	if fsys == nil {
		fsys = os.DirFS(".")
	}
	if name == "" || !fs.ValidPath(name) {
		return fmt.Errorf("%w: static file name %q invalid", ErrInvalidParam, name)
	}
	handler := func(ctx context.Context, req *Request, resp *Response) error {
		http.ServeFileFS(resp.ResponseWriter, req.Request, fsys, name)
		return nil
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if err := m.RawHandle(method, path, handler); err != nil {
			return fmt.Errorf("ghttp: register file %s %s: %w", method, path, err)
		}
	}
	return nil
}
