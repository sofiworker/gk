package ghttp

import (
	"fmt"
	"strconv"
)

// Value 是单值访问器，提供类型安全的参数访问。
// Value is a single-value accessor that provides type-safe parameter access.
type Value struct {
	name   string
	raw    string
	exists bool
}

// Values 是多值访问器，用于处理数组参数（如 query string 的多值）。
// Values is a multi-value accessor for handling array parameters (e.g., multi-value query strings).
type Values struct {
	name string
	raw  []string
}

// newValue 创建 Value 实例。
// newValue creates a Value instance.
func newValue(raw string, exists bool) *Value {
	return &Value{raw: raw, exists: exists}
}

// newNamedValue 创建带参数名的 Value，参数名用于错误信息。
// newNamedValue creates a Value carrying the parameter name used in error messages.
func newNamedValue(name, raw string, exists bool) *Value {
	return &Value{name: name, raw: raw, exists: exists}
}

// missing 返回参数缺失错误，包装 ErrInvalidInput（映射为 400）。
// missing returns the missing-parameter error wrapping ErrInvalidInput (maps to 400).
func (v *Value) missing() error {
	return fmt.Errorf("%w: parameter %q is missing", ErrInvalidInput, v.name)
}

// convErr 返回类型转换错误，包装 ErrInvalidInput（映射为 400）并保留底层 strconv 错误。
// convErr returns a conversion error wrapping ErrInvalidInput (maps to 400) and keeping the
// underlying strconv error.
func convErr(name, raw, kind string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: parameter %q: cannot convert %q to %s", ErrInvalidInput, name, raw, kind)
	}
	return fmt.Errorf("%w: parameter %q: cannot convert %q to %s: %w", ErrInvalidInput, name, raw, kind, err)
}

// newValues 创建 Values 实例。
// newValues creates a Values instance.
func newValues(raw []string) *Values {
	return &Values{raw: raw}
}

// newNamedValues 创建带参数名的 Values。
// newNamedValues creates a Values carrying the parameter name.
func newNamedValues(name string, raw []string) *Values {
	return &Values{name: name, raw: raw}
}

// Exists 返回参数是否存在。
// Exists returns whether the parameter exists.
func (v *Value) Exists() bool {
	return v.exists
}

// String 返回字符串值。
// String returns the string value.
func (v *Value) String() (string, error) {
	if !v.exists {
		return "", v.missing()
	}
	return v.raw, nil
}

// Int 将参数转换为 int。
// Int converts the parameter to int.
func (v *Value) Int() (int, error) {
	if !v.exists {
		return 0, v.missing()
	}
	if v.raw == "" {
		return 0, convErr(v.name, v.raw, "int", nil)
	}
	n, err := strconv.Atoi(v.raw)
	if err != nil {
		return 0, convErr(v.name, v.raw, "int", err)
	}
	return n, nil
}

// Int64 将参数转换为 int64。
// Int64 converts the parameter to int64.
func (v *Value) Int64() (int64, error) {
	if !v.exists {
		return 0, v.missing()
	}
	if v.raw == "" {
		return 0, convErr(v.name, v.raw, "int64", nil)
	}
	n, err := strconv.ParseInt(v.raw, 10, 64)
	if err != nil {
		return 0, convErr(v.name, v.raw, "int64", err)
	}
	return n, nil
}

// Float64 将参数转换为 float64。
// Float64 converts the parameter to float64.
func (v *Value) Float64() (float64, error) {
	if !v.exists {
		return 0, v.missing()
	}
	if v.raw == "" {
		return 0, convErr(v.name, v.raw, "float64", nil)
	}
	n, err := strconv.ParseFloat(v.raw, 64)
	if err != nil {
		return 0, convErr(v.name, v.raw, "float64", err)
	}
	return n, nil
}

// Bool 将参数转换为 bool。
// Bool converts the parameter to bool.
//
// 空字符串返回 false，非空字符串按 strconv.ParseBool 规则解析。
// Empty string returns false, non-empty strings are parsed according to strconv.ParseBool rules.
func (v *Value) Bool() (bool, error) {
	if !v.exists {
		return false, v.missing()
	}
	if v.raw == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v.raw)
	if err != nil {
		return false, convErr(v.name, v.raw, "bool", err)
	}
	return b, nil
}

