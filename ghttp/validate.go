package ghttp

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ===========================================================================
// 声明式字段校验 / Declarative field validation
//
// params 结构体字段可加 `validate:"..."` tag,规则在【注册期】编译为闭包(compileFieldRules),
// 请求期只执行闭包、零 tag 解析开销。支持的规则子集(零第三方依赖,自实现):
//   required      必填(缺失即 ErrValidation);在绑定层按字段是否出现判定,不入闭包
//   min=N / max=N  数值:比较大小;字符串:比较 utf8 长度(与 gin/validator 语义一致)
//   len=N         字符串长度恰为 N;数值等于 N
//   oneof=a b c   值必须是空格分隔候选之一(数值/字符串均可)
//   email         字符串须形如 x@y.z 的简化邮箱(不做完整 RFC 5322)
// 违反规则统一归 ErrValidation(→400)。
//
// A params struct field may carry a `validate:"..."` tag; rules compile to
// closures at REGISTRATION (compileFieldRules), so request time only runs the
// closures with zero tag-parsing cost. Supported subset (zero third-party deps,
// self-implemented): required, min/max, len, oneof, email. Violations map to
// ErrValidation (→400).
// ===========================================================================

// Validator 是请求体自校验接口:body 类型实现 Validate() 后,解码成功即自动调用,返回
// 非 nil error 视为校验失败。框架把它归一到 ErrValidation(→400),除非该 error 已实现
// StatusCoder 自带状态码。这让复杂的跨字段/业务校验用普通 Go 代码表达,补 tag 之不足。
// Validator is the request-body self-validation interface: once a body type
// implements Validate(), it is called automatically after a successful decode; a
// non-nil error is a validation failure. The framework normalizes it to
// ErrValidation (→400) unless the error already implements StatusCoder. This
// expresses complex cross-field/business validation in plain Go, complementing
// tags.
type Validator interface {
	Validate() error
}

// fieldRule 是对已赋值字段的一条校验;返回非 nil error 即校验失败(消息英文,面向用户)。
// fieldRule validates an already-set field; a non-nil error means failure
// (English message, user-facing).
type fieldRule func(fv reflect.Value) error

// bodyValidatorFor 在注册期判定 body 类型 B 是否实现 Validator(指针或值接收者),返回一个
// 请求期直接调用的校验闭包;若 B 不实现,返回 nil,请求期据此完全跳过校验,零反射零分配。
// 这把"是否实现"的判定从每请求前移到注册一次——不用校验的 body 端点回到零额外成本。
// bodyValidatorFor decides at registration whether body type B implements
// Validator (pointer or value receiver) and returns a closure the request path
// calls directly; if B does not, it returns nil so the request path skips
// validation entirely with zero reflection and zero allocation. This hoists the
// implements-check from per-request to once, restoring zero extra cost for body
// endpoints that don't validate.
func bodyValidatorFor[B any]() func(*B) error {
	// *B 实现 Validator?(指针接收者,最常见)
	// Does *B implement Validator? (pointer receiver, most common)
	if _, ok := any((*B)(nil)).(Validator); ok {
		return func(b *B) error { return runValidate(any(b).(Validator)) }
	}
	// B 实现 Validator?(值接收者)
	// Does B implement Validator? (value receiver)
	var zero B
	if _, ok := any(zero).(Validator); ok {
		return func(b *B) error { return runValidate(any(*b).(Validator)) }
	}
	return nil
}

// runValidate 调用 v.Validate() 并归一化错误:自带 StatusCoder 的原样透传,否则包裹为
// ErrValidation(→400)。
// runValidate calls v.Validate() and normalizes the error: a StatusCoder passes
// through, otherwise it is wrapped as ErrValidation (→400).
func runValidate(v Validator) error {
	err := v.Validate()
	if err == nil {
		return nil
	}
	var sc StatusCoder
	if errors.As(err, &sc) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrValidation, err)
}

