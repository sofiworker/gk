# GRX Reflection Toolkit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a stronger `grx` reflection toolkit with cached struct metadata, tag-aware mapping, compiled traversals, and lightweight struct binding while preserving existing APIs.

**Architecture:** Add a new `Mapper`/`StructMap`/`FieldInfo` layer beside the existing `FieldCache`, using immutable per-mapper type maps and copy-on-write cache publication. Build field metadata with BFS traversal, parse tags once, and expose read-only/write traversal helpers for hot paths.

**Tech Stack:** Go 1.25, standard library `reflect`, `sync`, `sync/atomic`, `errors`, `strconv`, and existing package-local tests; no new dependencies.

---

## Global Constraints

- Always respond and document implementation notes in Chinese when interacting with the user.
- Keep current public APIs working unless a task explicitly updates compatibility behavior.
- Do not add external dependencies.
- Use `gofmt` on touched Go files.
- Keep tests beside code as `*_test.go`.
- Prefer table-driven tests for field mapping and conversion cases.
- Unless a task touches cross-package behavior, run targeted `go test ./grx`.
- After implementation is complete, run `make check` and `go test ./...` if the repository Makefile is available.
- Do not reformat unrelated files.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `grx/errors.go` | Public sentinel errors and `FieldError`. |
| `grx/tag.go` | `Tag`, `TagOptions`, tag parsing, simple name mappers. |
| `grx/mapper.go` | `Mapper`, options, default mapper, copy-on-write type cache. |
| `grx/struct_map.go` | `StructMap`, `FieldInfo`, BFS mapping, conflict resolution. |
| `grx/traversal.go` | `FieldByIndexesReadOnly`, `FieldByIndexesAlloc`, `Accessor`, batch traversal APIs. |
| `grx/bind.go` | Lightweight map/string-map to struct binding. |
| `grx/reflect.go` | Existing compatibility API; later tasks may delegate some internals to mapper. |
| `grx/*_test.go` | Unit tests for each responsibility. |
| `grx/bench_test.go` | Benchmarks for cache hits, traversal, and binding. |
| `grx/README.md` | Recommended usage examples and compatibility notes. |

---

### Task 1: Public Errors

**Files:**
- Create: `grx/errors.go`
- Create: `grx/errors_test.go`

- [ ] **Step 1: Write failing tests**

Create `grx/errors_test.go`:

```go
package grx

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestFieldErrorWrapsSentinel(t *testing.T) {
	err := &FieldError{
		Op:   "lookup",
		Type: reflect.TypeOf(struct{ Name string }{}),
		Name: "Name",
		Err:  ErrFieldNotFound,
	}

	if !errors.Is(err, ErrFieldNotFound) {
		t.Fatalf("errors.Is failed")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "lookup") || !strings.Contains(got, "Name") {
		t.Fatalf("unexpected error text: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./grx -run TestFieldErrorWrapsSentinel
```

Expected: build fails because `FieldError` and `ErrFieldNotFound` are undefined.

- [ ] **Step 3: Implement errors**

Create `grx/errors.go`:

```go
package grx

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrNotStruct      = errors.New("grx: type is not a struct")
	ErrFieldNotFound  = errors.New("grx: field not found")
	ErrAmbiguousField = errors.New("grx: ambiguous field")
	ErrUnexported     = errors.New("grx: field is unexported")
	ErrCannotSet      = errors.New("grx: field cannot be set")
	ErrNilValue       = errors.New("grx: nil value")
)

type FieldError struct {
	Op    string
	Type  reflect.Type
	Name  string
	Path  string
	Index []int
	Err   error
}

func (e *FieldError) Error() string {
	if e == nil {
		return "<nil>"
	}
	target := e.Name
	if target == "" {
		target = e.Path
	}
	if e.Type != nil {
		return fmt.Sprintf("grx: %s %s on %s: %v", e.Op, target, e.Type, e.Err)
	}
	return fmt.Sprintf("grx: %s %s: %v", e.Op, target, e.Err)
}

func (e *FieldError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
```

- [ ] **Step 4: Fix test imports and run**

Run:

```bash
gofmt -w grx/errors.go grx/errors_test.go
go test ./grx -run TestFieldErrorWrapsSentinel
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add grx/errors.go grx/errors_test.go
git commit -m "feat: add grx field errors"
```

