package ghttp

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ===========================================================================
// 共享值绑定引擎:把「原始字符串 → 目标字段」的类型分析集中在注册期完成,请求期只
// 按已编译的 fieldBinder 赋值。params(path/query/header)与 form 请求体共用同一套
// 引擎,因此两侧的类型支持范围完全一致,不会出现"query 支持切片而 form 不支持"的分裂。
//
// 支持的目标形态:
//   - 标量:string / int* / uint* / float* / bool
//   - 指针:*T(T 为任一受支持形态);缺失时保持 nil,可据此区分"未提供"与"零值"
//   - encoding.TextUnmarshaler:含 time.Time、net.IP 等标准库类型
//   - []byte:按原始字节接收(不按数字列表解析,与 encoding/json 约定一致)
//   - 切片/数组:[]T / [N]T(T 为标量、指针或 TextUnmarshaler)
//   - 映射:map[string]T / map[string][]T(键为 string 或其命名类型)
//
// Shared value-binding engine: all "raw string → target field" type analysis
// happens at registration, and request time only assigns through the compiled
// fieldBinder. Params (path/query/header) and the form body share one engine, so
// both sides support exactly the same shapes — there is no "query supports slices
// but form does not" split.
//
// Supported target shapes: scalars (string / int* / uint* / float* / bool);
// pointers *T (T any supported shape), left nil when absent so "not provided" is
// distinguishable from "zero"; encoding.TextUnmarshaler (covering time.Time,
// net.IP, and other stdlib types); []byte received as raw bytes (not parsed as a
// number list, matching the encoding/json convention); slices/arrays []T / [N]T;
// and maps map[string]T / map[string][]T.
// ===========================================================================

// textUnmarshalerType 是 encoding.TextUnmarshaler 的反射类型,包级缓存避免重复构造。
// textUnmarshalerType is the reflect type of encoding.TextUnmarshaler, cached at
// package level to avoid repeated construction.
var textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

// maxBindDepth 限制嵌套结构体的展开深度,防御自引用类型导致的注册期无限递归。
// maxBindDepth caps nested-struct expansion depth, guarding against registration
// -time infinite recursion from self-referential types.
const maxBindDepth = 8

// valueKind 是注册期判定出的目标字段形态,请求期据它选择赋值分支,避免重复类型判断。
// valueKind is the target field shape decided at registration; request time
// switches on it instead of re-inspecting types.
type valueKind uint8

const (
	vkScalar valueKind = iota // 标量 / scalar
	vkText                    // encoding.TextUnmarshaler
	vkBytes                   // []byte
	vkSlice                   // []T / [N]T
	vkMap                     // map[string]T
)

// fieldBinder 是一个字段的已编译赋值器。elem 递归描述切片/映射的元素形态,因此
// map[string][]*int 之类的组合无需特例代码。
// fieldBinder is one field's compiled setter. elem recursively describes the
// element shape of slices/maps, so combinations like map[string][]*int need no
// special-case code.
type fieldBinder struct {
	vk valueKind
	// kind 仅 vkScalar 有意义:目标标量的 reflect.Kind。
	// kind matters only for vkScalar: the target scalar's reflect.Kind.
	kind reflect.Kind
	// ptr 表示字段本身是指针,赋值前需按需分配。
	// ptr marks the field itself as a pointer, allocated on demand before assignment.
	ptr bool
	// base 是去掉一层指针后的类型,用于 reflect.New / MakeSlice / MakeMap。
	// base is the type with one pointer level removed, used for reflect.New /
	// MakeSlice / MakeMap.
	base reflect.Type
	// elem 是切片/映射元素的绑定器(vkSlice / vkMap 时非 nil)。
	// elem is the binder for slice/map elements (non-nil for vkSlice / vkMap).
	elem *fieldBinder
	// arrayLen 为定长数组的长度;0 表示切片。
	// arrayLen is the length of a fixed-size array; 0 means a slice.
	arrayLen int
}

