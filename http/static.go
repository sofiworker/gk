package ghttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// StaticParam 是 Static 路由使用的通配参数名。
// StaticParam is the catch-all parameter name used by Static routes.
const StaticParam = "filepath"

// staticIndexName 是目录默认首页文件名。
// staticIndexName is the default index file name of a directory.
const staticIndexName = "index.html"

// StaticOption 配置 Static（WithFunc 风格）。
// StaticOption configures Static (WithFunc style).
type StaticOption func(*staticConfig)

type staticConfig struct {
	index        bool
	cacheControl string
	precompress  bool
}

// WithStaticIndex 设置目录是否回退到其 index.html；默认 true。关闭后访问目录一律 404。
// WithStaticIndex sets whether a directory falls back to its index.html (default true).
// When disabled, directories always answer 404.
func WithStaticIndex(enabled bool) StaticOption {
	return func(c *staticConfig) { c.index = enabled }
}

// WithStaticCacheControl 为成功响应设置 Cache-Control 头；空串表示不设置。
// WithStaticCacheControl sets Cache-Control on successful responses; empty means unset.
func WithStaticCacheControl(v string) StaticOption {
	return func(c *staticConfig) { c.cacheControl = v }
}

// WithStaticPrecompressed 启用预压缩文件：存在 name.br / name.gz 且客户端 Accept-Encoding
// 接受时直接返回它们，并设置 Content-Encoding、原文件的 Content-Type 与 Vary。
// WithStaticPrecompressed enables precompressed siblings: when name.br / name.gz exists and
// the client accepts it, that file is served with Content-Encoding, the original
// Content-Type and Vary.
func WithStaticPrecompressed() StaticOption {
	return func(c *staticConfig) { c.precompress = true }
}

// Static 返回为 fsys 提供静态文件的 GET/HEAD 路由，挂载在 prefix/*filepath。
//
// 默认禁止目录列表（目录无 index.html 时 404），含 ".." 或反斜杠的路径一律 404，支持
// Range 与条件请求（If-Modified-Since 等）。不存在或被禁止的资源返回 HTTPError 404，
// 交给错误链处理。prefix 为 "/" 或 "" 时路由为 "/*filepath"，会与同一方法下的其他根级
// 路由冲突（取决于路由树对通配符的约束），此时请使用独立前缀，例如 "/static"。
// Static returns GET/HEAD routes serving fsys under prefix/*filepath.
//
// Directory listings are disabled by default (404 without index.html); paths containing
// ".." or backslashes yield 404; Range and conditional requests are supported. Missing or
// forbidden resources return HTTPError 404 for the error chain. With prefix "/" or "" the
// route is "/*filepath", which conflicts with other root-level routes of the same method
// (subject to the router's catch-all rules); prefer a dedicated prefix such as "/static".
func Static(prefix string, fsys fs.FS, opts ...StaticOption) []Route {
	cfg := staticConfig{index: true}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	p := strings.TrimRight(prefix, "/") + "/*" + StaticParam
	h := func(_ context.Context, req *Request, resp *Response) error {
		return staticServe(fsys, cfg, req, resp)
	}
	return []Route{
		Raw(http.MethodGet, p, h),
		Raw(http.MethodHead, p, h),
	}
}

// staticName 把通配参数规整为 fs.FS 路径；非法返回 false。
// staticName normalizes the catch-all value into an fs.FS path; false when invalid.
func staticName(raw string) (string, bool) {
	if strings.ContainsRune(raw, 0x5c) || strings.ContainsRune(raw, 0) {
		return "", false
	}
	raw = strings.TrimPrefix(raw, "/")
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." || seg == "." {
			return "", false
		}
	}
	name := strings.TrimSuffix(raw, "/")
	if name == "" {
		name = "."
	}
	if !fs.ValidPath(name) {
		return "", false
	}
	return name, true
}

func staticNotFound() error { return NotFound("not found") }

// staticOpen 打开 name；目录按配置回退 index.html。返回文件、信息与最终名称。
// staticOpen opens name; directories fall back to index.html per config.
func staticOpen(fsys fs.FS, cfg staticConfig, name string) (fs.File, fs.FileInfo, string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, nil, "", staticMapErr(err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, "", staticMapErr(err)
	}
	if !st.IsDir() {
		return f, st, name, nil
	}
	_ = f.Close()
	if !cfg.index {
		return nil, nil, "", staticNotFound()
	}
	idx := path.Join(name, staticIndexName)
	f, err = fsys.Open(idx)
	if err != nil {
		return nil, nil, "", staticMapErr(err)
	}
	st, err = f.Stat()
	if err != nil || st.IsDir() {
		_ = f.Close()
		if err != nil {
			return nil, nil, "", staticMapErr(err)
		}
		return nil, nil, "", staticNotFound()
	}
	return f, st, idx, nil
}

func staticMapErr(err error) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrInvalid) {
		return staticNotFound()
	}
	return err
}

// staticAccepts 报告 Accept-Encoding 是否接受 coding（q=0 视为拒绝）。
// staticAccepts reports whether Accept-Encoding accepts coding (q=0 rejects).
func staticAccepts(header, coding string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), coding) {
			continue
		}
		for _, p := range fields[1:] {
			p = strings.TrimSpace(p)
			if v, ok := strings.CutPrefix(p, "q="); ok {
				if q, err := strconv.ParseFloat(v, 64); err == nil && q <= 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

func staticServe(fsys fs.FS, cfg staticConfig, req *Request, resp *Response) error {
	name, ok := staticName(req.Params.Get(StaticParam))
	if !ok {
		return staticNotFound()
	}
	f, st, final, err := staticOpen(fsys, cfg, name)
	if err != nil {
		return err
	}
	ctype := mime.TypeByExtension(path.Ext(final))
	h := resp.Header()
	if cfg.precompress {
		h.Add("Vary", "Accept-Encoding")
		ae := req.Raw.Header.Get("Accept-Encoding")
		for _, c := range []struct{ coding, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
			if !staticAccepts(ae, c.coding) {
				continue
			}
			cf, err := fsys.Open(final + c.ext)
			if err != nil {
				continue
			}
			cst, err := cf.Stat()
			if err != nil || cst.IsDir() {
				_ = cf.Close()
				continue
			}
			_ = f.Close()
			f, st = cf, cst
			h.Set("Content-Encoding", c.coding)
			if ctype == "" {
				ctype = "application/octet-stream"
			}
			break
		}
	}
	defer f.Close()
	if ctype != "" {
		h.Set("Content-Type", ctype)
	}
	if cfg.cacheControl != "" {
		h.Set("Cache-Control", cfg.cacheControl)
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		rs = bytes.NewReader(data)
	}
	mod := st.ModTime()
	http.ServeContent(resp, req.Raw, final, mod, rs)
	return nil
}
