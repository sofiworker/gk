package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// typeOf 返回值的反射类型,供注册期计划构建测试使用。
// typeOf returns the value's reflect type for registration-time plan tests.
func typeOf(v any) reflect.Type { return reflect.TypeOf(v) }

// needQuery 的契约:计划含 query 步时必须解析 query;纯 path/header 计划跳过
// url.ParseQuery 但绑定行为不变。本测试固化两侧行为,防止该优化被误删或误判。
// The needQuery contract: a plan with query steps must parse the query; a pure
// path/header plan skips url.ParseQuery with identical binding behavior. This
// test pins both sides so the optimization is neither dropped nor misjudged.
func TestBindPlan_NeedQuery(t *testing.T) {
	t.Run("flag reflects plan shape", func(t *testing.T) {
		type pathOnly struct {
			ID string `path:"id"`
		}
		type withQuery struct {
			ID   string `path:"id"`
			Page int    `query:"page"`
		}
		type headerOnly struct {
			Token string `header:"X-Token"`
		}
		for _, tc := range []struct {
			name string
			typ  any
			want bool
		}{
			{"path only", pathOnly{}, false},
			{"with query", withQuery{}, true},
			{"header only", headerOnly{}, false},
		} {
			plan, err := buildBindPlan(typeOf(tc.typ))
			if err != nil {
				t.Fatalf("%s: buildBindPlan: %v", tc.name, err)
			}
			if plan.needQuery != tc.want {
				t.Errorf("%s: needQuery=%v, want %v", tc.name, plan.needQuery, tc.want)
			}
		}
	})

	t.Run("pure path endpoint binds without query parsing", func(t *testing.T) {
		s := New()
		var gotID string
		if err := GetParams(s, "/users/{id}", JSON[string](), func(ctx context.Context, p struct {
			ID string `path:"id"`
		}) (string, error) {
			gotID = p.ID
			return "ok", nil
		}); err != nil {
			t.Fatal(err)
		}
		// 带 query 串请求纯 path 端点:query 被忽略,path 绑定正常。
		// Request a pure-path endpoint with a query string: query ignored, path binds.
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/42?page=9", nil))
		if w.Code != http.StatusOK || gotID != "42" {
			t.Errorf("pure-path binding broken: status=%d id=%q", w.Code, gotID)
		}
	})

	t.Run("query endpoint still binds query", func(t *testing.T) {
		s := New()
		var gotPage int
		if err := GetParams(s, "/list", JSON[string](), func(ctx context.Context, p struct {
			Page int `query:"page"`
		}) (string, error) {
			gotPage = p.Page
			return "ok", nil
		}); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/list?page=7", nil))
		if w.Code != http.StatusOK || gotPage != 7 {
			t.Errorf("query binding broken: status=%d page=%d", w.Code, gotPage)
		}
	})

	t.Run("header endpoint binds without query parsing", func(t *testing.T) {
		s := New()
		var gotToken string
		if err := GetParams(s, "/auth", JSON[string](), func(ctx context.Context, p struct {
			Token string `header:"X-Token"`
		}) (string, error) {
			gotToken = p.Token
			return "ok", nil
		}); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/auth?noise=1", nil)
		r.Header.Set("X-Token", "abc")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusOK || gotToken != "abc" {
			t.Errorf("header binding broken: status=%d token=%q", w.Code, gotToken)
		}
	})
}