---

### Task 2: Tag Parsing

**Files:**
- Create: `grx/tag.go`
- Create: `grx/tag_test.go`

- [ ] **Step 1: Write failing tests**

Create table-driven tests:

```go
func TestParseTag(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		raw       reflect.StructTag
		wantName  string
		wantRaw   string
		wantSkip  bool
		wantOpts  []string
		wantValue map[string]string
	}{
		{name: "missing", key: "json", raw: `db:"id"`, wantName: ""},
		{name: "named", key: "json", raw: `json:"user_id,omitempty"`, wantName: "user_id", wantRaw: "user_id,omitempty", wantOpts: []string{"omitempty"}},
		{name: "skip", key: "json", raw: `json:"-"`, wantSkip: true},
		{name: "empty name", key: "json", raw: `json:",omitempty"`, wantName: "", wantOpts: []string{"omitempty"}},
		{name: "key value option", key: "db", raw: `db:"name,size=64"`, wantName: "name", wantValue: map[string]string{"size": "64"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTag(tt.key, tt.raw, nil)
			if got.Name != tt.wantName || got.Raw != tt.wantRaw || got.Ignored != tt.wantSkip {
				t.Fatalf("tag = %#v", got)
			}
			for _, opt := range tt.wantOpts {
				if !got.Options.Has(opt) {
					t.Fatalf("missing option %q in %#v", opt, got.Options)
				}
			}
			for key, want := range tt.wantValue {
				if got, ok := got.Options.Get(key); !ok || got != want {
					t.Fatalf("option %s = %q, %v; want %q", key, got, ok, want)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./grx -run TestParseTag
```

Expected: build fails because `ParseTag` is undefined.

- [ ] **Step 3: Implement tag parser**

Create `grx/tag.go`:

```go
package grx

import (
	"reflect"
	"strings"
)

type Tag struct {
	Key     string
	Raw     string
	Name    string
	Options TagOptions
	Present bool
	Ignored bool
}

type TagOptions map[string]string

func (opts TagOptions) Has(name string) bool {
	_, ok := opts[name]
	return ok
}

func (opts TagOptions) Get(name string) (string, bool) {
	value, ok := opts[name]
	return value, ok
}

func ParseTag(key string, tag reflect.StructTag, tagMapFunc func(string) string) Tag {
	if key == "" || !strings.Contains(string(tag), key+":") {
		return Tag{Key: key, Options: TagOptions{}}
	}
	raw := tag.Get(key)
	if tagMapFunc != nil {
		raw = tagMapFunc(raw)
	}
	parsed := Tag{Key: key, Raw: raw, Present: true, Options: TagOptions{}}
	if raw == "-" {
		parsed.Ignored = true
		return parsed
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 0 {
		parsed.Name = parts[0]
	}
	for _, opt := range parts[1:] {
		if opt == "" {
			continue
		}
		key, value, ok := strings.Cut(opt, "=")
		if ok {
			parsed.Options[key] = value
			continue
		}
		parsed.Options[opt] = ""
	}
	return parsed
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w grx/tag.go grx/tag_test.go
go test ./grx -run TestParseTag
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add grx/tag.go grx/tag_test.go
git commit -m "feat: parse grx struct tags"
```

---

### Task 3: Mapper Cache

**Files:**
- Create: `grx/mapper.go`
- Create: `grx/mapper_test.go`

- [ ] **Step 1: Write failing tests for construction and cache hits**

```go
func TestMapperTypeMapCachesByType(t *testing.T) {
	type sample struct {
		ID int `json:"id"`
	}

	m := NewMapper(WithTagName("json"))
	first, err := m.TypeMap(reflect.TypeOf(sample{}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.TypeMap(reflect.TypeOf(&sample{}))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("TypeMap did not return cached map")
	}
}

func TestMapperRejectsNonStruct(t *testing.T) {
	_, err := NewMapper().TypeMap(reflect.TypeOf(1))
	if !errors.Is(err, ErrNotStruct) {
		t.Fatalf("err = %v, want ErrNotStruct", err)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./grx -run "TestMapper(TypeMapCachesByType|RejectsNonStruct)"
```