// compileFieldRules 解析 validate tag 为 required 标志与规则闭包列表。tag 为空则空规则。
// 非法规则或与字段 kind 不匹配的规则在注册期报错,尽早暴露。
// compileFieldRules parses a validate tag into a required flag and a list of rule
// closures. An empty tag yields no rules. Illegal rules, or rules incompatible
// with the field kind, error at registration to surface early.
func compileFieldRules(tag string, kind reflect.Kind) (required bool, rules []fieldRule, err error) {
	if tag == "" {
		return false, nil, nil
	}
	for _, part := range strings.Split(tag, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, arg, hasArg := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		arg = strings.TrimSpace(arg)
		switch name {
		case "required":
			required = true
		case "min":
			r, e := boundRule(kind, arg, true)
			if e != nil {
				return false, nil, e
			}
			rules = append(rules, r)
		case "max":
			r, e := boundRule(kind, arg, false)
			if e != nil {
				return false, nil, e
			}
			rules = append(rules, r)
		case "len":
			r, e := lenRule(kind, arg)
			if e != nil {
				return false, nil, e
			}
			rules = append(rules, r)
		case "oneof":
			r, e := oneofRule(kind, arg, hasArg)
			if e != nil {
				return false, nil, e
			}
			rules = append(rules, r)
		case "email":
			if kind != reflect.String {
				return false, nil, fmt.Errorf("rule %q only applies to string fields", name)
			}
			rules = append(rules, emailRule)
		default:
			return false, nil, fmt.Errorf("unknown validate rule %q", name)
		}
	}
	return required, rules, nil
}

// isIntKind / isUintKind / isFloatKind 归类数值 kind,供规则按类别取值比较。
// isIntKind / isUintKind / isFloatKind classify numeric kinds for value access.
func isIntKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}

func isUintKind(k reflect.Kind) bool {
	switch k {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func isFloatKind(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}

// boundRule 构造 min(isMin=true)或 max 规则:数值比较大小,字符串比较长度(rune 数)。
// boundRule builds a min (isMin=true) or max rule: numeric compares magnitude,
// string compares length (rune count).
func boundRule(kind reflect.Kind, arg string, isMin bool) (fieldRule, error) {
	label := "max"
	if isMin {
		label = "min"
	}
	switch {
	case kind == reflect.String:
		n, err := strconv.Atoi(arg)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("rule %s needs a non-negative integer, got %q", label, arg)
		}
		return func(fv reflect.Value) error {
			l := len([]rune(fv.String()))
			if isMin && l < n {
				return fmt.Errorf("length must be at least %d", n)
			}
			if !isMin && l > n {
				return fmt.Errorf("length must be at most %d", n)
			}
			return nil
		}, nil
	case isIntKind(kind):
		n, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rule %s needs an integer, got %q", label, arg)
		}
		return func(fv reflect.Value) error {
			v := fv.Int()
			if isMin && v < n {
				return fmt.Errorf("must be at least %d", n)
			}
			if !isMin && v > n {
				return fmt.Errorf("must be at most %d", n)
			}
			return nil
		}, nil
	case isUintKind(kind):
		n, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rule %s needs an unsigned integer, got %q", label, arg)
		}
		return func(fv reflect.Value) error {
			v := fv.Uint()
			if isMin && v < n {
				return fmt.Errorf("must be at least %d", n)
			}
			if !isMin && v > n {
				return fmt.Errorf("must be at most %d", n)
			}
			return nil
		}, nil
	case isFloatKind(kind):
		n, err := strconv.ParseFloat(arg, 64)
		if err != nil {
			return nil, fmt.Errorf("rule %s needs a number, got %q", label, arg)
		}
		return func(fv reflect.Value) error {
			v := fv.Float()
			if isMin && v < n {
				return fmt.Errorf("must be at least %v", n)
			}
			if !isMin && v > n {
				return fmt.Errorf("must be at most %v", n)
			}
			return nil
		}, nil
	}
	return nil, fmt.Errorf("rule %s unsupported for kind %s", label, kind)
}

