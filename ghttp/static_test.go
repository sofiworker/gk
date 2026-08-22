package ghttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// mapFS 构造一个内存文件系统供静态服务测试，避免依赖真实磁盘。
// mapFS builds an in-memory file system for static-serving tests.
func mapFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("<h1>home</h1>")},
		"css/app.css":     {Data: []byte("body{}")},
		"assets/logo.png": {Data: []byte("\x89PNG-fake")},
		"sub/index.html":  {Data: []byte("<h1>sub</h1>")},
	}
}

// TestStaticFS_ServesFile 验证普通文件（非 index.html）可正常服务。
func TestStaticFS_ServesFile(t *testing.T) {
	m := New()
	if err := StaticFS(m, "/static/", mapFS()); err != nil {
		t.Fatalf("StaticFS: %v", err)
	}
	cases := []struct {
		path, wantBody string
	}{
		{"/static/css/app.css", "body{}"},
		{"/static/assets/logo.png", "\x89PNG-fake"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status=%d, want 200", c.path, rec.Code)
			continue
		}
		if got := rec.Body.String(); got != c.wantBody {
			t.Errorf("%s: body=%q, want %q", c.path, got, c.wantBody)
		}
	}
}

// TestStaticFS_DirectoryIndex 验证目录请求自动服务其 index.html（含子目录）。
func TestStaticFS_DirectoryIndex(t *testing.T) {
	m := New()
	if err := StaticFS(m, "/static/", mapFS()); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path, wantBody string
	}{
		{"/static/", "<h1>home</h1>"},    // 根目录 → index.html
		{"/static/sub/", "<h1>sub</h1>"}, // 子目录 → sub/index.html
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status=%d, want 200", c.path, rec.Code)
			continue
		}
		if got := rec.Body.String(); got != c.wantBody {
			t.Errorf("%s: body=%q, want %q", c.path, got, c.wantBody)
		}
	}
}

// TestStaticFS_NoDirectoryListing 验证无索引的目录默认返回 404（不列目录）。
func TestStaticFS_NoDirectoryListing(t *testing.T) {
	fsys := fstest.MapFS{
		"data/a.txt": {Data: []byte("A")},
		"data/b.txt": {Data: []byte("B")},
	}
	m := New()
	if err := StaticFS(m, "/static/", fsys); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/data/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("dir without index: status=%d, want 404 (no listing)", rec.Code)
	}
}

// TestStaticFS_Browsable 验证开启 WithBrowsable 后无索引目录会列出条目。
func TestStaticFS_Browsable(t *testing.T) {
	fsys := fstest.MapFS{"data/a.txt": {Data: []byte("A")}}
	m := New()
	if err := StaticFS(m, "/static/", fsys, WithBrowsable()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/data/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("browsable dir: status=%d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "a.txt") {
		t.Errorf("browsable listing should mention a.txt, got %q", rec.Body.String())
	}
}

// TestStaticFS_SPAFallback 验证 SPA 回退：未命中路径服务根 index.html。
func TestStaticFS_SPAFallback(t *testing.T) {
	m := New()
	if err := StaticFS(m, "/app/", mapFS(), WithSPAFallback()); err != nil {
		t.Fatal(err)
	}
	// /app/some/client/route 不存在 → 回退到根 index.html。
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/some/client/route", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("SPA fallback: status=%d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "<h1>home</h1>" {
		t.Errorf("SPA fallback body=%q, want root index", got)
	}
}

// TestStaticFS_CustomIndex 验证 WithIndexFile 自定义索引名。
func TestStaticFS_CustomIndex(t *testing.T) {
	fsys := fstest.MapFS{"main.htm": {Data: []byte("CUSTOM")}}
	m := New()
	if err := StaticFS(m, "/static/", fsys, WithIndexFile("main.htm")); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "CUSTOM" {
		t.Errorf("custom index: status=%d body=%q, want 200/CUSTOM", rec.Code, rec.Body.String())
	}
}

// TestStaticFS_NotFound 验证不存在的文件返回 404。
func TestStaticFS_NotFound(t *testing.T) {
	m := New()
	if err := StaticFS(m, "/static/", mapFS()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rec.Code)
	}
}

// TestStaticFS_HeadMethod 验证 HEAD 请求返回头但无 body。
func TestStaticFS_HeadMethod(t *testing.T) {
	m := New()
	if err := StaticFS(m, "/static/", mapFS()); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/static/css/app.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body len=%d, want 0", rec.Body.Len())
	}
}

// TestStaticFS_InvalidPrefix 验证 prefix 校验（须首尾均为 "/"）。
func TestStaticFS_InvalidPrefix(t *testing.T) {
	m := New()
	for _, bad := range []string{"", "static/", "/static", "static"} {
		if err := StaticFS(m, bad, mapFS()); err == nil {
			t.Errorf("prefix %q should be rejected", bad)
		}
	}
}

// TestStaticFS_NilFS 验证 nil fsys 被拒绝。
func TestStaticFS_NilFS(t *testing.T) {
	m := New()
	if err := StaticFS(m, "/static/", nil); err == nil {
		t.Error("nil fsys should be rejected")
	}
}

// TestFile_ServesSingle 验证单文件映射。
func TestFile_ServesSingle(t *testing.T) {
	m := New()
	if err := File(m, "/favicon.ico", "assets/logo.png", mapFS()); err != nil {
		t.Fatalf("File: %v", err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "\x89PNG-fake" {
		t.Errorf("body=%q, want fake PNG", got)
	}
}

// TestFile_InvalidName 验证非法文件名被拒绝。
func TestFile_InvalidName(t *testing.T) {
	m := New()
	for _, bad := range []string{"", "../etc/passwd", "/abs/path"} {
		if err := File(m, "/x", bad, mapFS()); err == nil {
			t.Errorf("file name %q should be rejected", bad)
		}
	}
}

// TestStatic_DiskDir 验证 Static 挂载真实磁盘目录（用 t.TempDir）。
func TestStatic_DiskDir(t *testing.T) {
	dir := t.TempDir()
	if err := writeTempFile(dir, "hello.txt", "world"); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := Static(m, "/files/", dir); err != nil {
		t.Fatalf("Static: %v", err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/files/hello.txt", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "world" {
		t.Errorf("body=%q, want %q", got, "world")
	}
}

// TestJoinFSPath 单测路径拼接辅助。
func TestJoinFSPath(t *testing.T) {
	cases := []struct{ dir, elem, want string }{
		{".", "index.html", "index.html"},
		{"", "index.html", "index.html"},
		{"sub", "index.html", "sub/index.html"},
		{"a/b", "c.txt", "a/b/c.txt"},
	}
	for _, c := range cases {
		if got := joinFSPath(c.dir, c.elem); got != c.want {
			t.Errorf("joinFSPath(%q,%q)=%q, want %q", c.dir, c.elem, got, c.want)
		}
	}
}

// writeTempFile 在 dir 下写一个测试文件。
// writeTempFile writes a test file under dir.
func writeTempFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}
