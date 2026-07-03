# ghttp VFS Static Root Design

## Goal

`ghttp` should provide a safe static-file root for applications that expose
files through HTTP. When a server is configured with:

```go
app := ghttp.New(ghttp.WithVFSPath("/file/path"))
```

a request for `/a.txt` should resolve to `/file/path/a.txt`, not to the
process working directory or the operating-system root. Requests that try to
escape the configured root, including `../` traversal, percent-encoded
traversal, Windows absolute paths, and backslash separator tricks, must be
rejected.

## Background

Go already provides the right primitive for this project: `os.OpenRoot` and
`os.Root` in Go 1.24+. `os.Root` constrains file operations to a directory tree
and rejects paths that escape the root, including symlink escapes. The project
already targets Go 1.25, so `ghttp` can use this standard-library mechanism
without adding a dependency.

`http.Dir` is not enough for this feature. It follows symlinks that point
outside the configured directory and is documented as potentially exposing
sensitive files. `ghttp` should make its built-in static file helper safer by
default while preserving the custom `ToStaticFS(http.FileSystem)` escape hatch.

## Public API

Add server-level static root configuration:

```go
func WithVFSPath(root string) ServerOption
```

Add a safe file-system constructor for users who want explicit control:

```go
func NewSafeFS(root string) (http.FileSystem, error)
```

Change `ToStatic` to accept an optional root while staying source compatible
with existing code:

```go
func (b *RouteBuilder[Req, Resp]) ToStatic(root ...string)
```

Behavior:

- `ToStatic("/file/path")` serves from the explicit root.
- `ToStatic()` serves from `WithVFSPath`.
- `ToStatic()` without `WithVFSPath` records `ErrStaticRootRequired`.
- `ToStaticFS(fs http.FileSystem)` remains unchanged for advanced users and
  embedded file systems.
- `ToStaticFile(path)` remains unchanged because it serves one explicit file,
  not a directory tree.

## Path Semantics

HTTP request paths use slash-separated URL path semantics. The VFS layer must
normalize only to a slash-separated relative file name before calling
`os.Root.Open`.

Accepted names:

- `a.txt`
- `/a.txt`
- `dir/b.txt`
- `/dir/b.txt`
- `.` for the static root directory

Rejected names:

- empty file names after cleaning
- `..`
- `../a.txt`
- `/../../a.txt`
- `dir/../../a.txt`
- percent-decoded traversal such as `/%2e%2e/a.txt`
- names containing `\` so Windows backslashes cannot become path separators
- Windows drive or reserved-name escapes, which `os.Root` and `filepath.IsLocal`
  reject on Windows

The implementation should use `path.Clean` for URL slash normalization and
`io/fs.ValidPath` plus `filepath.IsLocal` for lexical checks. It must not use
manual string concatenation to build native file paths.

## Root Enforcement

The safe file system should open files through `os.Root`, not by joining root
and request path manually.

Recommended implementation shape:

```go
type safeFS struct {
	root string
}

func (fsys *safeFS) Open(name string) (http.File, error) {
	name, err := cleanVFSName(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(fsys.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(name)
}
```

Opening the root per request avoids keeping a hidden root handle alive through
`http.FileSystem`, which has no `Close` method. It also keeps the API simple.
The cost is acceptable for a safety-focused default static-file helper.

`os.Root` follows symlinks only when they remain inside the root. Symlinks that
escape the root must fail.

## HTTP Behavior

Invalid or escaping paths should behave like missing files. `http.FileServer`
maps failed `Open` calls to HTTP errors, so the VFS should return errors such as
`fs.ErrNotExist` or a wrapped `*os.PathError` without exposing the real root
path in response bodies.

Directory listing behavior should remain whatever `http.FileServer` provides
for an opened directory. This feature is about root confinement, not directory
index policy.

## Cross-Platform Requirements

- URL path handling is always slash-based.
- Native path handling is delegated to `os.OpenRoot`, `filepath.IsLocal`, and
  the operating system.
- Backslashes are rejected before native path handling so `..\\secret.txt`
  cannot mean traversal on Windows.
- Windows absolute paths, drive-relative paths, and reserved device names must
  not be accepted as request names.
- Linux, macOS, and Windows should share the same public API and tests. Tests
  for OS-specific reserved-name behavior may be guarded by runtime checks.

## Testing

Add focused unit tests in `ghttp/vfs_test.go` and route integration tests in
`ghttp/static_test.go`.

Required coverage:

- `NewSafeFS(root).Open("/a.txt")` reads a file under root.
- `Open("../a.txt")`, `Open("/../../a.txt")`, and encoded traversal through an
  HTTP request do not read files outside root.
- Backslash traversal is rejected.
- `WithVFSPath(root)` plus `ToStatic()` serves `/a.txt` from root.
- Existing `ToStatic(root)` calls still compile and serve files.
- A symlink pointing outside root is rejected when the platform supports
  symlinks.

Validation commands:

```bash
gofmt -w ghttp/config.go ghttp/builder.go ghttp/vfs.go ghttp/vfs_test.go ghttp/static_test.go
go test ./ghttp
go test ./...
```