// newFieldBinder 在注册期分析字段类型 ft,返回其赋值器。第二个返回值为 false 表示该
// 类型不受支持,由调用方决定报错还是静默跳过。
// newFieldBinder analyzes field type ft at registration and returns its setter. A
// false second result means the type is unsupported; the caller decides whether to
// error or skip silently.
func newFieldBinder(ft reflect.Type) (*fieldBinder, bool) {
	b := &fieldBinder{base: ft}
	if ft.Kind() == reflect.Pointer {
		b.ptr = true
		b.base = ft.Elem()
	}
	// TextUnmarshaler 优先于底层 Kind 判定:net.IP 底层是 []byte、time.Time 底层是
	// 结构体,但二者都应走各自的文本解析而非按底层形态绑定。
	// TextUnmarshaler wins over the underlying Kind: net.IP is a []byte and
	// time.Time is a struct, yet both must go through their own text parsing
	// rather than being bound by their underlying shape.
	if implementsTextUnmarshaler(b.base) {
		b.vk = vkText
		return b, true
	}
	switch b.base.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.Bool:
		b.vk = vkScalar
		b.kind = b.base.Kind()
		return b, true

	case reflect.Slice:
		if b.base.Elem().Kind() == reflect.Uint8 {
			b.vk = vkBytes
			return b, true
		}
		elem, ok := newFieldBinder(b.base.Elem())
		if !ok || elem.vk == vkSlice || elem.vk == vkMap {
			return nil, false // 不支持切片的切片 / no slice of slices
		}
		b.vk, b.elem = vkSlice, elem
		return b, true

	case reflect.Array:
		elem, ok := newFieldBinder(b.base.Elem())
		if !ok || elem.vk == vkSlice || elem.vk == vkMap {
			return nil, false
		}
		b.vk, b.elem, b.arrayLen = vkSlice, elem, b.base.Len()
		return b, true

	case reflect.Map:
		if b.base.Key().Kind() != reflect.String {
			return nil, false // 仅支持 string 键 / string keys only
		}
		elem, ok := newFieldBinder(b.base.Elem())
		if !ok || elem.vk == vkMap {
			return nil, false // 不支持映射的映射 / no map of maps
		}
		b.vk, b.elem = vkMap, elem
		return b, true
	}
	return nil, false
}

// implementsTextUnmarshaler 报告 t（或 *t）是否实现 encoding.TextUnmarshaler。
// implementsTextUnmarshaler reports whether t (or *t) implements encoding.TextUnmarshaler.
func implementsTextUnmarshaler(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return t.Implements(textUnmarshalerType)
	}
	return reflect.PointerTo(t).Implements(textUnmarshalerType)
}

// isScalarLike 报告该绑定器是否接收单个原始字符串（标量 / 文本 / 字节）。
// isScalarLike reports whether this binder consumes a single raw string
// (scalar / text / bytes).
func (b *fieldBinder) isScalarLike() bool {
	return b.vk == vkScalar || b.vk == vkText || b.vk == vkBytes
}

// deref 按需为指针字段分配对象并返回可赋值的目标值。
// deref allocates a pointer field on demand and returns the assignable target.
func (b *fieldBinder) deref(fv reflect.Value) reflect.Value {
	if !b.ptr {
		return fv
	}
	if fv.IsNil() {
		fv.Set(reflect.New(b.base))
	}
	return fv.Elem()
}

// setOne 把单个原始字符串写入 fv。仅用于 scalar/text/bytes 形态。
// setOne writes a single raw string into fv, for scalar/text/bytes shapes only.
func (b *fieldBinder) setOne(fv reflect.Value, raw string, src bindSrc, name string) error {
	fv = b.deref(fv)
	switch b.vk {
	case vkText:
		tu, ok := fv.Addr().Interface().(encoding.TextUnmarshaler)
		if !ok {
			return bindErrf(src, name, "type %s cannot unmarshal text", b.base)
		}
		if err := tu.UnmarshalText([]byte(raw)); err != nil {
			return bindErrf(src, name, "%v", err)
		}
		return nil
	case vkBytes:
		fv.SetBytes([]byte(raw))
		return nil
	default:
		return setScalarKind(fv, b.kind, raw, src, name)
	}
}

// setSequence 把多个原始字符串写入切片或定长数组字段。定长数组多余的输入被丢弃、
// 不足的位保持零值;切片按输入长度精确分配。
// setSequence writes multiple raw strings into a slice or fixed-size array field.
// Extra input beyond an array's length is dropped and missing slots keep their
// zero value; a slice is allocated exactly to the input length.
func (b *fieldBinder) setSequence(fv reflect.Value, raws []string, src bindSrc, name string) error {
	fv = b.deref(fv)
	if b.arrayLen > 0 {
		n := len(raws)
		if n > b.arrayLen {
			n = b.arrayLen
		}
		for i := 0; i < n; i++ {
			if err := b.elem.setOne(fv.Index(i), raws[i], src, name); err != nil {
				return err
			}
		}
		return nil
	}
	s := reflect.MakeSlice(b.base, len(raws), len(raws))
	for i, raw := range raws {
		if err := b.elem.setOne(s.Index(i), raw, src, name); err != nil {
			return err
		}
	}
	fv.Set(s)
	return nil
}

