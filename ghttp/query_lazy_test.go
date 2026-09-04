package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ===========================================================================
// 惰性 query 测试：核心要求是与 net/url.ParseQuery 语义完全一致。
// 性能优化若改变语义就是 bug，故本文件以“对照标准库”为主线。
// Lazy query tests: the core requirement is exact semantic parity with
// net/url.ParseQuery. A performance optimization that changes semantics is a bug,
// so this file is organized around comparison against the standard library.
// ===========================================================================

// lazyQueryCases 汇集易错边界：多值、百分号转义、加号、空值、无 '='、非法转义、
// 分号作废、非 ASCII、键本身带转义。
// lazyQueryCases gathers error-prone edges: repetition, percent escapes, plus signs,
// empty values, missing '=', bad escapes, semicolon invalidation, non-ASCII, and
// escaped keys.
var lazyQueryCases = []string{
	"",
	"page=1&size=20",
	"a=1&a=2&a=3",
	"keyword=hello%20world&page=2",
	"plus=a+b",
	"empty=&x=1",
	"noval&x=1",
	"weird%3Dkey=v",
	"k=%E4%B8%AD%E6%96%87",
	"&&a=1&&",
	"a=1;b=2",
	"k=%ZZ",
	"k=%&k=1",
	"k=%",
	"=noKey&a=1",
	"a=1&a=%zz&a=3",
	"%2Fslash=v",
	"tags=a,b&tags=c",
}

var lazyQueryKeys = []string{
	"page", "size", "a", "keyword", "plus", "empty", "x", "noval",
	"weird=key", "k", "b", "", "/slash", "tags",
}

// TestLazyQueryMatchesStdlib 逐用例、逐键对照标准库,覆盖单值与多值两条路径。
func TestLazyQueryMatchesStdlib(t *testing.T) {
	for _, raw := range lazyQueryCases {
		// 标准库对非法输入返回 err 但仍产出已解析部分;ghttp 沿用其"尽力而为"语义。
		std, _ := url.ParseQuery(raw)
		for _, key := range lazyQueryKeys {
			want := std[key]

			gotAll := lazyQueryAll(raw, key, nil)
			if len(gotAll) != len(want) {
				t.Errorf("lazyQueryAll(%q, %q) = %q, stdlib = %q", raw, key, gotAll, want)
			} else {
				for i := range want {
					if gotAll[i] != want[i] {
						t.Errorf("lazyQueryAll(%q, %q)[%d] = %q, stdlib = %q", raw, key, i, gotAll[i], want[i])
					}
				}
			}

			gotOne, gotOK := lazyQueryFirst(raw, key)
			wantOne, wantOK := "", false
			if len(want) > 0 {
				wantOne, wantOK = want[0], true
			}
			if gotOK != wantOK || gotOne != wantOne {
				t.Errorf("lazyQueryFirst(%q, %q) = (%q, %v), stdlib = (%q, %v)",
					raw, key, gotOne, gotOK, wantOne, wantOK)
			}
		}
	}
}

// FuzzLazyQueryVsStdlib 用模糊测试覆盖手写用例想不到的输入。
// 手写边界总有遗漏,模糊测试是这类"重新实现标准库语义"改动的必要保险。
func FuzzLazyQueryVsStdlib(f *testing.F) {
	for _, raw := range lazyQueryCases {
		f.Add(raw, "a")
		f.Add(raw, "k")
	}
	f.Fuzz(func(t *testing.T, raw, key string) {
		std, _ := url.ParseQuery(raw)
		want := std[key]

		got := lazyQueryAll(raw, key, nil)
		if len(got) != len(want) {
			t.Fatalf("lazyQueryAll(%q, %q) = %q, stdlib = %q", raw, key, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("lazyQueryAll(%q, %q)[%d] = %q, stdlib = %q", raw, key, i, got[i], want[i])
			}
		}

		gotOne, gotOK := lazyQueryFirst(raw, key)
		if len(want) > 0 {
			if !gotOK || gotOne != want[0] {
				t.Fatalf("lazyQueryFirst(%q, %q) = (%q, %v), want %q", raw, key, gotOne, gotOK, want[0])
			}
		} else if gotOK {
			t.Fatalf("lazyQueryFirst(%q, %q) = (%q, true), want miss", raw, key, gotOne)
		}
	})
}

