package ghttp

import "fmt"

// --- Header 组合器：多字段到结构体 (零反射) ---

type headerField[H any] struct {
	name     string
	required bool
	parse    func(raw string, dst *H) error
}

// Required 标记该字段必填 (键缺失即返回 ErrMissingRequired)。值接收器返回副本，
// 流式链不影响可选场景。
// Required marks the field mandatory (a missing key yields ErrMissingRequired).
// Value receiver returns a copy, enabling fluent chaining without affecting
// unmarked fields.
func (f headerField[H]) Required() headerField[H] {
	f.required = true
	return f
}

// HStr 声明字符串 Header 字段。空名在 bind 期返回 ErrInvalidParam(与其它源一致,
// 不 panic)。
// HStr declares a string HTTP header field. An empty name returns
// ErrInvalidParam at bind time (consistent with other sources; no panic).
func HStr[H any](name string, set func(*H, string)) headerField[H] {
	return headerField[H]{name: name, parse: func(raw string, dst *H) error {
		set(dst, raw)
		return nil
	}}
}

// HInt/HInt64/HBool 分别用 toInt/toInt64/toBool 解析。
func HInt[H any](name string, set func(*H, int)) headerField[H] {
	return headerField[H]{name: name, parse: func(raw string, dst *H) error {
		n, err := toInt("header", name, raw)
		if err != nil {
			return err
		}
		set(dst, n)
		return nil
	}}
}

func HInt64[H any](name string, set func(*H, int64)) headerField[H] {
	return headerField[H]{name: name, parse: func(raw string, dst *H) error {
		n, err := toInt64("header", name, raw)
		if err != nil {
			return err
		}
		set(dst, n)
		return nil
	}}
}

func HBool[H any](name string, set func(*H, bool)) headerField[H] {
	return headerField[H]{name: name, parse: func(raw string, dst *H) error {
		b, err := toBool("header", name, raw)
		if err != nil {
			return err
		}
		set(dst, b)
		return nil
	}}
}

// hsource 是 Header 组合器编译后的 InputSource[H]。
// hsource is the compiled InputSource[H] for the HTTP Header combinator.
type hsource[H any] struct {
	fields []headerField[H]
}

func (hs hsource[H]) bind(*endpointSpec) (func(*Request) (H, error), error) {
	for _, f := range hs.fields {
		if f.name == "" {
			return nil, ErrInvalidParam
		}
	}
	fields := hs.fields
	return func(req *Request) (H, error) {
		var dst H
		for _, f := range fields {
			// Header.Values 会对 name 做 CanonicalMIMEHeaderKey 规范化,故大小写无关。
			// Header.Values canonicalizes name via CanonicalMIMEHeaderKey, so it
			// is case-insensitive.
			vs := req.Header.Values(f.name)
			if len(vs) == 0 {
				if f.required {
					return dst, fmt.Errorf("%w: header %q", ErrMissingRequired, f.name)
				}
				continue // 缺失且可选：保留字段零值。Absent & optional: keep zero value.
			}
			if err := f.parse(vs[0], &dst); err != nil {
				return dst, err
			}
		}
		return dst, nil
	}, nil
}

// Header 把多个 Header 字段组合成一个 InputSource[H],目标类型由调用点推断。
// Header composes multiple HTTP headers into one InputSource[H]; target type
// is inferred at the call site.
func Header[H any](fields ...headerField[H]) InputSource[H] {
	if len(fields) == 0 {
		return NoInput[H]()
	}
	return hsource[H]{fields: fields}
}

// --- 单值便捷源：只需一个 Header 时，免去包一层结构体 ---

// HeaderString 声明单个字符串 Header,直接返回 InputSource[string]。
// HeaderString declares a single string header, returning InputSource[string].
func HeaderString(name string) InputSource[string] {
	return scalarHeader[string]{name: name, conv: func(raw string) (string, error) { return raw, nil }}
}

// HeaderInt 声明单个 int Header。
// HeaderInt declares a single int header.
func HeaderInt(name string) InputSource[int] {
	return scalarHeader[int]{name: name, conv: func(raw string) (int, error) { return toInt("header", name, raw) }}
}

// HeaderInt64 声明单个 int64 Header。
// HeaderInt64 declares a single int64 header.
func HeaderInt64(name string) InputSource[int64] {
	return scalarHeader[int64]{name: name, conv: func(raw string) (int64, error) { return toInt64("header", name, raw) }}
}

// HeaderBool 声明单个 bool Header。
// HeaderBool declares a single bool header.
func HeaderBool(name string) InputSource[bool] {
	return scalarHeader[bool]{name: name, conv: func(raw string) (bool, error) { return toBool("header", name, raw) }}
}

// scalarHeader 是单值 Header 源;缺失返回类型零值(可选语义),存在但解析失败返回错误。
// 不支持 .Required()(需要必填时用 Header 组合器 + .Required())。
// scalarHeader is a single-value header source; absent yields the type's zero
// value (optional), present-but-unparseable yields an error. It does not
// support .Required() (use the Header combinator + .Required() when needed).
type scalarHeader[T any] struct {
	name string
	conv func(raw string) (T, error)
}

func (s scalarHeader[T]) bind(*endpointSpec) (func(*Request) (T, error), error) {
	if s.name == "" {
		return nil, ErrInvalidParam
	}
	name, conv := s.name, s.conv
	return func(req *Request) (T, error) {
		var zero T
		vs := req.Header.Values(name)
		if len(vs) == 0 {
			return zero, nil // 缺失：可选零值。Absent: optional zero value.
		}
		return conv(vs[0])
	}, nil
}
