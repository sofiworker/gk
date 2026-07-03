# ghttp VFS Static Root Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a safe static-file root to `ghttp` so `/a.txt` resolves under `WithVFSPath(root)` and traversal attempts cannot escape that root.

**Architecture:** Implement a small `http.FileSystem` in `ghttp/vfs.go` backed by Go 1.25 `os.OpenRoot`. Wire it into `Config`, `WithVFSPath`, and `RouteBuilder.ToStatic(root ...string)` while keeping `ToStatic(root)` source-compatible.

**Tech Stack:** Go 1.25.6, standard library `net/http`, `os.Root`, `io/fs`, `path`, `path/filepath`; no new dependencies.

## Global Constraints

- Always format touched Go files with `gofmt`.
- Do not add external dependencies.
- HTTP request paths use slash-separated URL semantics.
- Do not concatenate native file paths manually for request names.
- Reject traversal, absolute paths, backslash separator tricks, and Windows drive-style names before opening files.
- Preserve existing `ToStatic(root)` and `ToStaticFS(http.FileSystem)` behavior except that `ToStatic(root)` becomes traversal-safe.
- Keep tests alongside code as `*_test.go`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `ghttp/vfs.go` | New safe static file system: `NewSafeFS`, internal `safeFS`, and `cleanVFSName`. |
| `ghttp/vfs_test.go` | Unit tests for safe FS path cleaning, root confinement, encoded traversal inputs after URL decoding, and symlink escape behavior. |
| `ghttp/config.go` | Add `vfsPath string` to `Config` and `WithVFSPath(root string) ServerOption`. |
| `ghttp/builder.go` | Change `ToStatic(root string)` to `ToStatic(root ...string)` and route through `NewSafeFS`. |
| `ghttp/static_test.go` | Add route-level tests for `WithVFSPath(root)` plus `ToStatic()` and existing `ToStatic(root)`. |

---

### Task 1: SafeFS Unit Behavior

**Files:**
- Create: `ghttp/vfs_test.go`
- Create: `ghttp/vfs.go`

**Interfaces:**
- Produces: `func NewSafeFS(root string) (http.FileSystem, error)`
- Produces: `func cleanVFSName(name string) (string, error)`

- [ ] **Step 1: Write failing tests**

Add tests that exercise the desired API before implementation:

```go
func TestSafeFSOpenServesFileUnderRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	fsys, err := NewSafeFS(root)
	if err != nil {
		t.Fatal(err)
	}
	file, err := fsys.Open("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("body = %q, want ok", got)
	}
}

func TestSafeFSRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	fsys, err := NewSafeFS(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"../a.txt", "/../../a.txt", "dir/../../a.txt", "..\\a.txt", "C:/a.txt"} {
		if file, err := fsys.Open(name); err == nil {
			file.Close()
			t.Fatalf("Open(%q) succeeded, want error", name)
		}
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run:

```bash
go test ./ghttp -run "TestSafeFS"
```

Expected: build fails because `NewSafeFS` is undefined.

- [ ] **Step 3: Implement minimal SafeFS**

Create `ghttp/vfs.go` with:

```go
package ghttp

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type safeFS struct {
	root string
}

func NewSafeFS(root string) (http.FileSystem, error) {
	if strings.TrimSpace(root) == "" {
		return nil, ErrStaticRootRequired
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "stat", Path: root, Err: errors.New("not a directory")}
	}
	return &safeFS{root: root}, nil
}