func TestLazyQueryKeyCount(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 0},
		{"a=1", 1},
		{"a=1&b=2", 2},
		{"a=1&b=2&c=3", 3},
		{"&", 2}, // 空项也计入:阈值只需上界估计,宁可高估而回退 / empty items count: the
		// threshold only needs an upper bound, and overestimating merely falls back
	}
	for _, c := range cases {
		if got := lazyQueryKeyCount(c.raw); got != c.want {
			t.Errorf("lazyQueryKeyCount(%q) = %d, want %d", c.raw, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 计划层：惰性适用性判定
// ---------------------------------------------------------------------------

type lazyScalarParams struct {
	ID      int64  `path:"id"`
	Page    int    `query:"page"`
	Keyword string `query:"keyword"`
}

type lazySliceParams struct {
	Tags []string `query:"tags"`
}

type lazyBracketMapParams struct {
	Filter map[string]string `query:"filter"`
}

type lazyStarMapParams struct {
	All map[string]string `query:"*"`
}

type lazyPathOnlyParams struct {
	ID int64 `path:"id"`
}

// TestBindPlanLazyEligibility 确认只有 map 形态的 query 步才禁用惰性。
func TestBindPlanLazyEligibility(t *testing.T) {
	cases := []struct {
		name      string
		typ       any
		wantLazy  bool
		wantQuery bool
	}{
		{"scalars", lazyScalarParams{}, true, true},
		{"slice", lazySliceParams{}, true, true},
		{"bracket map disables lazy", lazyBracketMapParams{}, false, true},
		{"star map disables lazy", lazyStarMapParams{}, false, true},
		{"path only", lazyPathOnlyParams{}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, err := buildBindPlan(reflectTypeOf(c.typ))
			if err != nil {
				t.Fatalf("buildBindPlan: %v", err)
			}
			if got := plan.canLazyQuery(); got != c.wantLazy {
				t.Errorf("canLazyQuery() = %v, want %v", got, c.wantLazy)
			}
			if plan.needQuery != c.wantQuery {
				t.Errorf("needQuery = %v, want %v", plan.needQuery, c.wantQuery)
			}
		})
	}
}

// TestQuerySourceFallbackAboveThreshold 确认键数超阈值时回退到映射形态,且取值仍正确。
func TestQuerySourceFallbackAboveThreshold(t *testing.T) {
	plan, err := buildBindPlan(reflectTypeOf(lazyScalarParams{}))
	if err != nil {
		t.Fatalf("buildBindPlan: %v", err)
	}

	// 阈值内：走惰性
	small := newQuerySourceFor(t, plan, "page=7&keyword=x")
	if !small.lazy {
		t.Fatal("expected lazy source below threshold")
	}
	if v, ok := small.first("page"); !ok || v != "7" {
		t.Fatalf("lazy first(page) = (%q, %v)", v, ok)
	}

	// 阈值外：回退建映射，取值语义必须一致
	var sb strings.Builder
	for i := 0; i < lazyQueryMaxKeys+5; i++ {
		if i > 0 {
			sb.WriteByte('&')
		}
		fmt.Fprintf(&sb, "k%02d=v%02d", i, i)
	}
	sb.WriteString("&page=9")
	big := newQuerySourceFor(t, plan, sb.String())
	if big.lazy {
		t.Fatal("expected map fallback above threshold")
	}
	if v, ok := big.first("page"); !ok || v != "9" {
		t.Fatalf("fallback first(page) = (%q, %v)", v, ok)
	}
}

func newQuerySourceFor(t *testing.T, plan *BindPlan, rawQuery string) querySource {
	t.Helper()
	req := &Request{Request: httptest.NewRequest(http.MethodGet, "/x?"+rawQuery, nil)}
	return newQuerySource(req, plan)
}

// ---------------------------------------------------------------------------
// 端到端等价性：惰性与映射两条路径必须产出相同的绑定结果
// ---------------------------------------------------------------------------

type lazyAllShapes struct {
	ID     int64             `path:"id"`
	Page   int               `query:"page"`
	Limit  *int              `query:"limit"`
	Tags   []string          `query:"tags"`
	Filter map[string]string `query:"filter"`
	Trace  string            `header:"X-Trace"`
}

type lazyEchoResp struct {
	ID     int64             `json:"id"`
	Page   int               `json:"page"`
	Limit  *int              `json:"limit"`
	Tags   []string          `json:"tags"`
	Filter map[string]string `json:"filter"`
	Trace  string            `json:"trace"`
}

// TestLazyQueryEndToEndEquivalence 用同一组请求分别打到"含 map 步"(强制映射)与
// "不含 map 步"(走惰性)的端点,断言两者对相同参数的解析结果一致。
func TestLazyQueryEndToEndEquivalence(t *testing.T) {
	type noMapParams struct {
		ID    int64    `path:"id"`
		Page  int      `query:"page"`
		Limit *int     `query:"limit"`
		Tags  []string `query:"tags"`
		Trace string   `header:"X-Trace"`
	}

	s := New()
	// A: 无 map 步 —— 走惰性扫描
	if err := GetParams(s, "/lazy/{id}", JSON[lazyEchoResp](),
		func(ctx context.Context, p noMapParams) (lazyEchoResp, error) {
			return lazyEchoResp{ID: p.ID, Page: p.Page, Limit: p.Limit, Tags: p.Tags, Trace: p.Trace}, nil
		}); err != nil {
		t.Fatalf("register lazy: %v", err)
	}
	// B: 含 map 步 —— 强制建映射
	if err := GetParams(s, "/mapped/{id}", JSON[lazyEchoResp](),
		func(ctx context.Context, p lazyAllShapes) (lazyEchoResp, error) {
			return lazyEchoResp{ID: p.ID, Page: p.Page, Limit: p.Limit, Tags: p.Tags, Trace: p.Trace}, nil
		}); err != nil {
		t.Fatalf("register mapped: %v", err)
	}

	queries := []string{
		"page=3",
		"page=3&limit=10",
		"page=3&tags=a&tags=b",
		"page=3&tags=a,b&tags=c",
		"page=3&keyword=hello%20world",
		"page=3&limit=0",
		"",
		"page=3&empty=",
	}
	for _, q := range queries {
		t.Run("q="+q, func(t *testing.T) {
			get := func(path string) string {
				target := path + "?" + q
				if q == "" {
					target = path
				}
				req := httptest.NewRequest(http.MethodGet, target, nil)
				req.Header.Set("X-Trace", "t-1")
				rec := httptest.NewRecorder()
				s.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
				}
				return rec.Body.String()
			}
			lazyBody := get("/lazy/42")
			mapBody := get("/mapped/42")
			if lazyBody != mapBody {
				t.Fatalf("lazy path and map path disagree:\n lazy = %s\n  map = %s", lazyBody, mapBody)
			}
		})
	}
}

// TestLazyQueryMapStepStillWorks 确认禁用惰性的 map 形态仍按原语义工作(回归保护)。
func TestLazyQueryMapStepStillWorks(t *testing.T) {
	s := New()
	if err := GetParams(s, "/m/{id}", JSON[lazyEchoResp](),
		func(ctx context.Context, p lazyAllShapes) (lazyEchoResp, error) {
			return lazyEchoResp{ID: p.ID, Filter: p.Filter, Tags: p.Tags}, nil
		}); err != nil {
		t.Fatalf("register: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/m/7?filter[status]=active&filter[kind]=b&tags=x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"status":"active"`, `"kind":"b"`, `"x"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s missing %s", body, want)
		}
	}
}
