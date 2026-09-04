package ghttp

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// 绑定能力扩展的测试:数组/切片、map、嵌套结构体、内嵌字段递归、指针可选语义、
// TextUnmarshaler、[]byte 原始字节。
//
// 关键约束:params(path/query/header)与 form(请求体)共享同一套 fieldBinder,因此
// 每种形态都要在【两条轴上】各测一遍——它们能力一致是本次扩展的核心承诺。
//
// Tests for the extended binding capabilities: arrays/slices, maps, nested structs,
// embedded-field recursion, pointer-optional semantics, TextUnmarshaler, and raw
// []byte.
//
// Key constraint: params (path/query/header) and form (request body) share one
// fieldBinder, so every shape is tested on BOTH axes — their capability parity is
// the core promise of this extension.
// ===========================================================================

// ——— 切片与数组 / slices and arrays ——— //

type sliceParams struct {
	Tags   []string  `query:"tags"`
	IDs    []int     `query:"ids"`
	Ratios []float64 `query:"ratios"`
	Flags  []bool    `query:"flags"`
	Fixed  [3]int    `query:"fixed"`
}

func TestBindParams_Slices(t *testing.T) {
	m := New()
	var got sliceParams
	if err := GetParams(m, "/slices", JSON[map[string]any](),
		func(_ context.Context, p sliceParams) (map[string]any, error) {
			got = p
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		query string
		want  sliceParams
	}{
		{
			// 重复出现风格:?tags=a&tags=b,HTML 表单与多数客户端库的默认行为。
			name:  "repeated occurrences",
			query: "tags=a&tags=b&ids=1&ids=2&ids=3",
			want:  sliceParams{Tags: []string{"a", "b"}, IDs: []int{1, 2, 3}},
		},
		{
			// 逗号分隔风格:?tags=a,b,OpenAPI 的 form/simple 序列化常用形式。
			name:  "comma separated",
			query: "tags=a,b,c&ids=4,5",
			want:  sliceParams{Tags: []string{"a", "b", "c"}, IDs: []int{4, 5}},
		},
		{
			// 两种风格混用必须都被吸收,不能只取其一。
			name:  "mixed styles",
			query: "tags=a,b&tags=c",
			want:  sliceParams{Tags: []string{"a", "b", "c"}},
		},
		{
			name:  "typed elements",
			query: "ratios=1.5,2.5&flags=true,false,1,0",
			want:  sliceParams{Ratios: []float64{1.5, 2.5}, Flags: []bool{true, false, true, false}},
		},
		{
			// 定长数组:多余元素被丢弃,不足则留零值——不因长度不符而报错。
			name:  "fixed array truncates",
			query: "fixed=1,2,3,4,5",
			want:  sliceParams{Fixed: [3]int{1, 2, 3}},
		},
		{
			name:  "fixed array partial",
			query: "fixed=7",
			want:  sliceParams{Fixed: [3]int{7, 0, 0}},
		},
		{
			// 缺省:切片保持 nil(而非空切片),让 handler 能区分"未提供"与"提供了空列表"。
			name:  "absent stays nil",
			query: "",
			want:  sliceParams{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got = sliceParams{}
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slices?"+tc.query, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
			if !equalStrings(got.Tags, tc.want.Tags) {
				t.Errorf("Tags=%v want %v", got.Tags, tc.want.Tags)
			}
			if !equalInts(got.IDs, tc.want.IDs) {
				t.Errorf("IDs=%v want %v", got.IDs, tc.want.IDs)
			}
			if !equalFloats(got.Ratios, tc.want.Ratios) {
				t.Errorf("Ratios=%v want %v", got.Ratios, tc.want.Ratios)
			}
			if !equalBools(got.Flags, tc.want.Flags) {
				t.Errorf("Flags=%v want %v", got.Flags, tc.want.Flags)
			}
			if got.Fixed != tc.want.Fixed {
				t.Errorf("Fixed=%v want %v", got.Fixed, tc.want.Fixed)
			}
		})
	}
}

func TestBindParams_SliceElementError(t *testing.T) {
	m := New()
	if err := GetParams(m, "/bad-slice", JSON[map[string]any](),
		func(_ context.Context, p sliceParams) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// 元素解析失败必须整体 400,而不是静默丢弃坏元素——静默丢弃会让调用方以为参数生效了。
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bad-slice?ids=1,notanumber", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d want 400 body=%s", rec.Code, rec.Body.String())
	}
}