Expected: build fails because mapper APIs are undefined.

- [ ] **Step 3: Implement mapper shell and COW cache**

Create `grx/mapper.go`:

```go
package grx

import (
	"reflect"
	"sync"
	"sync/atomic"
)

type Mapper struct {
	cfg   mapperConfig
	cache typeMapCache
}

type mapperConfig struct {
	tagName          string
	nameMapper       func(string) string
	tagMapper        func(string) string
	ignoreUnexported bool
}

type MapperOption func(*mapperConfig)

func NewMapper(opts ...MapperOption) *Mapper {
	cfg := mapperConfig{ignoreUnexported: true}
	for _, opt := range opts {
		opt(&cfg)
	}
	m := &Mapper{cfg: cfg}
	m.cache.init()
	return m
}

func NewMapperForTag(tagName string) *Mapper {
	return NewMapper(WithTagName(tagName))
}

func WithTagName(name string) MapperOption {
	return func(cfg *mapperConfig) { cfg.tagName = name }
}

func WithNameMapper(fn func(string) string) MapperOption {
	return func(cfg *mapperConfig) { cfg.nameMapper = fn }
}

func WithTagMapper(fn func(string) string) MapperOption {
	return func(cfg *mapperConfig) { cfg.tagMapper = fn }
}

func WithIgnoreUnexported(ignore bool) MapperOption {
	return func(cfg *mapperConfig) { cfg.ignoreUnexported = ignore }
}

type typeMapCache struct {
	mu sync.Mutex
	v  atomic.Value // map[reflect.Type]*StructMap
}

func (c *typeMapCache) init() {
	c.v.Store(map[reflect.Type]*StructMap{})
}

func (m *Mapper) TypeMap(t reflect.Type) (*StructMap, error) {
	t = derefType(t)
	if t == nil || t.Kind() != reflect.Struct {
		return nil, &FieldError{Op: "typemap", Type: t, Err: ErrNotStruct}
	}
	if sm, ok := m.cache.load(t); ok {
		return sm, nil
	}
	m.cache.mu.Lock()
	defer m.cache.mu.Unlock()
	if sm, ok := m.cache.load(t); ok {
		return sm, nil
	}
	sm, err := buildStructMap(t, m.cfg)
	if err != nil {
		return nil, err
	}
	m.cache.store(t, sm)
	return sm, nil
}

func (m *Mapper) MustTypeMap(t reflect.Type) *StructMap {
	sm, err := m.TypeMap(t)
	if err != nil {
		panic(err)
	}
	return sm
}

func (c *typeMapCache) load(t reflect.Type) (*StructMap, bool) {
	maps := c.v.Load().(map[reflect.Type]*StructMap)
	sm, ok := maps[t]
	return sm, ok
}

func (c *typeMapCache) store(t reflect.Type, sm *StructMap) {
	old := c.v.Load().(map[reflect.Type]*StructMap)
	next := make(map[reflect.Type]*StructMap, len(old)+1)
	for key, value := range old {
		next[key] = value
	}
	next[t] = sm
	c.v.Store(next)
}

func derefType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}
```

This task calls `buildStructMap`; add a temporary minimal `struct_map.go` in the next step and replace it with the full implementation in Task 4.

- [ ] **Step 4: Run tests with temporary buildStructMap**

If Task 4 is not implemented yet, create `grx/struct_map.go` with temporary definitions:

```go
package grx

import "reflect"

type StructMap struct {
	Type  reflect.Type
	Names map[string]*FieldInfo
	Paths map[string]*FieldInfo
}

type FieldInfo struct{}

func buildStructMap(t reflect.Type, cfg mapperConfig) (*StructMap, error) {
	return &StructMap{Type: t, Names: map[string]*FieldInfo{}, Paths: map[string]*FieldInfo{}}, nil
}
```

Run:

```bash
gofmt -w grx/mapper.go grx/mapper_test.go grx/struct_map.go
go test ./grx -run "TestMapper(TypeMapCachesByType|RejectsNonStruct)"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add grx/mapper.go grx/mapper_test.go grx/struct_map.go
git commit -m "feat: add grx mapper cache"
```

---

### Task 4: StructMap and BFS Field Mapping

**Files:**
- Modify: `grx/struct_map.go`
- Create: `grx/struct_map_test.go`

