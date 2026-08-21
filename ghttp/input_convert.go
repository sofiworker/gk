package ghttp

import (
	"fmt"
	"strconv"
)

// toInt64 将 raw 解析为 int64;失败时包装 ErrInvalidInput(包含 kind+name)。
// toInt64 parses raw into int64; on failure wraps it with ErrInvalidInput
// (including kind and name).
func toInt64(kind, name string, raw string) (int64, error) {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s %q = %q is not an int64", ErrInvalidInput, kind, name, raw)
	}
	return v, nil
}

// toInt 将 raw 解析为 int;失败时包装 ErrInvalidInput(包含 kind+name)。
// strconv.Atoi 返回平台相关的 int 并在溢出时正确报错。
// toInt parses raw into int; on failure wraps it with ErrInvalidInput
// (including kind and name). strconv.Atoi returns a platform-dependent int and
// correctly reports overflow.
func toInt(kind, name string, raw string) (int, error) {
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s %q = %q is not an int", ErrInvalidInput, kind, name, raw)
	}
	return v, nil
}

// toBool 将 raw 解析为 bool("1"/"true"/"t"/"yes"/"y"/"on"→true,"false"等→false)。
// toBool parses raw into bool ("1"/"true"/"t"/"yes"/"y"/"on" → true; others false).
func toBool(kind, name string, raw string) (bool, error) {
	switch raw {
	case "1", "true", "t", "yes", "y", "on":
		return true, nil
	case "0", "false", "f", "no", "n", "off":
		return false, nil
	default:
		// 非标准真值也返回错误而非静默 false。
		// Return error for non-standard truthy values instead of silent false.
		return false, fmt.Errorf("%w: %s %q = %q is not a valid boolean", ErrInvalidInput, kind, name, raw)
	}
}