func (fsys *safeFS) Open(name string) (http.File, error) {
	cleaned, err := cleanVFSName(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(fsys.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(cleaned)
}

func cleanVFSName(name string) (string, error) {
	if strings.Contains(name, "\\") {
		return "", fs.ErrNotExist
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fs.ErrNotExist
	}
	cleaned := path.Clean("/" + name)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" {
		cleaned = "."
	}
	if cleaned != "." && !fs.ValidPath(cleaned) {
		return "", fs.ErrNotExist
	}
	if !filepath.IsLocal(cleaned) {
		return "", fs.ErrNotExist
	}
	return cleaned, nil
}
```

- [ ] **Step 4: Verify tests pass**

Run:

```bash
go test ./ghttp -run "TestSafeFS"
```

Expected: SafeFS tests pass.

---

### Task 2: Server Option and ToStatic Integration

**Files:**
- Modify: `ghttp/config.go`
- Modify: `ghttp/builder.go`
- Modify: `ghttp/static_test.go`

**Interfaces:**
- Consumes: `NewSafeFS(root string) (http.FileSystem, error)`
- Produces: `func WithVFSPath(root string) ServerOption`
- Produces: `func (b *RouteBuilder[Req, Resp]) ToStatic(root ...string)`

- [ ] **Step 1: Write failing route integration tests**

Add to `ghttp/static_test.go`:

```go
func TestStaticUsesConfiguredVFSPath(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("from-vfs"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New(WithProduces(MIMEJSON), WithVFSPath(tmpDir))
	Route[struct{}, struct{}](app).GET("/").ToStatic()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/a.txt", nil)
	app.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if w.Body.String() != "from-vfs" {
		t.Fatalf("body = %q, want from-vfs", w.Body.String())
	}
}

func TestStaticRejectsEncodedTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	parentFile := filepath.Join(filepath.Dir(tmpDir), "outside.txt")
	if err := os.WriteFile(parentFile, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New(WithProduces(MIMEJSON), WithVFSPath(tmpDir))
	Route[struct{}, struct{}](app).GET("/").ToStatic()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/%2e%2e/outside.txt", nil)
	app.ServeHTTP(w, r)

	if w.Code == http.StatusOK {
		t.Fatalf("encoded traversal returned 200 with body=%q", w.Body.String())
	}
}
```

- [ ] **Step 2: Verify tests fail**

Run:

```bash
go test ./ghttp -run "TestStaticUsesConfiguredVFSPath|TestStaticRejectsEncodedTraversal"
```

Expected: build fails because `WithVFSPath` is undefined and `ToStatic()` does not accept zero arguments.

- [ ] **Step 3: Add config option**

In `ghttp/config.go`, add `vfsPath string` to `Config` and:

```go
func WithVFSPath(root string) ServerOption {
	return func(c *Config) {
		c.vfsPath = root
	}
}
```

- [ ] **Step 4: Wire `ToStatic` through SafeFS**

Replace `ToStatic(root string)` in `ghttp/builder.go` with:

```go
func (b *RouteBuilder[Req, Resp]) ToStatic(root ...string) {
	staticRoot := b.target.owner().config.vfsPath
	if len(root) > 0 {
		staticRoot = root[0]
	}
	if staticRoot == "" {
		b.recordSetupError(ErrStaticRootRequired)
		return
	}
	fsys, err := NewSafeFS(staticRoot)
	if err != nil {
		b.recordSetupError(err)
		return
	}
	b.ToStaticFS(fsys)
}
```

- [ ] **Step 5: Verify integration tests pass**

Run:

```bash
go test ./ghttp -run "TestStatic"
```

Expected: static tests pass.

---

### Task 3: Platform Edge Cases and Final Validation

**Files:**
- Modify: `ghttp/vfs_test.go`
- Modify: `ghttp/README.md`

**Interfaces:**
- Consumes: `NewSafeFS`
- Consumes: `WithVFSPath`

- [ ] **Step 1: Add symlink and path-cleaning tests**

Add tests:

```go
func TestCleanVFSName(t *testing.T) {
	cases := map[string]string{
		"/":         ".",
		"/a.txt":   "a.txt",
		"dir/b.txt": "dir/b.txt",
	}
	for input, want := range cases {
		got, err := cleanVFSName(input)
		if err != nil {
			t.Fatalf("cleanVFSName(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("cleanVFSName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSafeFSRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation on Windows can require privileges")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	fsys, err := NewSafeFS(root)
	if err != nil {
		t.Fatal(err)
	}
	if file, err := fsys.Open("link.txt"); err == nil {
		file.Close()
		t.Fatal("symlink escape opened successfully, want error")
	}
}
```

- [ ] **Step 2: Document VFS usage**

Add a short section to `ghttp/README.md` showing:

```go
app := ghttp.New(ghttp.WithVFSPath("/srv/files"))
ghttp.Route[struct{}, struct{}](app).GET("/").ToStatic()
```

State that static paths are resolved under the configured root and traversal
attempts are rejected.

- [ ] **Step 3: Format touched Go files**

Run:

```bash
gofmt -w ghttp/config.go ghttp/builder.go ghttp/vfs.go ghttp/vfs_test.go ghttp/static_test.go
```

Expected: command exits 0.

- [ ] **Step 4: Run related tests**

Run:

```bash
go test ./ghttp
```

Expected: package passes.

- [ ] **Step 5: Run repository tests**

Run:

```bash
go test ./...
```

Expected: repository tests pass.

---

## Self-Review

- Spec coverage: tasks cover SafeFS construction, root confinement, `WithVFSPath`, `ToStatic()` integration, traversal rejection, symlink escape rejection, existing `ToStatic(root)` compatibility, and documentation.
- Placeholder scan: no unresolved placeholders are present.
- Type consistency: `NewSafeFS`, `cleanVFSName`, `WithVFSPath`, and `ToStatic(root ...string)` signatures are consistent across tasks.