// setMapping 把收集到的键值对写入映射字段。元素为切片时接收该键的全部值,否则取首值。
// setMapping writes collected key/value pairs into a map field. A slice element
// receives all values for the key; otherwise the first value is taken.
func (b *fieldBinder) setMapping(fv reflect.Value, kv map[string][]string, src bindSrc, name string) error {
	fv = b.deref(fv)
	keyType, elemType := b.base.Key(), b.base.Elem()
	m := reflect.MakeMapWithSize(b.base, len(kv))
	for k, vs := range kv {
		ev := reflect.New(elemType).Elem()
		if b.elem.vk == vkSlice {
			if err := b.elem.setSequence(ev, vs, src, name+"["+k+"]"); err != nil {
				return err
			}
		} else {
			if len(vs) == 0 {
				continue
			}
			if err := b.elem.setOne(ev, vs[0], src, name+"["+k+"]"); err != nil {
				return err
			}
		}
		m.SetMapIndex(reflect.ValueOf(k).Convert(keyType), ev)
	}
	fv.Set(m)
	return nil
}

// setScalarKind 把原始字符串按 kind 解析并写入字段。解析失败或越界归 ErrInvalidInput(→400)。
// setScalarKind parses raw per kind and writes it into the field. A parse failure
// or overflow maps to ErrInvalidInput (→400).
func setScalarKind(fv reflect.Value, kind reflect.Kind, raw string, src bindSrc, name string) error {
	switch kind {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return bindErrf(src, name, "%v", err)
		}
		if fv.OverflowInt(n) {
			return bindErrf(src, name, "value out of range")
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return bindErrf(src, name, "%v", err)
		}
		if fv.OverflowUint(n) {
			return bindErrf(src, name, "value out of range")
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return bindErrf(src, name, "%v", err)
		}
		if fv.OverflowFloat(f) {
			return bindErrf(src, name, "value out of range")
		}
		fv.SetFloat(f)
	case reflect.Bool:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return bindErrf(src, name, "%v", err)
		}
		fv.SetBool(v)
	}
	return nil
}

// bindErrf 构造带来源与字段名定位的绑定错误,统一包装 ErrInvalidInput 以映射 400。
// bindErrf builds a binding error located by source and field name, uniformly
// wrapping ErrInvalidInput so it maps to 400.
func bindErrf(src bindSrc, name, format string, args ...any) error {
	return fmt.Errorf("%w: %s %q: %s", ErrInvalidInput, bindSrcName(src), name, fmt.Sprintf(format, args...))
}

// splitList 按逗号切分单个原始值(OpenAPI explode=false 风格),逐段去空白并跳过空段。
// 空串返回 nil,使 "?tags=" 表现为"未提供元素"而非一个空元素;"a,,b" 与 "a," 里的空段
// 同样被跳过——尾随逗号是常见笔误,产出空元素只会让下游把它当成一个真实值去解析。
// splitList splits one raw value on commas (OpenAPI explode=false style), trimming
// each part and skipping empty ones. An empty string returns nil so "?tags="
// behaves as "no elements provided" rather than one empty element; empty segments in
// "a,,b" and "a," are skipped too — a trailing comma is a common typo, and emitting
// an empty element would only make downstream parse it as a real value.
func splitList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// expandList 把多个原始值按逗号展开并拼接,使 "?a=1,2&a=3" 得到 [1 2 3]。
// 全为空段时返回 nil,与 splitList 的"未提供"语义保持一致。
// expandList comma-expands multiple raw values and concatenates them, so
// "?a=1,2&a=3" yields [1 2 3]. It returns nil when every segment was empty,
// consistent with splitList's "not provided" semantics.
func expandList(raws []string) []string {
	if len(raws) == 0 {
		return nil
	}
	out := make([]string, 0, len(raws))
	for _, raw := range raws {
		out = append(out, splitList(raw)...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectBracketed 从 values 中收集 prefix["key"] 形态的键,返回 key→values。prefix
// 为 "*" 时收集全部键(原样保留键名)。无匹配返回 nil,使目标映射字段保持 nil 以表达"未提供"。
// collectBracketed collects prefix["key"]-shaped keys from values, returning
// key→values. A "*" prefix collects every key verbatim. No match returns nil so
// the target map field stays nil, expressing "not provided".
func collectBracketed(values map[string][]string, prefix string) map[string][]string {
	if prefix == bindMapAll {
		if len(values) == 0 {
			return nil
		}
		return values
	}
	var out map[string][]string
	open := prefix + "["
	for k, vs := range values {
		if len(k) <= len(open) || !strings.HasPrefix(k, open) || k[len(k)-1] != ']' {
			continue
		}
		key := k[len(open) : len(k)-1]
		if key == "" {
			// "filter[]" 是"追加到列表"的表单惯例,不是映射键;收进来会产生一个空键条目。
			// "filter[]" is the form convention for "append to a list", not a map
			// key; collecting it would produce an entry under an empty key.
			continue
		}
		if out == nil {
			out = make(map[string][]string, 4)
		}
		out[key] = vs
	}
	return out
}
