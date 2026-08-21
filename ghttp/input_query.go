package ghttp

import (
	"fmt"
)

// queryField 描述一个查询参数字段:name 是键,required 标记是否必填,parse 只在值存在
// (键出现)时被调用,把原始字符串写入目标结构体 dst 的对应字段。presence/required 判断
// 统一收敛到 bind 循环,parse 不感知它,故 .Required() 值副本语义干净、可选场景零污染。
// queryField describes one query parameter: name is the key, required marks it
// mandatory, and parse is called only when the value is present (key exists) to
// write the raw string into dst's corresponding field. Presence/required logic
// is centralized in the bind loop; parse is unaware of it, so .Required()'s
// value-copy semantics stay clean and optional fields incur zero contamination.
type queryField[Q any] struct {
	name     string
	required bool
	parse    func(raw string, dst *Q) error
}

// Required 标记该字段必填 (键缺失即返回 ErrMissingRequired)。值接收器返回副本，支持
// 流式链且不影响未标记的场景。
// Required marks the field mandatory (a missing key yields ErrMissingRequired).
// Value receiver returns a copy, enabling fluent chaining without affecting
// unmarked fields.
func (f queryField[Q]) Required() queryField[Q] {
	f.required = true
	return f
}

// QStr 声明字符串查询字段,set 把解析出的值写入目标结构体。
// QStr declares a string query field; set writes the parsed value into the target.
func QStr[Q any](name string, set func(*Q, string)) queryField[Q] {
	return queryField[Q]{name: name, parse: func(raw string, dst *Q) error {
		set(dst, raw)
		return nil
	}}
}

// QInt 声明 int 查询字段，解析失败返回 400 级 ErrInvalidInput。
// QInt declares an int query field; a parse failure returns a 400-class ErrInvalidInput.
func QInt[Q any](name string, set func(*Q, int)) queryField[Q] {
	return queryField[Q]{name: name, parse: func(raw string, dst *Q) error {
		n, err := toInt("query", name, raw)
		if err != nil {
			return err
		}
		set(dst, n)
		return nil
	}}
}

// QInt64 声明 int64 查询字段。
// QInt64 declares an int64 query field.
func QInt64[Q any](name string, set func(*Q, int64)) queryField[Q] {
	return queryField[Q]{name: name, parse: func(raw string, dst *Q) error {
		n, err := toInt64("query", name, raw)
		if err != nil {
			return err
		}
		set(dst, n)
		return nil
	}}
}

// QBool 声明 bool 查询字段。
// QBool declares a bool query field.
func QBool[Q any](name string, set func(*Q, bool)) queryField[Q] {
	return queryField[Q]{name: name, parse: func(raw string, dst *Q) error {
		b, err := toBool("query", name, raw)
		if err != nil {
			return err
		}
		set(dst, b)
		return nil
	}}
}

// qsource 是多字段查询组合器编译后的 InputSource[Q]。
// qsource is the compiled InputSource[Q] for the multi-field query combinator.
type qsource[Q any] struct {
	fields []queryField[Q]
}

func (qs qsource[Q]) bind(*endpointSpec) (func(*Request) (Q, error), error) {
	for _, f := range qs.fields {
		if f.name == "" {
			return nil, ErrInvalidParam
		}
	}
	fields := qs.fields
	return func(req *Request) (Q, error) {
		var dst Q
		values := req.URL.Query()
		for _, f := range fields {
			vs, ok := values[f.name]
			if !ok || len(vs) == 0 {
				if f.required {
					return dst, fmt.Errorf("%w: query %q", ErrMissingRequired, f.name)
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

// Query 把多个字段组合成一个 InputSource[Q],目标结构体类型 Q 由调用点推断。
// 无字段时等价于空输入 (退化为 NoInput[Q])。
// Query composes multiple fields into one InputSource[Q]; the target struct type
// Q is inferred at the call site. With no fields it degrades to NoInput[Q].
func Query[Q any](fields ...queryField[Q]) InputSource[Q] {
	if len(fields) == 0 {
		return NoInput[Q]()
	}
	return qsource[Q]{fields: fields}
}

// --- 单值便捷源：只需一个查询参数时，免去包一层结构体 ---

// QueryString 声明单个字符串查询参数，直接返回 InputSource[string]。
// QueryString declares a single string query parameter, returning InputSource[string].
func QueryString(name string) InputSource[string] {
	return scalarQuery[string]{name: name, conv: func(raw string) (string, error) { return raw, nil }}
}

// QueryInt 声明单个 int 查询参数。
// QueryInt declares a single int query parameter.
func QueryInt(name string) InputSource[int] {
	return scalarQuery[int]{name: name, conv: func(raw string) (int, error) { return toInt("query", name, raw) }}
}

// QueryInt64 声明单个 int64 查询参数。
// QueryInt64 declares a single int64 query parameter.
func QueryInt64(name string) InputSource[int64] {
	return scalarQuery[int64]{name: name, conv: func(raw string) (int64, error) { return toInt64("query", name, raw) }}
}

// QueryBool 声明单个 bool 查询参数。
// QueryBool declares a single bool query parameter.
func QueryBool(name string) InputSource[bool] {
	return scalarQuery[bool]{name: name, conv: func(raw string) (bool, error) { return toBool("query", name, raw) }}
}

// scalarQuery 是单值查询源；缺失时返回类型零值 (可选语义),存在但解析失败返回错误。
// 单值源不支持 .Required()(需要必填时用 Query 组合器 + .Required());保持单值路径最简。
// scalarQuery is a single-value query source; absent yields the type's zero
// value (optional semantics), present-but-unparseable yields an error. It does
// not support .Required() (use the Query combinator + .Required() when needed),
// keeping the single-value path minimal.
type scalarQuery[T any] struct {
	name string
	conv func(raw string) (T, error)
}

func (s scalarQuery[T]) bind(*endpointSpec) (func(*Request) (T, error), error) {
	if s.name == "" {
		return nil, ErrInvalidParam
	}
	name, conv := s.name, s.conv
	return func(req *Request) (T, error) {
		var zero T
		vs, ok := req.URL.Query()[name]
		if !ok || len(vs) == 0 {
			return zero, nil // 缺失：可选零值。Absent: optional zero value.
		}
		return conv(vs[0])
	}, nil
}
