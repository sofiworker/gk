package ghttp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// ——— 扩展标量类型绑定 ———

type scalarParams struct {
	I8  int8    `query:"i8"`
	U16 uint16  `query:"u16"`
	F64 float64 `query:"f64"`
	U   uint    `query:"u"`
}

func TestBindExtendedScalarKinds(t *testing.T) {
	s := New()
	var got scalarParams
	GetParams(s, "/scalar", JSON[scalarParams](), func(_ context.Context, p scalarParams) (scalarParams, error) {
		got = p
		return p, nil
	})
	r := httptest.NewRequest(http.MethodGet, "/scalar?i8=-12&u16=6553&f64=3.14&u=42", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if got.I8 != -12 || got.U16 != 6553 || got.F64 != 3.14 || got.U != 42 {
		t.Errorf("bound wrong: %+v", got)
	}
}

func TestBindIntOverflowRejected(t *testing.T) {
	s := New()
	GetParams(s, "/ov", JSON[scalarParams](), func(_ context.Context, p scalarParams) (scalarParams, error) {
		return p, nil
	})
	// int8 放不下 999,应 400 而非静默截断
	r := httptest.NewRequest(http.MethodGet, "/ov?i8=999", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("overflow: status %d want 400 (body %s)", w.Code, w.Body.String())
	}
}

// ——— validate tag 规则 ———

type validatedParams struct {
	Name string `query:"name" validate:"required,min=2,max=10"`
	Age  int    `query:"age" validate:"min=0,max=150"`
	Role string `query:"role" validate:"oneof=admin user guest"`
}

func TestValidateTagRules(t *testing.T) {
	s := New()
	GetParams(s, "/v", JSON[validatedParams](), func(_ context.Context, p validatedParams) (validatedParams, error) {
		return p, nil
	})
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"all valid", "name=bob&age=30&role=admin", 200},
		{"missing required name", "age=30&role=user", 400},
		{"name too short", "name=a&age=30&role=user", 400},
		{"name too long", "name=abcdefghijk&age=30&role=user", 400},
		{"age over max", "name=bob&age=200&role=user", 400},
		{"role not in oneof", "name=bob&age=30&role=root", 400},
		{"age negative", "name=bob&age=-5&role=user", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/v?"+tt.query, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Errorf("query %q: status %d want %d (body %s)", tt.query, w.Code, tt.want, w.Body.String())
			}
		})
	}
}

type emailLenParams struct {
	Email string `query:"email" validate:"email"`
	Code  string `query:"code" validate:"len=4"`
}

func TestValidateEmailAndLen(t *testing.T) {
	s := New()
	GetParams(s, "/el", JSON[emailLenParams](), func(_ context.Context, p emailLenParams) (emailLenParams, error) {
		return p, nil
	})
	tests := []struct {
		query string
		want  int
	}{
		{"email=a@b.com&code=1234", 200},
		{"email=notanemail&code=1234", 400},
		{"email=a@b.com&code=12", 400},
		{"email=a@@b.com&code=1234", 400},
		{"email=a@bcom&code=1234", 400},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, "/el?"+tt.query, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("query %q: status %d want %d", tt.query, w.Code, tt.want)
		}
	}
}

// TestValidateBadRuleRejectedAtRegistration 验证非法 validate tag 在注册期报错。
func TestValidateBadRuleRejectedAtRegistration(t *testing.T) {
	type badTag struct {
		X int `query:"x" validate:"nonexistent"`
	}
	s := New()
	err := GetParams(s, "/bad", JSON[badTag](), func(_ context.Context, p badTag) (badTag, error) {
		return p, nil
	})
	if err == nil {
		t.Fatal("expected registration error for unknown validate rule")
	}
	if !errors.Is(err, ErrInvalidParam) {
		t.Errorf("err %v, want ErrInvalidParam", err)
	}
}

// ——— Validator 接口 ———

type bodyWithValidate struct {
	Amount int `json:"amount"`
}

func (b bodyWithValidate) Validate() error {
	if b.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	return nil
}

func TestBodyValidatorInterface(t *testing.T) {
	s := New()
	PostBody(s, "/order", JSONBody(), JSON[bodyWithValidate](), func(_ context.Context, b bodyWithValidate) (bodyWithValidate, error) {
		return b, nil
	})

	// 合法
	r := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(`{"amount":5}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("valid body: status %d want 200 (%s)", w.Code, w.Body.String())
	}

	// 非法 → 400 ErrValidation
	r = httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(`{"amount":-1}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid body: status %d want 400 (%s)", w.Code, w.Body.String())
	}
}

// bodyValidatePtr 用指针接收者实现 Validator。
type bodyValidatePtr struct {
	Name string `json:"name"`
}

func (b *bodyValidatePtr) Validate() error {
	if b.Name == "" {
		return errors.New("name required")
	}
	return nil
}

func TestBodyValidatorPointerReceiver(t *testing.T) {
	s := New()
	PostBody(s, "/p", JSONBody(), JSON[bodyValidatePtr](), func(_ context.Context, b bodyValidatePtr) (bodyValidatePtr, error) {
		return b, nil
	})
	r := httptest.NewRequest(http.MethodPost, "/p", strings.NewReader(`{"name":""}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("ptr-receiver validator: status %d want 400 (%s)", w.Code, w.Body.String())
	}
}

// bodyValidateStatus 的 Validate 返回自带状态码的 error(StatusCoder),应透传该码。
type bodyValidateStatus struct {
	V int `json:"v"`
}

func (b bodyValidateStatus) Validate() error {
	if b.V == 0 {
		return statusError(http.StatusConflict)
	}
	return nil
}

func TestBodyValidatorStatusCoderPassThrough(t *testing.T) {
	s := New()
	PostBody(s, "/sc", JSONBody(), JSON[bodyValidateStatus](), func(_ context.Context, b bodyValidateStatus) (bodyValidateStatus, error) {
		return b, nil
	})
	r := httptest.NewRequest(http.MethodPost, "/sc", bytes.NewReader([]byte(`{"v":0}`)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("StatusCoder validate: status %d want 409 (%s)", w.Code, w.Body.String())
	}
}

// TestCompileFieldRulesUnits 直接单测规则编译器的边界。
func TestCompileFieldRulesUnits(t *testing.T) {
	// email 只能用于 string
	if _, _, err := compileFieldRules("email", reflect.Int); err == nil {
		t.Error("email on int should error")
	}
	// oneof 缺候选
	if _, _, err := compileFieldRules("oneof", reflect.String); err == nil {
		t.Error("oneof without candidates should error")
	}
	// required 单独存在,rules 为空
	req, rules, err := compileFieldRules("required", reflect.String)
	if err != nil || !req || len(rules) != 0 {
		t.Errorf("required-only: req=%v rules=%d err=%v", req, len(rules), err)
	}
}
