package v3

import (
	"errors"
	"strconv"
	"strings"
)

// Value 是类型化的值访问器,用于 path/query/header/cookie 参数。
// Value is a typed accessor for path/query/header/cookie parameters.
type Value struct {
	raw string
	ok  bool // 标记值是否存在
}

// String 返回原始字符串值。
// String returns the raw string value.
func (v *Value) String() string {
	return v.raw
}

// StringOr 返回字符串值,如果不存在或为空则返回默认值。
// StringOr returns the string value, or the default if absent or empty.
func (v *Value) StringOr(def string) string {
	if !v.ok || v.raw == "" {
		return def
	}
	return v.raw
}

// Int 返回解析后的 int 值。
// Int returns the parsed int value.
func (v *Value) Int() (int, error) {
	if !v.ok {
		return 0, ErrMissingParameter
	}
	i, err := strconv.ParseInt(v.raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return int(i), nil
}

// IntOr 返回解析后的 int 值,如果解析失败则返回默认值。
// IntOr returns the parsed int value, or the default if parsing fails.
func (v *Value) IntOr(def int) int {
	i, err := v.Int()
	if err != nil {
		return def
	}
	return i
}

// Int64 返回解析后的 int64 值。
// Int64 returns the parsed int64 value.
func (v *Value) Int64() (int64, error) {
	if !v.ok {
		return 0, ErrMissingParameter
	}
	return strconv.ParseInt(v.raw, 10, 64)
}

// Int64Or 返回解析后的 int64 值,如果解析失败则返回默认值。
// Int64Or returns the parsed int64 value, or the default if parsing fails.
func (v *Value) Int64Or(def int64) int64 {
	i, err := v.Int64()
	if err != nil {
		return def
	}
	return i
}

// Int32 返回解析后的 int32 值。
// Int32 returns the parsed int32 value.
func (v *Value) Int32() (int32, error) {
	if !v.ok {
		return 0, ErrMissingParameter
	}
	i, err := strconv.ParseInt(v.raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(i), nil
}

// Int32Or 返回解析后的 int32 值,如果解析失败则返回默认值。
// Int32Or returns the parsed int32 value, or the default if parsing fails.
func (v *Value) Int32Or(def int32) int32 {
	i, err := v.Int32()
	if err != nil {
		return def
	}
	return i
}

// Uint 返回解析后的 uint 值。
// Uint returns the parsed uint value.
func (v *Value) Uint() (uint, error) {
	if !v.ok {
		return 0, ErrMissingParameter
	}
	i, err := strconv.ParseUint(v.raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(i), nil
}

// UintOr 返回解析后的 uint 值,如果解析失败则返回默认值。
// UintOr returns the parsed uint value, or the default if parsing fails.
func (v *Value) UintOr(def uint) uint {
	i, err := v.Uint()
	if err != nil {
		return def
	}
	return i
}

// Uint64 返回解析后的 uint64 值。
// Uint64 returns the parsed uint64 value.
func (v *Value) Uint64() (uint64, error) {
	if !v.ok {
		return 0, ErrMissingParameter
	}
	return strconv.ParseUint(v.raw, 10, 64)
}

// Uint64Or 返回解析后的 uint64 值,如果解析失败则返回默认值。
// Uint64Or returns the parsed uint64 value, or the default if parsing fails.
func (v *Value) Uint64Or(def uint64) uint64 {
	i, err := v.Uint64()
	if err != nil {
		return def
	}
	return i
}

// Bool 返回解析后的 bool 值。
// Bool returns the parsed bool value.
// 支持: "1", "t", "T", "true", "TRUE", "True", "0", "f", "F", "false", "FALSE", "False"
func (v *Value) Bool() (bool, error) {
	if !v.ok {
		return false, ErrMissingParameter
	}
	return strconv.ParseBool(v.raw)
}

// BoolOr 返回解析后的 bool 值,如果解析失败则返回默认值。
// BoolOr returns the parsed bool value, or the default if parsing fails.
func (v *Value) BoolOr(def bool) bool {
	b, err := v.Bool()
	if err != nil {
		return def
	}
	return b
}

// Float64 返回解析后的 float64 值。
// Float64 returns the parsed float64 value.
func (v *Value) Float64() (float64, error) {
	if !v.ok {
		return 0, ErrMissingParameter
	}
	return strconv.ParseFloat(v.raw, 64)
}

// Float64Or 返回解析后的 float64 值,如果解析失败则返回默认值。
// Float64Or returns the parsed float64 value, or the default if parsing fails.
func (v *Value) Float64Or(def float64) float64 {
	f, err := v.Float64()
	if err != nil {
		return def
	}
	return f
}

// Float32 返回解析后的 float32 值。
// Float32 returns the parsed float32 value.
func (v *Value) Float32() (float32, error) {
	if !v.ok {
		return 0, ErrMissingParameter
	}
	f, err := strconv.ParseFloat(v.raw, 32)
	if err != nil {
		return 0, err
	}
	return float32(f), nil
}

// Float32Or 返回解析后的 float32 值,如果解析失败则返回默认值。
// Float32Or returns the parsed float32 value, or the default if parsing fails.
func (v *Value) Float32Or(def float32) float32 {
	f, err := v.Float32()
	if err != nil {
		return def
	}
	return f
}

// Exists 返回值是否存在。
// Exists returns whether the value exists.
func (v *Value) Exists() bool {
	return v.ok
}

// IsEmpty 返回值是否为空字符串(存在但为空)。
// IsEmpty returns whether the value is an empty string (exists but empty).
func (v *Value) IsEmpty() bool {
	return v.ok && v.raw == ""
}

// Values 是多值访问器,用于 query 参数的多值场景。
// Values is a multi-value accessor for query parameters with multiple values.
type Values struct {
	raw []string
}

// Strings 返回所有字符串值。
// Strings returns all string values.
func (vs *Values) Strings() []string {
	return vs.raw
}

// Ints 返回所有解析后的 int 值,跳过解析失败的值。
// Ints returns all parsed int values, skipping failed ones.
func (vs *Values) Ints() []int {
	result := make([]int, 0, len(vs.raw))
	for _, s := range vs.raw {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			result = append(result, int(i))
		}
	}
	return result
}

// Int64s 返回所有解析后的 int64 值,跳过解析失败的值。
// Int64s returns all parsed int64 values, skipping failed ones.
func (vs *Values) Int64s() []int64 {
	result := make([]int64, 0, len(vs.raw))
	for _, s := range vs.raw {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			result = append(result, i)
		}
	}
	return result
}

// First 返回第一个值的访问器。
// First returns the accessor for the first value.
func (vs *Values) First() *Value {
	if len(vs.raw) == 0 {
		return &Value{ok: false}
	}
	return &Value{raw: vs.raw[0], ok: true}
}

// Join 使用分隔符连接所有值。
// Join joins all values with the separator.
func (vs *Values) Join(sep string) string {
	return strings.Join(vs.raw, sep)
}

// Len 返回值的数量。
// Len returns the number of values.
func (vs *Values) Len() int {
	return len(vs.raw)
}

// ErrMissingParameter 表示参数不存在。
// ErrMissingParameter indicates the parameter is missing.
var ErrMissingParameter = HTTPError{Status: 400, Cause: errors.New("missing required parameter")}
