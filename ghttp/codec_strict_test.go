package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type strictBody struct {
	A int `json:"a"`
}

// TestJSONDecode_RejectsTrailingContent 锁定首个 JSON 值之后的内容被拒。
//
// json.Decoder 是流式的,读到第一个完整值就返回,于是 `{"a":1} GARBAGE` 与
// `{"a":1}{"a":2}` 都会静默成功。后者尤其危险:请求体里有两个对象,本端按第一个处理,
// 链路上另一个同样宽松的组件可能取到第二个,两端对"这次请求是什么"理解不一致。
func TestJSONDecode_RejectsTrailingContent(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"single value", `{"a":1}`, http.StatusOK},
		{"single value with whitespace", "  {\"a\":1}\n\t ", http.StatusOK},
		{"trailing garbage", `{"a":1} GARBAGE`, http.StatusBadRequest},
		{"two objects", `{"a":1}{"a":2}`, http.StatusBadRequest},
		{"trailing array", `{"a":1}[1,2]`, http.StatusBadRequest},
		{"trailing scalar", `{"a":1} 5`, http.StatusBadRequest},
		{"empty body is accepted as zero value", ``, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if err := PostBody(s, "/x", JSONBody[strictBody](), JSON[strictBody](),
				func(_ context.Context, b strictBody) (strictBody, error) { return b, nil }); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status=%d, want %d (body=%q, resp=%s)",
					rec.Code, tc.wantStatus, tc.body, strings.TrimSpace(rec.Body.String()))
			}
			if tc.wantStatus == http.StatusBadRequest &&
				!strings.Contains(rec.Body.String(), "invalid_input") {
				t.Errorf("rejection should use the invalid_input code, got %s", rec.Body.String())
			}
		})
	}
}

// TestJSONDecode_TrailingContentIsInvalidInput 确认哨兵可供调用方分支。
func TestJSONDecode_TrailingContentIsInvalidInput(t *testing.T) {
	req := &Request{Request: httptest.NewRequest(
		http.MethodPost, "/x", strings.NewReader(`{"a":1} extra`))}
	var v strictBody
	err := jsonCodec{}.Decode(req, &v)
	if err == nil {
		t.Fatal("trailing content must be an error")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err=%v, want errors.Is(err, ErrInvalidInput)", err)
	}
}

// TestJSONDecode_EmptyBodyStaysZeroValue 锁定空体不是错误(非 GET 方法允许无体请求)。
func TestJSONDecode_EmptyBodyStaysZeroValue(t *testing.T) {
	req := &Request{Request: httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(""))}
	v := strictBody{A: 42}
	if err := (jsonCodec{}).Decode(req, &v); err != nil {
		t.Errorf("an empty body must not be an error, got %v", err)
	}
	if v.A != 42 {
		t.Errorf("an empty body must leave the target untouched, got A=%d", v.A)
	}
}