- [ ] **Step 1: Write failing tests for field metadata**

```go
func TestStructMapNamesAndTags(t *testing.T) {
	type embedded struct {
		CreatedAt int `json:"created_at"`
	}
	type sample struct {
		embedded
		ID   int    `json:"id"`
		Name string `json:"name,omitempty"`
		Skip string `json:"-"`
	}

	sm := NewMapper(WithTagName("json")).MustTypeMap(reflect.TypeOf(sample{}))
	if _, ok := sm.Names["id"]; !ok {
		t.Fatalf("missing id")
	}
	name := sm.Names["name"]
	if name == nil || !name.Options.Has("omitempty") {
		t.Fatalf("name field/options not parsed: %#v", name)
	}
	if _, ok := sm.Names["Skip"]; ok {
		t.Fatalf("skipped field is mapped")
	}
	if got := sm.Names["created_at"]; got == nil || len(got.Index) != 2 {
		t.Fatalf("embedded field index = %#v", got)
	}
}
```

- [ ] **Step 2: Write conflict tests**

```go
func TestStructMapConflicts(t *testing.T) {
	type left struct {
		Value string `json:"value"`
	}
	type right struct {
		Value string `json:"value"`
	}
	type sample struct {
		left
		right
	}

	sm := NewMapper(WithTagName("json")).MustTypeMap(reflect.TypeOf(sample{}))
	if _, ok := sm.Names["value"]; ok {
		t.Fatalf("ambiguous field should not be resolved")
	}
	if len(sm.Conflicts["value"]) != 2 {
		t.Fatalf("conflicts = %#v", sm.Conflicts)
	}
	if _, err := sm.LookupName("value"); !errors.Is(err, ErrAmbiguousField) {
		t.Fatalf("err = %v, want ErrAmbiguousField", err)
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./grx -run "TestStructMap"
```

Expected: tests fail because `StructMap` is incomplete.

- [ ] **Step 4: Implement `StructMap` and `FieldInfo`**

Replace the temporary stub with:

```go
type StructMap struct {
	Type      reflect.Type
	Tree      *FieldInfo
	Fields    []*FieldInfo
	Names     map[string]*FieldInfo
	Paths     map[string]*FieldInfo
	Conflicts map[string][]*FieldInfo
}

type FieldInfo struct {
	Field     reflect.StructField
	Index     []int
	Path      string
	Name      string
	Tag       Tag
	Options   TagOptions
	Exported  bool
	Anonymous bool
	Embedded  bool
	Depth     int
	Parent    *FieldInfo
	Children  []*FieldInfo
}

func (sm *StructMap) LookupName(name string) (*FieldInfo, error) {
	if fields := sm.Conflicts[name]; len(fields) > 0 {
		return nil, &FieldError{Op: "lookup", Type: sm.Type, Name: name, Err: ErrAmbiguousField}
	}
	fi, ok := sm.Names[name]
	if !ok {
		return nil, &FieldError{Op: "lookup", Type: sm.Type, Name: name, Err: ErrFieldNotFound}
	}
	return fi, nil
}
```

Implement BFS with a queue:

```go
type fieldQueueItem struct {
	t      reflect.Type
	parent *FieldInfo
	prefix string
	index  []int
	depth  int
}
```

Core rules:

