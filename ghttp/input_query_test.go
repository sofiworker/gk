package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type listQuery struct {
	Page    int
	Size    int
	Keyword string
	Desc    bool
}

// Query 组合器:多字段端到端,含 int/string/bool 混合与可选字段缺省保留零值。
func TestQueryCombinatorEndToEnd(t *testing.T) {
	m := New()
	err := Get(m, "/items",
		NoInput[NoPath](),
		Query(
			QInt("page", func(q *listQuery, v int) { q.Page = v }),
			QInt("size", func(q *listQuery, v int) { q.Size = v }),
			QStr("q", func(q *listQuery, v string) { q.Keyword = v }),
			QBool("desc", func(q *listQuery, v bool) { q.Desc = v }),
		),
		NoInput[NoBody](),
		JSON[listQuery](),
		func(ctx context.Context, in RequestInput[NoPath, listQuery, NoBody]) (listQuery, error) {
			return in.Query, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	// 全参数。
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items?page=2&size=20&q=foo&desc=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"Page":2,"Size":20,"Keyword":"foo","Desc":true}`+"\n" {
		t.Errorf("full query body = %s", got)
	}

	// 部分参数:缺省字段保留零值。
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items?page=5", nil))
	if got := rec.Body.String(); got != `{"Page":5,"Size":0,"Keyword":"","Desc":false}`+"\n" {
		t.Errorf("partial query body = %s", got)
	}
}

// Query 类型解析失败:非法 int 返回 500(阶段 4 前兜底),业务函数不执行。
func TestQueryTypeError(t *testing.T) {
	m := New()
	ran := false
	err := Get(m, "/items",
		NoInput[NoPath](),
		Query(QInt("page", func(q *listQuery, v int) { q.Page = v })),
		NoInput[NoBody](),
		JSON[listQuery](),
		func(ctx context.Context, in RequestInput[NoPath, listQuery, NoBody]) (listQuery, error) {
			ran = true
			return in.Query, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items?page=abc", nil))
	if ran {
		t.Error("handler ran despite query parse error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (pre-stage-4 fallback)", rec.Code)
	}
}

// .Required() 缺失:直接触达 bind 闭包验证 ErrMissingRequired。
func TestQueryRequiredMissing(t *testing.T) {
	src := Query(QStr("token", func(q *listQuery, v string) { q.Keyword = v }).Required())
	decode, err := src.(qsource[listQuery]).bind(&endpointSpec{})
	if err != nil {
		t.Fatal(err)
	}
	// 缺 token。
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	_, derr := decode(&Request{Request: req})
	if !errors.Is(derr, ErrMissingRequired) {
		t.Errorf("missing required: err = %v, want ErrMissingRequired", derr)
	}
	// 有 token。
	req = httptest.NewRequest(http.MethodGet, "/x?token=abc", nil)
	got, derr := decode(&Request{Request: req})
	if derr != nil {
		t.Fatalf("present required: unexpected err %v", derr)
	}
	if got.Keyword != "abc" {
		t.Errorf("present required: Keyword = %q, want abc", got.Keyword)
	}
}

// .Required() 值副本语义:标记一个字段 Required 不污染原 field。
func TestQueryRequiredIsCopy(t *testing.T) {
	base := QStr("a", func(q *listQuery, v string) { q.Keyword = v })
	req := base.Required()
	if base.required {
		t.Error("Required() mutated the original field (should return a copy)")
	}
	if !req.required {
		t.Error("Required() copy is not marked required")
	}
}

// 单值 Query 源:QueryInt 直接产出 InputSource[int],缺失返回零值。
func TestQueryScalarSource(t *testing.T) {
	m := New()
	err := Get(m, "/page",
		NoInput[NoPath](),
		QueryInt("n"),
		NoInput[NoBody](),
		JSON[int](),
		func(ctx context.Context, in RequestInput[NoPath, int, NoBody]) (int, error) {
			return in.Query, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page?n=42", nil))
	if got := rec.Body.String(); got != "42\n" {
		t.Errorf("scalar query body = %q, want 42", got)
	}
	// 缺失 → 零值 0。
	rec = httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if got := rec.Body.String(); got != "0\n" {
		t.Errorf("scalar query absent body = %q, want 0", got)
	}
}

// 注册期空名:Query 字段名为空 → bind 返回 ErrInvalidParam。
func TestQueryEmptyNameRegError(t *testing.T) {
	m := New()
	err := Get(m, "/x",
		NoInput[NoPath](),
		Query(QStr("", func(q *listQuery, v string) {})),
		NoInput[NoBody](),
		JSON[listQuery](),
		func(ctx context.Context, in RequestInput[NoPath, listQuery, NoBody]) (listQuery, error) {
			return in.Query, nil
		})
	if !errors.Is(err, ErrInvalidParam) {
		t.Errorf("empty query name: err = %v, want ErrInvalidParam", err)
	}
}

// 覆盖 QInt64/QBool 组合器 + QueryString/QueryInt64/QueryBool 单值源 + 单值解析错误。
func TestQueryFullCoverage(t *testing.T) {
	// QInt64 + QBool 组合器。
	m := New()
	err := Get(m, "/mix",
		NoInput[NoPath](),
		Query(
			QInt64("b", func(q *listQuery, v int64) { q.Page = int(v) }),
			QBool("c", func(q *listQuery, v bool) { q.Desc = v }),
		),
		NoInput[NoBody](),
		JSON[listQuery](),
		func(ctx context.Context, in RequestInput[NoPath, listQuery, NoBody]) (listQuery, error) {
			return in.Query, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mix?b=100&c=true", nil))
	if got := rec.Body.String(); got != `{"Page":100,"Size":0,"Keyword":"","Desc":true}`+"\n" {
		t.Errorf("QInt64+QBool combo body = %s", got)
	}

	// QueryString 单值源。
	m2 := New()
	if err := Get(m2, "/scalar",
		NoInput[NoPath](), QueryString("s"), NoInput[NoBody](),
		JSON[string](),
		func(ctx context.Context, in RequestInput[NoPath, string, NoBody]) (string, error) {
			return in.Query, nil
		},
	); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	m2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/scalar?s=val", nil))
	if got := rec.Body.String(); got != `"val"`+"\n" {
		t.Errorf("QueryString = %s", got)
	}

	// QueryInt64 单值源。
	m3 := New()
	if err := Get(m3, "/i64",
		NoInput[NoPath](), QueryInt64("id"), NoInput[NoBody](),
		JSON[int64](),
		func(ctx context.Context, in RequestInput[NoPath, int64, NoBody]) (int64, error) { return in.Query, nil },
	); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	m3.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i64?id=123", nil))
	if got := rec.Body.String(); got != "123\n" {
		t.Errorf("QueryInt64 = %s", got)
	}

	// QueryBool 单值源。
	m4 := New()
	if err := Get(m4, "/b",
		NoInput[NoPath](), QueryBool("f"), NoInput[NoBody](),
		JSON[bool](),
		func(ctx context.Context, in RequestInput[NoPath, bool, NoBody]) (bool, error) { return in.Query, nil },
	); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	m4.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/b?f=y", nil))
	if got := rec.Body.String(); got != "true\n" {
		t.Errorf("QueryBool = %s", got)
	}

	// QueryInt 单值源类型解析失败 → 500。
	m5 := New()
	if err := Get(m5, "/bad",
		NoInput[NoPath](), QueryInt("n"), NoInput[NoBody](),
		JSON[int](),
		func(ctx context.Context, in RequestInput[NoPath, int, NoBody]) (int, error) { return in.Query, nil },
	); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	m5.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bad?n=xx", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("scalar query parse error status = %d, want 500", rec.Code)
	}
}

// QInt64/QBool 各自的解析错误分支 + 空组合器退化为 NoInput。
func TestQueryEdgeBranches(t *testing.T) {
	// QInt64 解析错误。
	src := Query(QInt64("b", func(q *listQuery, v int64) { q.Page = int(v) }))
	decode, err := src.(qsource[listQuery]).bind(&endpointSpec{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/x?b=notnum", nil)
	if _, derr := decode(&Request{Request: req}); !errors.Is(derr, ErrInvalidInput) {
		t.Errorf("QInt64 bad: err = %v, want ErrInvalidInput", derr)
	}

	// QBool 解析错误。
	src = Query(QBool("c", func(q *listQuery, v bool) { q.Desc = v }))
	decode, err = src.(qsource[listQuery]).bind(&endpointSpec{})
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/x?c=maybe", nil)
	if _, derr := decode(&Request{Request: req}); !errors.Is(derr, ErrInvalidInput) {
		t.Errorf("QBool bad: err = %v, want ErrInvalidInput", derr)
	}

	// 空组合器退化为 NoInput[listQuery](none 类型,返回零值不报错)。
	empty := Query[listQuery]()
	if _, ok := empty.(none[listQuery]); !ok {
		t.Errorf("Query() with no fields should degrade to none[listQuery], got %T", empty)
	}
}
