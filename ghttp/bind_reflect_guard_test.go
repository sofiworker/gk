package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// --- 未导出内嵌指针 --- //

type fixReviewHidden struct {
	Secret string `query:"secret"`
}

type fixReviewEmbedPtrParams struct {
	*fixReviewHidden
	Plain string `query:"plain"`
}

type fixReviewEmbedValParams struct {
	fixReviewHidden
	Plain string `query:"plain"`
}

// TestBindParams_UnexportedEmbeddedPointerRejectedAtRegistration 锁定 P0-1：未导出内嵌
// 【指针】结构体必须在注册期被拒（它请求期要 v.Set 分配，对未导出字段 reflect 会 panic），
// 而内嵌【值】形态仍可正常绑定（flagEmbedRO 非粘性，提升字段可写）。
// Locks P0-1: an unexported embedded POINTER struct must be rejected at registration
// (request time must v.Set to allocate it, and reflect panics on an unexported field),
// while an embedded VALUE still binds (flagEmbedRO is not sticky).
func TestBindParams_UnexportedEmbeddedPointerRejectedAtRegistration(t *testing.T) {
	t.Parallel()

	s := New()
	err := GetParams[fixReviewEmbedPtrParams, string](s, "/e", JSON[string](),
		func(_ context.Context, p fixReviewEmbedPtrParams) (string, error) { return p.Plain, nil })
	if err == nil {
		t.Fatal("want a registration error for an unexported embedded pointer, got nil")
	}
	if !strings.Contains(err.Error(), "embedded unexported pointer") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestBindParams_UnexportedEmbeddedValueStillBinds(t *testing.T) {
	t.Parallel()

	s := New()
	if err := GetParams[fixReviewEmbedValParams, string](s, "/e", JSON[string](),
		func(_ context.Context, p fixReviewEmbedValParams) (string, error) {
			return p.Secret + "|" + p.Plain, nil
		}); err != nil {
		t.Fatalf("embedded value form must stay registrable: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/e?secret=s&plain=p", nil))
	if got := decodeJSONString(t, rec); got != "s|p" {
		t.Fatalf("got %d %q, want 200 s|p", rec.Code, rec.Body.String())
	}
}

// --- [0]T 零长度数组 --- //

type fixReviewZeroArrParams struct {
	Fixed [0]int `query:"fixed"`
}

type fixReviewArrParams struct {
	Fixed [2]int `query:"fixed"`
}

// TestBindParams_ZeroLengthArrayRejectedAtRegistration 锁定 P0-2：`[0]T` 必须在注册期
// 被拒——否则请求期落入切片分支对数组类型 MakeSlice，直接 panic 成 500。
// Locks P0-2: `[0]T` must be rejected at registration; otherwise request time takes
// the slice branch and calls MakeSlice on an array type, panicking into a 500.
func TestBindParams_ZeroLengthArrayRejectedAtRegistration(t *testing.T) {
	t.Parallel()

	s := New()
	err := GetParams[fixReviewZeroArrParams, string](s, "/z", JSON[string](),
		func(_ context.Context, p fixReviewZeroArrParams) (string, error) { return "ok", nil })
	if err == nil {
		t.Fatal("want a registration error for [0]int, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported bind type") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestBindParams_FixedLengthArrayStillBinds(t *testing.T) {
	t.Parallel()

	s := New()
	if err := GetParams[fixReviewArrParams, string](s, "/z", JSON[string](),
		func(_ context.Context, p fixReviewArrParams) (string, error) {
			return strings.Join([]string{strconv.Itoa(p.Fixed[0]), strconv.Itoa(p.Fixed[1])}, ","), nil
		}); err != nil {
		t.Fatalf("[2]int must stay registrable: %v", err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/z?fixed=7&fixed=9", nil))
	if got := decodeJSONString(t, rec); got != "7,9" {
		t.Fatalf("got %d %q, want 200 7,9", rec.Code, rec.Body.String())
	}
}

// decodeJSONString 解出 JSON 字符串响应的值，避免断言里手搓引号与换行。
// decodeJSONString unwraps a JSON string response so assertions need no quoting.
func decodeJSONString(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var v string
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	return v
}