- `field.PkgPath != "" && !field.Anonymous` is skipped when `ignoreUnexported` is true.
- `ParseTag(cfg.tagName, field.Tag, cfg.tagMapper)` runs once per field.
- `Tag.Ignored` skips the field.
- If tag name is empty, use `cfg.nameMapper(field.Name)` when configured, otherwise `field.Name`.
- Anonymous `struct` or `*struct` fields are queued for BFS.
- Copy every `Index` slice before storing.
- Fill `Fields`, `Paths`, `Names`, and `Conflicts` after traversal using the conflict rules from the design spec.

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w grx/struct_map.go grx/struct_map_test.go
go test ./grx -run "TestStructMap|TestMapper"
```

Expected: PASS.

- [ ] **Step 6: Add concurrency test**

Add:

```go
func TestMapperTypeMapConcurrent(t *testing.T) {
	type sample struct {
		ID int `json:"id"`
	}
	mapper := NewMapper(WithTagName("json"))
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := mapper.TypeMap(reflect.TypeOf(sample{})); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
```

Run:

```bash
gofmt -w grx/struct_map_test.go
go test ./grx -run TestMapperTypeMapConcurrent -race
```

Expected: PASS with no race.

- [ ] **Step 7: Commit**

```bash
git add grx/struct_map.go grx/struct_map_test.go grx/mapper.go grx/mapper_test.go
git commit -m "feat: map struct fields with grx mapper"
```

---

### Task 5: Field Traversal Helpers

**Files:**
- Create: `grx/traversal.go`
- Create: `grx/traversal_test.go`

- [ ] **Step 1: Write read-only traversal tests**

```go
func TestFieldByIndexesReadOnlyDoesNotAllocate(t *testing.T) {
	type child struct {
		Name string
	}
	type parent struct {
		Child *child
	}
	p := parent{}

	_, err := FieldByIndexesReadOnly(reflect.ValueOf(p), []int{0, 0})
	if !errors.Is(err, ErrNilValue) {
		t.Fatalf("err = %v, want ErrNilValue", err)
	}
	if p.Child != nil {
		t.Fatalf("read-only traversal allocated child")
	}
}
```

- [ ] **Step 2: Write allocating traversal tests**

```go
func TestFieldByIndexesAllocAllocatesPointersAndMaps(t *testing.T) {
	type child struct {
		Labels map[string]string
	}
	type parent struct {
		Child *child
	}
	p := parent{}

	v, err := FieldByIndexesAlloc(reflect.ValueOf(&p), []int{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if p.Child == nil || p.Child.Labels == nil {
		t.Fatalf("expected pointer and map allocation")
	}
	if v.Kind() != reflect.Map {
		t.Fatalf("kind = %s, want map", v.Kind())
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./grx -run "TestFieldByIndexes"
```

Expected: build fails because traversal helpers are undefined.

- [ ] **Step 4: Implement traversal helpers**

Create `grx/traversal.go`:

```go
func FieldByIndexesReadOnly(v reflect.Value, indexes []int) (reflect.Value, error) {
	if !v.IsValid() {
		return reflect.Value{}, ErrNilValue
	}
	for _, idx := range indexes {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}, ErrNilValue
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, ErrNotStruct
		}
		v = v.Field(idx)
	}
	return v, nil
}

func FieldByIndexesAlloc(v reflect.Value, indexes []int) (reflect.Value, error) {
	if !v.IsValid() {
		return reflect.Value{}, ErrNilValue
	}
	for _, idx := range indexes {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				if !v.CanSet() {
					return reflect.Value{}, ErrCannotSet
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, ErrNotStruct
		}
		v = v.Field(idx)
		if v.Kind() == reflect.Map && v.IsNil() {
			if !v.CanSet() {
				return reflect.Value{}, ErrCannotSet
			}
			v.Set(reflect.MakeMap(v.Type()))
		}
	}
	return v, nil
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w grx/traversal.go grx/traversal_test.go
go test ./grx -run "TestFieldByIndexes"
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add grx/traversal.go grx/traversal_test.go
git commit -m "feat: add grx field traversal helpers"
```

---

### Task 6: Mapper Lookup and Batch Traversals

**Files:**
- Modify: `grx/mapper.go`
- Modify: `grx/traversal.go`
- Create or modify: `grx/traversal_test.go`

- [ ] **Step 1: Write tests for mapper field lookup**

```go
func TestMapperFieldByName(t *testing.T) {
	type sample struct {
		Name string `json:"name"`
	}
	v := reflect.ValueOf(sample{Name: "gk"})
	got, ok := NewMapper(WithTagName("json")).FieldByName(v, "name")
	if !ok || got.String() != "gk" {
		t.Fatalf("FieldByName = %v, %v", got, ok)
	}
}
```

- [ ] **Step 2: Write tests for batch traversals**

```go
func TestMapperTraversalsByName(t *testing.T) {
	type sample struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	mapper := NewMapper(WithTagName("db"))
	got, err := mapper.TraversalsByName(reflect.TypeOf(sample{}), []string{"name", "missing", "id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || len(got[0]) != 1 || got[1] != nil || got[2][0] != 0 {
		t.Fatalf("traversals = %#v", got)
	}
}
```

- [ ] **Step 3: Run tests to verify failure**

Run:

```bash
go test ./grx -run "TestMapper(FieldByName|TraversalsByName)"
```

Expected: build fails because methods are undefined.

- [ ] **Step 4: Implement methods**

Add to `mapper.go`:

```go
func (m *Mapper) FieldByName(v reflect.Value, name string) (reflect.Value, bool) {
	if !v.IsValid() {
		return reflect.Value{}, false
	}
	t := derefType(v.Type())
	sm, err := m.TypeMap(t)
	if err != nil {
		return reflect.Value{}, false
	}
	fi, err := sm.LookupName(name)
	if err != nil {
		return reflect.Value{}, false
	}
	got, err := FieldByIndexesReadOnly(v, fi.Index)
	if err != nil {
		return reflect.Value{}, false
	}
	return got, true
}

func (m *Mapper) TraversalsByName(t reflect.Type, names []string) ([][]int, error) {
	out := make([][]int, len(names))
	err := m.TraversalsByNameFunc(t, names, func(i int, indexes []int) error {
		if indexes == nil {
			return nil
		}
		out[i] = append([]int(nil), indexes...)
		return nil
	})
	return out, err
}

func (m *Mapper) TraversalsByNameFunc(t reflect.Type, names []string, fn func(int, []int) error) error {
	sm, err := m.TypeMap(t)
	if err != nil {
		return err
	}
	for i, name := range names {
		fi, err := sm.LookupName(name)
		if errors.Is(err, ErrFieldNotFound) || errors.Is(err, ErrAmbiguousField) {
			if err := fn(i, nil); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if err := fn(i, fi.Index); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
gofmt -w grx/mapper.go grx/traversal.go grx/traversal_test.go
go test ./grx -run "TestMapper(FieldByName|TraversalsByName)"
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add grx/mapper.go grx/traversal.go grx/traversal_test.go
git commit -m "feat: add grx mapper traversal lookups"
```

---

### Task 7: Accessor and Strict Set

**Files:**
- Modify: `grx/traversal.go`
- Modify: `grx/traversal_test.go`

- [ ] **Step 1: Write accessor tests**

```go
func TestAccessorSet(t *testing.T) {
	type sample struct {
		Count int
	}
	s := sample{}
	a := NewAccessor([]int{0}, reflect.TypeOf(0))
	if err := a.Set(reflect.ValueOf(&s), int64(3)); err != nil {
		t.Fatal(err)
	}
	if s.Count != 3 {
		t.Fatalf("Count = %d, want 3", s.Count)
	}
}

func TestAccessorSetRejectsInvalidConversion(t *testing.T) {
	type sample struct {
		Count int
	}
	s := sample{}
	a := NewAccessor([]int{0}, reflect.TypeOf(0))
	if err := a.Set(reflect.ValueOf(&s), "bad"); !errors.Is(err, ErrCannotSet) {
		t.Fatalf("err = %v, want ErrCannotSet", err)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./grx -run "TestAccessor"
```

Expected: build fails because `Accessor` is undefined.

- [ ] **Step 3: Implement Accessor**

Add:

```go
type Accessor struct {
	Index []int
	Type  reflect.Type
}

func NewAccessor(index []int, typ reflect.Type) Accessor {
	return Accessor{Index: append([]int(nil), index...), Type: typ}
}

func (a Accessor) Get(v reflect.Value) (reflect.Value, error) {
	return FieldByIndexesReadOnly(v, a.Index)
}

func (a Accessor) Set(v reflect.Value, value any) error {
	field, err := FieldByIndexesAlloc(v, a.Index)
	if err != nil {
		return err
	}
	if !field.CanSet() {
		return ErrCannotSet
	}
	if value == nil {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}
	in := reflect.ValueOf(value)
	if in.Type().AssignableTo(field.Type()) {
		field.Set(in)
		return nil
	}
	if in.Type().ConvertibleTo(field.Type()) {
		field.Set(in.Convert(field.Type()))
		return nil
	}
	return ErrCannotSet
}
```

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w grx/traversal.go grx/traversal_test.go
go test ./grx -run "TestAccessor|TestFieldByIndexes"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add grx/traversal.go grx/traversal_test.go
git commit -m "feat: add grx field accessors"
```

---

### Task 8: Lightweight Binder

**Files:**
- Create: `grx/bind.go`
- Create: `grx/bind_test.go`

- [ ] **Step 1: Write bind tests**

```go
func TestBinderBindMap(t *testing.T) {
	type sample struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}
	var dst sample
	err := NewBinder(NewMapper(WithTagName("json"))).BindMap(&dst, map[string]any{
		"id":     int64(7),
		"name":   "gk",
		"active": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dst.ID != 7 || dst.Name != "gk" || !dst.Active {
		t.Fatalf("dst = %#v", dst)
	}
}

func TestBinderBindStringMap(t *testing.T) {
	type sample struct {
		ID     int  `form:"id"`
		Active bool `form:"active"`
	}
	var dst sample
	err := NewBinder(NewMapper(WithTagName("form"))).BindStringMap(&dst, map[string]string{
		"id":     "7",
		"active": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dst.ID != 7 || !dst.Active {
		t.Fatalf("dst = %#v", dst)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./grx -run "TestBinder"
```

Expected: build fails because `Binder` is undefined.

- [ ] **Step 3: Implement Binder**

Create `grx/bind.go`:

```go
type Binder struct {
	Mapper *Mapper
}

func NewBinder(mapper *Mapper) *Binder {
	if mapper == nil {
		mapper = NewMapper()
	}
	return &Binder{Mapper: mapper}
}

func (b *Binder) BindMap(dst any, values map[string]any) error {
	v := reflect.ValueOf(dst)
	if !v.IsValid() || v.Kind() != reflect.Ptr || v.IsNil() {
		return ErrNilValue
	}
	sm, err := b.Mapper.TypeMap(v.Type())
	if err != nil {
		return err
	}
	for name, value := range values {
		fi, err := sm.LookupName(name)
		if errors.Is(err, ErrFieldNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if err := NewAccessor(fi.Index, fi.Field.Type).Set(v, value); err != nil {
			return &FieldError{Op: "bind", Type: sm.Type, Name: name, Index: fi.Index, Err: err}
		}
	}
	return nil
}
```

Add `BindStringMap` with a helper that converts string to bool, signed ints, unsigned ints, floats, and string using `strconv`.

- [ ] **Step 4: Run tests**

Run:

```bash
gofmt -w grx/bind.go grx/bind_test.go
go test ./grx -run "TestBinder"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add grx/bind.go grx/bind_test.go
git commit -m "feat: add lightweight grx struct binder"
```

---

### Task 9: Compatibility Bridge for FieldCache

**Files:**
- Modify: `grx/reflect.go`
- Modify: `grx/reflect_test.go`

- [ ] **Step 1: Add regression tests for existing API**

Add tests that lock current behavior:

```go
func TestGetCachedStructFieldsReturnsCopy(t *testing.T) {
	cache := NewFieldCache()
	type sample struct {
		ID int
	}
	fields := cache.GetCachedStructFields(reflect.TypeOf(sample{}))
	delete(fields, "ID")
	if _, ok := cache.LookupFieldInfo(reflect.TypeOf(sample{}), "ID"); !ok {
		t.Fatalf("mutating returned map changed cache")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./grx -run TestGetCachedStructFieldsReturnsCopy
```

Expected: FAIL because the current API returns the internal map.

- [ ] **Step 3: Return a copy from `GetCachedStructFields`**

Modify `grx/reflect.go`:

```go
func (fc *FieldCache) GetCachedStructFields(t reflect.Type) map[string]StructFieldInfo {
	entry := fc.getEntry(indirectStructType(t))
	if entry == nil || entry.fields == nil {
		return nil
	}
	out := make(map[string]StructFieldInfo, len(entry.fields))
	for name, info := range entry.fields {
		info.Index = append([]int(nil), info.Index...)
		out[name] = info
	}
	return out
}
```

- [ ] **Step 4: Run compatibility tests**

Run:

```bash
gofmt -w grx/reflect.go grx/reflect_test.go
go test ./grx -run "Test(GetCachedStructFieldsReturnsCopy|FieldLookupAndTags|MethodLookupAndCall|FieldCacheConcurrentAccess)"
```

Expected: PASS.

- [ ] **Step 5: Decide whether to delegate FieldCache internals**

Keep this task conservative. Do not rewrite `FieldCache` yet unless the new mapper tests prove equivalent behavior for:

- field names,
- tag lookup,
- method lookup,
- embedded fields,
- concurrent cache access.

If delegation is done, preserve all old return types.

- [ ] **Step 6: Commit**

```bash
git add grx/reflect.go grx/reflect_test.go
git commit -m "fix: protect grx field cache snapshots"
```

---

### Task 10: Benchmarks

**Files:**
- Create: `grx/bench_test.go`

- [ ] **Step 1: Add benchmark structs**

Create representative nested and tagged structs:

```go
type benchEmbedded struct {
	CreatedAt int64 `json:"created_at" db:"created_at"`
	UpdatedAt int64 `json:"updated_at" db:"updated_at"`
}

type benchStruct struct {
	benchEmbedded
	ID     int    `json:"id" db:"id"`
	Name   string `json:"name" db:"name"`
	Email  string `json:"email" db:"email"`
	Active bool   `json:"active" db:"active"`
}
```

- [ ] **Step 2: Add cache and traversal benchmarks**

```go
func BenchmarkMapperTypeMapWarm(b *testing.B) {
	mapper := NewMapper(WithTagName("json"))
	typ := reflect.TypeOf(benchStruct{})
	if _, err := mapper.TypeMap(typ); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mapper.TypeMap(typ); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTraversalsByNameFunc(b *testing.B) {
	mapper := NewMapper(WithTagName("db"))
	names := []string{"id", "name", "email", "active", "created_at", "updated_at"}
	typ := reflect.TypeOf(benchStruct{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		err := mapper.TraversalsByNameFunc(typ, names, func(_ int, _ []int) error { return nil })
		if err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 3: Run benchmarks**

Run:

```bash
go test ./grx -bench "Benchmark(Mapper|Traversals|FieldByIndexes|Binder)" -benchmem
```

Expected: benchmarks complete. Record current numbers in the PR notes rather than asserting exact ns/op in tests.

- [ ] **Step 4: Commit**

```bash
git add grx/bench_test.go
git commit -m "test: add grx reflection benchmarks"
```

---

### Task 11: README

**Files:**
- Modify: `grx/README.md`

- [ ] **Step 1: Update usage examples**

Add examples for:

- `NewMapper(WithTagName("json"))`
- `TypeMap` and `LookupName`
- `TraversalsByName`
- `FieldByIndexesReadOnly`
- `NewBinder(...).BindStringMap`
- note that older `FieldCache` helpers remain available.

- [ ] **Step 2: Verify README examples compile mentally against public API**

Keep examples small:

```go
mapper := grx.NewMapper(grx.WithTagName("json"))
sm := mapper.MustTypeMap(reflect.TypeOf(User{}))
field, err := sm.LookupName("id")
```

- [ ] **Step 3: Commit**

```bash
git add grx/README.md
git commit -m "docs: document grx reflection mapper"
```

---

### Task 12: Final Verification

**Files:**
- No new files unless fixes are needed.

- [ ] **Step 1: Run package tests**

Run:

```bash
go test ./grx
```

Expected: PASS.

- [ ] **Step 2: Run race test for grx concurrency**

Run:

```bash
go test ./grx -race
```

Expected: PASS.

- [ ] **Step 3: Run benchmarks once**

Run:

```bash
go test ./grx -bench . -benchmem
```

Expected: benchmark output prints without failures.

- [ ] **Step 4: Run repository checks**

Run:

```bash
make check
go test ./...
```

Expected: PASS. If unrelated existing failures appear, record exact failing packages/tests and do not hide them.

- [ ] **Step 5: Review git diff**

Run:

```bash
git diff -- grx grx/README.md
```

Confirm:

- no unrelated files changed,
- no external dependencies added,
- compatibility APIs remain,
- tests cover mapper, tags, traversal, binding, concurrency, and cache snapshot behavior.

- [ ] **Step 6: Final commit**

If all previous task commits were made, no final commit is required. If fixes were batched during verification:

```bash
git add grx grx/README.md
git commit -m "test: verify grx reflection toolkit"
```