// ——— map ——— //

type mapParams struct {
	Filter map[string]string   `query:"filter"`
	Multi  map[string][]string `query:"multi"`
	Nums   map[string]int      `query:"nums"`
	Meta   map[string]string   `header:"X-Meta"`
	All    map[string]string   `header:"*"`
}

func TestBindParams_Maps(t *testing.T) {
	m := New()
	var got mapParams
	if err := GetParams(m, "/maps", JSON[map[string]any](),
		func(_ context.Context, p mapParams) (map[string]any, error) {
			got = p
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}

	// 方括号风格 filter[key]=value 是 query 里表达 map 的事实标准(Rails/PHP 生态)。
	req := httptest.NewRequest(http.MethodGet,
		"/maps?filter[status]=active&filter[kind]=user&multi[tag]=a&multi[tag]=b&nums[count]=42", nil)
	req.Header.Set("X-Meta-Region", "eu-west")
	req.Header.Set("X-Meta-Zone", "b")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	if got.Filter["status"] != "active" || got.Filter["kind"] != "user" {
		t.Errorf("Filter=%v", got.Filter)
	}
	if len(got.Filter) != 2 {
		t.Errorf("Filter should hold exactly the bracketed keys, got %v", got.Filter)
	}
	if !equalStrings(got.Multi["tag"], []string{"a", "b"}) {
		t.Errorf("Multi=%v", got.Multi)
	}
	if got.Nums["count"] != 42 {
		t.Errorf("Nums=%v", got.Nums)
	}
	// header map 用 Prefix- 家族收集:X-Meta-Region → key "region"。
	if got.Meta["region"] != "eu-west" || got.Meta["zone"] != "b" {
		t.Errorf("Meta=%v", got.Meta)
	}
	// `*` 收集全部头,键统一小写以免调用方纠缠 MIME 规范化大小写。
	if got.All["x-meta-region"] != "eu-west" {
		t.Errorf("All should contain every header lowercased, got %v", got.All)
	}
}

func TestBindParams_MapAbsentStaysNil(t *testing.T) {
	m := New()
	var got mapParams
	if err := GetParams(m, "/maps-empty", JSON[map[string]any](),
		func(_ context.Context, p mapParams) (map[string]any, error) {
			got = p
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/maps-empty", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	// 无匹配键时 map 保持 nil,不分配空 map:让"未提供"可被区分,也省一次分配。
	if got.Filter != nil {
		t.Errorf("Filter should stay nil when nothing matched, got %v", got.Filter)
	}
}

type pathMapParams struct {
	Bad map[string]string `path:"bad"`
}

func TestBindPlan_RejectsPathMap(t *testing.T) {
	m := New()
	// path 参数是单值语义,绑定 map 无意义,必须在注册期报错而不是运行期静默为空。
	err := GetParams(m, "/x/{bad}", JSON[map[string]any](),
		func(_ context.Context, p pathMapParams) (map[string]any, error) {
			return nil, nil
		})
	if err == nil {
		t.Fatal("expected registration error for a path-bound map")
	}
	if !strings.Contains(err.Error(), "map") {
		t.Errorf("error should explain the map restriction: %v", err)
	}
}

// ——— 嵌套结构体与内嵌字段 / nested structs and embedded fields ——— //

type pageParams struct {
	Page int `query:"page"`
	Size int `query:"size"`
}

type sortParams struct {
	SortBy string `query:"sort_by"`
	Desc   bool   `query:"desc"`
}

type deepNested struct {
	Level string `query:"level"`
}

type nestedGroup struct {
	Inner deepNested
	Extra string `query:"extra"`
}

type listQuery struct {
	pageParams // 内嵌:字段提升到外层,调用方看到扁平的 query 契约
	sortParams
	Nested nestedGroup        // 具名嵌套:同样递归展开,无需 tag
	Ptr    *pageParams        // 指针嵌套:按需分配
	Search string             `query:"q"`
	Ignore string             `query:"-"`
	Opaque struct{ X string } `query:"-"`
}

func TestBindParams_NestedAndEmbedded(t *testing.T) {
	m := New()
	var got listQuery
	if err := GetParams(m, "/nested", JSON[map[string]any](),
		func(_ context.Context, p listQuery) (map[string]any, error) {
			got = p
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/nested?page=2&size=50&sort_by=name&desc=true&level=deep&extra=e&q=go", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	if got.Page != 2 || got.Size != 50 {
		t.Errorf("embedded pageParams not bound: %+v", got.pageParams)
	}
	if got.SortBy != "name" || !got.Desc {
		t.Errorf("embedded sortParams not bound: %+v", got.sortParams)
	}
	if got.Nested.Inner.Level != "deep" {
		t.Errorf("deeply nested field not bound: %+v", got.Nested)
	}
	if got.Nested.Extra != "e" {
		t.Errorf("named nested sibling not bound: %+v", got.Nested)
	}
	if got.Search != "go" {
		t.Errorf("Search=%q", got.Search)
	}
	// 指针嵌套:同名 query 值同时填充内嵌与指针目标,证明指针路径确实被分配。
	if got.Ptr == nil {
		t.Fatal("pointer-nested struct should be allocated when its fields are present")
	}
	if got.Ptr.Page != 2 || got.Ptr.Size != 50 {
		t.Errorf("pointer-nested fields not bound: %+v", got.Ptr)
	}
	if got.Ignore != "" {
		t.Errorf(`tag "-" must be skipped, got %q`, got.Ignore)
	}
}

func TestBindParams_PointerNestedStaysNilWhenAbsent(t *testing.T) {
	m := New()
	var got listQuery
	if err := GetParams(m, "/nested-absent", JSON[map[string]any](),
		func(_ context.Context, p listQuery) (map[string]any, error) {
			got = p
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nested-absent?q=only", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	// 嵌套指针的字段全缺省时不应被分配:分配了就无法表达"整块未提供"。
	if got.Ptr != nil {
		t.Errorf("pointer-nested struct should stay nil when no field was provided, got %+v", got.Ptr)
	}
}

// ——— 指针标量的可选语义 / pointer scalars as optional ——— //

type optionalParams struct {
	Limit  *int    `query:"limit"`
	Name   *string `query:"name"`
	Active *bool   `query:"active"`
}

func TestBindParams_OptionalPointers(t *testing.T) {
	m := New()
	var got optionalParams
	if err := GetParams(m, "/opt", JSON[map[string]any](),
		func(_ context.Context, p optionalParams) (map[string]any, error) {
			got = p
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}

	// 缺省:指针为 nil,可与"显式传了零值"区分开——这正是指针标量存在的意义。
	got = optionalParams{}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/opt", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if got.Limit != nil || got.Name != nil || got.Active != nil {
		t.Fatalf("absent optionals should stay nil: %+v", got)
	}

	// 显式零值:指针非 nil 且指向零值。
	got = optionalParams{}
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/opt?limit=0&name=&active=false", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if got.Limit == nil || *got.Limit != 0 {
		t.Errorf("Limit should be a non-nil zero, got %v", got.Limit)
	}
	if got.Name == nil || *got.Name != "" {
		t.Errorf("Name should be a non-nil empty string, got %v", got.Name)
	}
	if got.Active == nil || *got.Active {
		t.Errorf("Active should be a non-nil false, got %v", got.Active)
	}
}

// ——— TextUnmarshaler 与 []byte ——— //

type textParams struct {
	When  time.Time   `query:"when"`
	Until *time.Time  `query:"until"`
	Addr  net.IP      `query:"addr"`
	Blob  []byte      `query:"blob"`
	Days  []time.Time `query:"days"`
}

func TestBindParams_TextUnmarshalerAndBytes(t *testing.T) {
	m := New()
	var got textParams
	if err := GetParams(m, "/text", JSON[map[string]any](),
		func(_ context.Context, p textParams) (map[string]any, error) {
			got = p
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/text?when=2026-08-24T10:00:00Z&addr=192.0.2.7&blob=hello&days=2026-01-01T00:00:00Z,2026-01-02T00:00:00Z", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	// time.Time 走 TextUnmarshaler 而非底层 struct 展开:优先级顺序是本次实现的关键点。
	if !got.When.Equal(time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("When=%v", got.When)
	}
	if got.Until != nil {
		t.Errorf("Until should stay nil when absent, got %v", got.Until)
	}
	if got.Addr.String() != "192.0.2.7" {
		t.Errorf("Addr=%v", got.Addr)
	}
	// []byte 取原始字节,不做 base64 解码——query 里的值本就是明文。
	if string(got.Blob) != "hello" {
		t.Errorf("Blob=%q want %q", got.Blob, "hello")
	}
	if len(got.Days) != 2 || !got.Days[0].Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("Days=%v", got.Days)
	}
}

func TestBindParams_TextUnmarshalerError(t *testing.T) {
	m := New()
	if err := GetParams(m, "/text-bad", JSON[map[string]any](),
		func(_ context.Context, p textParams) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/text-bad?when=not-a-time", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d want 400 body=%s", rec.Code, rec.Body.String())
	}
}

// ——— form 轴:与 params 能力对等 / the form axis: capability parity with params ——— //

type formShapes struct {
	Tags   []string            `form:"tags"`
	IDs    []int               `form:"ids"`
	Filter map[string]string   `form:"filter"`
	Multi  map[string][]string `form:"multi"`
	Opt    *int                `form:"opt"`
	When   time.Time           `form:"when"`
	Blob   []byte              `form:"blob"`
	Nested formNested
	formEmbedded
}

type formNested struct {
	Inner string `form:"inner"`
}

type formEmbedded struct {
	Promoted string `form:"promoted"`
}

func TestFormBody_ExtendedShapes(t *testing.T) {
	m := New()
	var got formShapes
	if err := PostBody(m, "/form-shapes", FormBody[formShapes](), JSON[map[string]any](),
		func(_ context.Context, b formShapes) (map[string]any, error) {
			got = b
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"tags=a&tags=b",
		"ids=1,2,3",
		"filter[status]=active",
		"multi[k]=x&multi[k]=y",
		"opt=0",
		"when=2026-08-24T10:00:00Z",
		"blob=raw",
		"inner=deep",
		"promoted=up",
	}, "&")
	req := httptest.NewRequest(http.MethodPost, "/form-shapes", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	if !equalStrings(got.Tags, []string{"a", "b"}) {
		t.Errorf("Tags=%v", got.Tags)
	}
	if !equalInts(got.IDs, []int{1, 2, 3}) {
		t.Errorf("IDs=%v", got.IDs)
	}
	if got.Filter["status"] != "active" {
		t.Errorf("Filter=%v", got.Filter)
	}
	if !equalStrings(got.Multi["k"], []string{"x", "y"}) {
		t.Errorf("Multi=%v", got.Multi)
	}
	if got.Opt == nil || *got.Opt != 0 {
		t.Errorf("Opt should be a non-nil zero, got %v", got.Opt)
	}
	if !got.When.Equal(time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("When=%v", got.When)
	}
	if string(got.Blob) != "raw" {
		t.Errorf("Blob=%q", got.Blob)
	}
	if got.Nested.Inner != "deep" {
		t.Errorf("nested form field not bound: %+v", got.Nested)
	}
	if got.Promoted != "up" {
		t.Errorf("embedded form field not promoted: %+v", got.formEmbedded)
	}
}

func TestFormBody_ExtendedShapesMultipart(t *testing.T) {
	m := New()
	var got formShapes
	if err := PostBody(m, "/form-shapes-mp", FormBody[formShapes](), JSON[map[string]any](),
		func(_ context.Context, b formShapes) (map[string]any, error) {
			got = b
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	// multipart 与 urlencoded 必须走同一套绑定:同样的字段名产生同样的结果。
	req := newMultipartFields(t, "/form-shapes-mp", map[string][]string{
		"tags":           {"a", "b"},
		"ids":            {"7,8"},
		"filter[status]": {"on"},
		"inner":          {"nested"},
		"promoted":       {"yes"},
	})
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !equalStrings(got.Tags, []string{"a", "b"}) {
		t.Errorf("Tags=%v", got.Tags)
	}
	if !equalInts(got.IDs, []int{7, 8}) {
		t.Errorf("IDs=%v", got.IDs)
	}
	if got.Filter["status"] != "on" {
		t.Errorf("Filter=%v", got.Filter)
	}
	if got.Nested.Inner != "nested" || got.Promoted != "yes" {
		t.Errorf("nested/embedded not bound: %+v %+v", got.Nested, got.formEmbedded)
	}
}

func TestFormBody_SliceElementError(t *testing.T) {
	m := New()
	if err := PostBody(m, "/form-bad", FormBody[formShapes](), JSON[map[string]any](),
		func(_ context.Context, b formShapes) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/form-bad", strings.NewReader("ids=1,oops"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d want 400 body=%s", rec.Code, rec.Body.String())
	}
}

// ——— 注册期拒绝不支持的形态 / registration rejects unsupported shapes ——— //

func TestNewFieldBinder_UnsupportedShapes(t *testing.T) {
	tests := []struct {
		name string
		val  any
		ok   bool
	}{
		{"scalar", int(0), true},
		{"string", "", true},
		{"pointer scalar", (*int)(nil), true},
		{"slice of scalar", []string(nil), true},
		{"array of scalar", [2]int{}, true},
		{"byte slice", []byte(nil), true},
		{"map string to scalar", map[string]int(nil), true},
		{"map string to slice", map[string][]string(nil), true},
		{"text unmarshaler", time.Time{}, true},
		{"pointer text unmarshaler", (*time.Time)(nil), true},
		// 以下形态在传输层没有公认的编码方式,注册期拒绝比运行期猜测更诚实。
		{"slice of slice", [][]string(nil), false},
		{"map of map", map[string]map[string]string(nil), false},
		{"non string key map", map[int]string(nil), false},
		{"slice of map", []map[string]string(nil), false},
		{"channel", (chan int)(nil), false},
		{"func", (func())(nil), false},
		{"plain struct", struct{ X int }{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := newFieldBinder(reflectTypeOf(tc.val))
			if ok != tc.ok {
				t.Fatalf("newFieldBinder ok=%v want %v", ok, tc.ok)
			}
		})
	}
}

func TestBindPlan_DepthCap(t *testing.T) {
	// 自引用结构体会让递归展开无限下降,注册期必须以明确错误终止,而不是栈溢出。
	// 这属于"注册期尽早暴露配置错误"原则:错误信息要指出层数上限与出错类型。
	type selfRef struct {
		Name string `query:"name"`
		Next *selfRef
	}
	_, err := buildBindPlan(reflectTypeOf(selfRef{}))
	if err == nil {
		t.Fatal("expected a depth-cap error for a self-referential params struct")
	}
	if !strings.Contains(err.Error(), "nesting exceeds") {
		t.Errorf("error should name the depth cap: %v", err)
	}

	// 有限深度的嵌套必须正常通过,证明上限只拦无限递归而非合理的分层复用。
	type lvl3 struct {
		C string `query:"c"`
	}
	type lvl2 struct {
		B string `query:"b"`
		L lvl3
	}
	type lvl1 struct {
		A string `query:"a"`
		L lvl2
	}
	plan, err := buildBindPlan(reflectTypeOf(lvl1{}))
	if err != nil {
		t.Fatalf("finite nesting should be accepted: %v", err)
	}
	if len(plan.steps) != 3 {
		t.Fatalf("expected 3 collected steps, got %d", len(plan.steps))
	}
}

// ——— splitList / expandList / collectBracketed 的直接单测 ——— //

func TestSplitList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ", []string{"a", "b"}},
		{"a,,b", []string{"a", "b"}}, // 空段跳过:尾随逗号是常见笔误,不该产出空元素
		{",", nil},
		{"a,", []string{"a"}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := splitList(tc.in)
			if !equalStrings(got, tc.want) {
				t.Fatalf("splitList(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandList(t *testing.T) {
	// 重复出现与逗号分隔混用时,展开顺序必须保持输入顺序。
	got := expandList([]string{"a,b", "c", "", "d,e"})
	if !equalStrings(got, []string{"a", "b", "c", "d", "e"}) {
		t.Fatalf("expandList=%v", got)
	}
	if expandList(nil) != nil {
		t.Fatal("expandList(nil) should stay nil")
	}
}

func TestCollectBracketed(t *testing.T) {
	values := map[string][]string{
		"filter[a]":  {"1"},
		"filter[b]":  {"2", "3"},
		"filter":     {"ignored"}, // 无方括号的同名键不属于 map
		"filter[]":   {"empty"},   // 空键跳过
		"other[c]":   {"4"},
		"filterx[d]": {"5"}, // 前缀必须完整匹配,不能前缀相似就误收
	}
	got := collectBracketed(values, "filter")
	if len(got) != 2 {
		t.Fatalf("collectBracketed=%v want 2 keys", got)
	}
	if !equalStrings(got["a"], []string{"1"}) || !equalStrings(got["b"], []string{"2", "3"}) {
		t.Fatalf("collectBracketed=%v", got)
	}
	if _, bad := got["c"]; bad {
		t.Error("must not collect another prefix's keys")
	}
	if collectBracketed(map[string][]string{"x": {"1"}}, "filter") != nil {
		t.Error("no match should yield nil, not an empty map")
	}
}

// ——— 端到端:params + body 同时使用扩展形态 ——— //

type mixedParams struct {
	ID   int      `path:"id"`
	Tags []string `query:"tags"`
}

type mixedBody struct {
	Names []string          `json:"names"`
	Attrs map[string]string `json:"attrs"`
}

func TestParamsBody_ExtendedShapes(t *testing.T) {
	m := New()
	if err := PostParamsBody(m, "/items/{id}", JSONBody[mixedBody](), JSON[map[string]any](),
		func(_ context.Context, p mixedParams, b mixedBody) (map[string]any, error) {
			return map[string]any{
				"id":    p.ID,
				"tags":  p.Tags,
				"names": b.Names,
				"attrs": b.Attrs,
			}, nil
		}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/items/42?tags=x,y",
		strings.NewReader(`{"names":["n1","n2"],"attrs":{"k":"v"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID    int               `json:"id"`
		Tags  []string          `json:"tags"`
		Names []string          `json:"names"`
		Attrs map[string]string `json:"attrs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != 42 || !equalStrings(out.Tags, []string{"x", "y"}) {
		t.Errorf("params not bound: %+v", out)
	}
	if !equalStrings(out.Names, []string{"n1", "n2"}) || out.Attrs["k"] != "v" {
		t.Errorf("body not decoded: %+v", out)
	}
}