// lenRule 构造 len 规则:字符串 rune 数恰为 N,数值恰等于 N。
// lenRule builds a len rule: string rune count exactly N, numeric exactly N.
func lenRule(kind reflect.Kind, arg string) (fieldRule, error) {
	switch {
	case kind == reflect.String:
		n, err := strconv.Atoi(arg)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("rule len needs a non-negative integer, got %q", arg)
		}
		return func(fv reflect.Value) error {
			if l := len([]rune(fv.String())); l != n {
				return fmt.Errorf("length must be exactly %d", n)
			}
			return nil
		}, nil
	case isIntKind(kind):
		n, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rule len needs an integer, got %q", arg)
		}
		return func(fv reflect.Value) error {
			if fv.Int() != n {
				return fmt.Errorf("must equal %d", n)
			}
			return nil
		}, nil
	case isUintKind(kind):
		n, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rule len needs an unsigned integer, got %q", arg)
		}
		return func(fv reflect.Value) error {
			if fv.Uint() != n {
				return fmt.Errorf("must equal %d", n)
			}
			return nil
		}, nil
	}
	return nil, fmt.Errorf("rule len unsupported for kind %s", kind)
}

// oneofRule 构造 oneof 规则:值必须是空格分隔候选之一。候选在注册期解析,请求期只比较。
// oneofRule builds a oneof rule: the value must be one of the space-separated
// candidates, parsed at registration and only compared at request time.
func oneofRule(kind reflect.Kind, arg string, hasArg bool) (fieldRule, error) {
	if !hasArg {
		return nil, fmt.Errorf("rule oneof needs candidates, e.g. oneof=a b c")
	}
	cands := strings.Fields(arg)
	if len(cands) == 0 {
		return nil, fmt.Errorf("rule oneof needs at least one candidate")
	}
	switch {
	case kind == reflect.String:
		set := make(map[string]struct{}, len(cands))
		for _, c := range cands {
			set[c] = struct{}{}
		}
		return func(fv reflect.Value) error {
			if _, ok := set[fv.String()]; !ok {
				return fmt.Errorf("must be one of [%s]", strings.Join(cands, " "))
			}
			return nil
		}, nil
	case isIntKind(kind):
		set := make(map[int64]struct{}, len(cands))
		for _, c := range cands {
			n, err := strconv.ParseInt(c, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("rule oneof candidate %q is not an integer", c)
			}
			set[n] = struct{}{}
		}
		return func(fv reflect.Value) error {
			if _, ok := set[fv.Int()]; !ok {
				return fmt.Errorf("must be one of [%s]", strings.Join(cands, " "))
			}
			return nil
		}, nil
	case isUintKind(kind):
		set := make(map[uint64]struct{}, len(cands))
		for _, c := range cands {
			n, err := strconv.ParseUint(c, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("rule oneof candidate %q is not an unsigned integer", c)
			}
			set[n] = struct{}{}
		}
		return func(fv reflect.Value) error {
			if _, ok := set[fv.Uint()]; !ok {
				return fmt.Errorf("must be one of [%s]", strings.Join(cands, " "))
			}
			return nil
		}, nil
	}
	return nil, fmt.Errorf("rule oneof unsupported for kind %s", kind)
}

// emailRule 是简化邮箱校验:非空 local、单个 '@'、domain 含 '.' 且各段非空。不追求完整
// RFC 5322,只挡明显非法值(与多数框架的默认 email 校验取向一致)。
// emailRule is a simplified email check: non-empty local part, a single '@', a
// domain containing '.' with non-empty labels. Not full RFC 5322 — it only
// rejects obviously invalid values, matching most frameworks' default email check.
func emailRule(fv reflect.Value) error {
	s := fv.String()
	at := strings.IndexByte(s, '@')
	if at <= 0 || at != strings.LastIndexByte(s, '@') {
		return fmt.Errorf("must be a valid email")
	}
	domain := s[at+1:]
	dot := strings.LastIndexByte(domain, '.')
	if dot <= 0 || dot == len(domain)-1 {
		return fmt.Errorf("must be a valid email")
	}
	return nil
}