// IntOr 将参数转换为 int，转换失败返回默认值。
// IntOr converts the parameter to int, returning the default value on failure.
func (v *Value) IntOr(defaultValue int) int {
	val, err := v.Int()
	if err != nil {
		return defaultValue
	}
	return val
}

// Int64Or 将参数转换为 int64，转换失败返回默认值。
// Int64Or converts the parameter to int64, returning the default value on failure.
func (v *Value) Int64Or(defaultValue int64) int64 {
	val, err := v.Int64()
	if err != nil {
		return defaultValue
	}
	return val
}

// Float64Or 将参数转换为 float64，转换失败返回默认值。
// Float64Or converts the parameter to float64, returning the default value on failure.
func (v *Value) Float64Or(defaultValue float64) float64 {
	val, err := v.Float64()
	if err != nil {
		return defaultValue
	}
	return val
}

// BoolOr 将参数转换为 bool，转换失败返回默认值。
// BoolOr converts the parameter to bool, returning the default value on failure.
func (v *Value) BoolOr(defaultValue bool) bool {
	val, err := v.Bool()
	if err != nil {
		return defaultValue
	}
	return val
}

// Len 返回多值参数的长度。
// Len returns the length of multi-value parameters.
func (vs *Values) Len() int {
	return len(vs.raw)
}

// Strings 返回原始字符串切片。
// Strings returns the raw string slice.
func (vs *Values) Strings() []string {
	return vs.raw
}

// IntSlice 将所有值转换为 int 切片。
// IntSlice converts all values to an int slice.
func (vs *Values) IntSlice() ([]int, error) {
	result := make([]int, 0, len(vs.raw))
	for _, s := range vs.raw {
		val, err := strconv.Atoi(s)
		if err != nil {
			return nil, convErr(vs.name, s, "int", err)
		}
		result = append(result, val)
	}
	return result, nil
}

// Int64Slice 将所有值转换为 int64 切片。
// Int64Slice converts all values to an int64 slice.
func (vs *Values) Int64Slice() ([]int64, error) {
	result := make([]int64, 0, len(vs.raw))
	for _, s := range vs.raw {
		val, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, convErr(vs.name, s, "int64", err)
		}
		result = append(result, val)
	}
	return result, nil
}

// Float64Slice 将所有值转换为 float64 切片。
// Float64Slice converts all values to a float64 slice.
func (vs *Values) Float64Slice() ([]float64, error) {
	result := make([]float64, 0, len(vs.raw))
	for _, s := range vs.raw {
		val, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, convErr(vs.name, s, "float64", err)
		}
		result = append(result, val)
	}
	return result, nil
}

// PathValue 返回路径参数的类型安全访问器。
// PathValue returns a type-safe accessor for path parameters.
func PathValue(req *Request, name string) *Value {
	val, exists := req.Params.Lookup(name)
	return newNamedValue(name, val, exists)
}

// QueryValue 返回查询参数的类型安全访问器（单值，取第一个）。
// QueryValue returns a type-safe accessor for query parameters (single value, the first one).
func QueryValue(req *Request, name string) *Value {
	vals, exists := req.Query()[name]
	val := ""
	if len(vals) > 0 {
		val = vals[0]
	}
	return newNamedValue(name, val, exists)
}

// QueryValues 返回查询参数的多值访问器。
// QueryValues returns a multi-value accessor for query parameters.
func QueryValues(req *Request, name string) *Values {
	vals := req.Query()[name]
	if vals == nil {
		vals = []string{}
	}
	return newNamedValues(name, vals)
}

// HeaderValue 返回 HTTP 头的类型安全访问器（单值，取第一个）。存在性按头是否出现判断，
// 因此显式发送的空值头也视为存在。
// HeaderValue returns a type-safe accessor for HTTP headers (single value, the first one).
// Existence means the header is present, so an explicitly empty header still exists.
func HeaderValue(req *Request, name string) *Value {
	vals := req.Raw.Header.Values(name)
	if len(vals) == 0 {
		return newNamedValue(name, "", false)
	}
	return newNamedValue(name, vals[0], true)
}

// CookieValue 返回 Cookie 的类型安全访问器。
// CookieValue returns a type-safe accessor for cookies.
func CookieValue(req *Request, name string) *Value {
	cookie, err := req.Raw.Cookie(name)
	if err != nil {
		return newNamedValue(name, "", false)
	}
	return newNamedValue(name, cookie.Value, true)
}
