package ghttp

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// 绑定形态测试的共享辅助:切片比较与 multipart 请求构造。
// Shared helpers for the binding-shape tests: slice comparison and multipart
// request construction.

// reflectTypeOf 返回值的反射类型,允许 nil 接口值(用 (*T)(nil) 形式传递类型)。
// reflectTypeOf returns a value's reflect type, accepting typed nil values (a type
// passed as (*T)(nil)).
func reflectTypeOf(v any) reflect.Type { return reflect.TypeOf(v) }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// newMultipartFields 构造一个只含普通表单字段(无文件)的 multipart 请求。
// newMultipartFields builds a multipart request carrying only plain form fields (no files).
func newMultipartFields(t *testing.T, target string, fields map[string][]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, values := range fields {
		for _, v := range values {
			if err := w.WriteField(name, v); err != nil {
				t.Fatalf("write field %s: %v", name, err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}
