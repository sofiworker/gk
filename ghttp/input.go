package ghttp

import (
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// structInfo 缓存输入结构体的反射元数据。
// structInfo caches reflection metadata for input struct types.
type structInfo struct {
	bodyIdx    int
	paramsIdx  int
	hasBody    bool
	bodyLazy   bool
	isLazyBody bool
	usesParams bool
	pathOnly   bool
	tagged     []taggedParamField
	err        error
}

type taggedParamSource uint8

const (
	taggedPath taggedParamSource = iota
	taggedQuery
	taggedHeader
	taggedCookie
)

type taggedParamField struct {
	index      int
	name       string
	source     taggedParamSource
	defaultVal string
	fieldName  string
	collection bool
}

var (
	structCache sync.Map
)

func getStructInfo(t reflect.Type) *structInfo {
	if info, ok := structCache.Load(t); ok {
		return info.(*structInfo)
	}

	info := &structInfo{bodyIdx: -1, paramsIdx: -1}
	info.isLazyBody = t.Implements(bodyFieldMarkerType)
	paramsType := reflect.TypeOf(Params{})
	paramsPtrType := reflect.TypeOf((*Params)(nil))
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type == paramsType {
			info.paramsIdx = i
			continue
		}
		if f.Type == paramsType {
			info.err = invalidParamsUsageError(t, f, "must be embedded anonymously")
			break
		}
		if f.Type == paramsPtrType {
			info.err = invalidParamsUsageError(t, f, "must be embedded as a value, not a pointer")
			break
		}
		if f.Anonymous && embeddedStructContainsParams(f.Type) {
			info.err = invalidParamsUsageError(t, f, "must not embed a struct that contains Params")
			break
		}
		if f.Name == "Body" && !isLazyBodyFieldType(f.Type) && !isLazyBodyPointerType(f.Type) {
			if info.hasBody {
				info.err = ErrMultipleBodyFields
				break
			}
			info.bodyIdx = i
			info.hasBody = true
			continue
		}
		if isLazyBodyFieldType(f.Type) {
			if info.hasBody {
				info.err = ErrMultipleBodyFields
				break
			}
			info.bodyIdx = i
			info.hasBody = true
			info.bodyLazy = true
			continue
		}
		if isLazyBodyPointerType(f.Type) {
			info.err = ErrBodyFieldMustBeValue
			break
		}
		if f.IsExported() {
			for _, tag := range [...]struct {
				source taggedParamSource
				name   string
			}{
				{taggedPath, "path"},
				{taggedQuery, "query"},
				{taggedHeader, "header"},
				{taggedCookie, "cookie"},
			} {
				name, ok := bindingName(f.Tag.Get(tag.name))
				if !ok {
					continue
				}
				info.tagged = append(info.tagged, taggedParamField{
					index:      i,
					name:       name,
					source:     tag.source,
					defaultVal: f.Tag.Get("default"),
					fieldName:  f.Name,
					collection: isCollectionType(f.Type),
				})
				break
			}
		}
	}
	info.usesParams = info.paramsIdx >= 0 || len(info.tagged) > 0
	info.pathOnly = info.paramsIdx < 0 && len(info.tagged) > 0
	for _, binding := range info.tagged {
		if binding.source != taggedPath {
			info.pathOnly = false
			break
		}
	}

	structCache.Store(t, info)
	return info
}

func invalidParamsUsageError(t reflect.Type, f reflect.StructField, reason string) error {
	return fmt.Errorf("%w: %s.%s %s; use anonymous ghttp.Params value embedding", ErrInvalidParamsUsage, t, f.Name, reason)
}

func directPointerParamsUsageError(t reflect.Type) error {
	return fmt.Errorf("%w: %s cannot be used directly as route input; use ghttp.Params value instead", ErrInvalidParamsUsage, t)
}

func embeddedStructContainsParams(t reflect.Type) bool {
	return embeddedStructContainsParamsSeen(t, make(map[reflect.Type]bool))
}

func embeddedStructContainsParamsSeen(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	if seen[t] {
		return false
	}
	seen[t] = true

	paramsType := reflect.TypeOf(Params{})
	paramsPtrType := reflect.TypeOf((*Params)(nil))
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type == paramsType || f.Type == paramsPtrType {
			return true
		}
		if f.Anonymous && embeddedStructContainsParamsSeen(f.Type, seen) {
			return true
		}
	}
	return false
}

func validateInputParamsUsage(t reflect.Type) error {
	if t == nil {
		return nil
	}
	if t == reflect.TypeOf((*Params)(nil)) {
		return directPointerParamsUsageError(t)
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == reflect.TypeOf(Params{}) {
		return nil
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return getStructInfo(t).err
}

func validateRequestParamsUsage[Req any]() error {
	var input Req
	return validateInputParamsUsage(reflect.TypeOf(input))
}

// BodyDecodeFunc 将请求体解码到 Body 字段地址；Body *T 的 target 类型为 **T。
// BodyDecodeFunc decodes a request body into the Body field address; target is
// **T when the field type is *T.
type BodyDecodeFunc func(r io.Reader, ct string, target interface{}) error

func ParseInput(r *http.Request, input interface{}) error {
	return parseInputWithConfig(r, input, nil)
}

func parseInput(r *http.Request, input interface{}) error {
	return parseInputWithConfig(r, input, nil)
}

func parseInputWithConfig(r *http.Request, input interface{}, c *Config) error {
	return parseInputWithConfigAndPathParams(r, input, c, nil, pathParamList{})
}

func parseInputWithConfigAndPathParams(r *http.Request, input interface{}, c *Config, codecMgr *CodecManager, routeParams pathParamList) error {
	return parseCompiledInput(r, input, c, codecMgr, routeParams, nil)
}

// parseCompiledInput 使用注册期 structInfo 构建请求输入；info 为 nil 时保留
// ParseInput 的独立调用语义并现场查找缓存。
// parseCompiledInput builds request input with registration-time structInfo;
// a nil info preserves standalone ParseInput behavior by consulting the cache.
func parseCompiledInput(r *http.Request, input interface{}, c *Config, codecMgr *CodecManager, routeParams pathParamList, info *structInfo) error {
	v := reflect.ValueOf(input)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	if v.Type().Elem() == reflect.TypeOf((*Params)(nil)) {
		return directPointerParamsUsageError(v.Type().Elem())
	}
	// 顶层简写:Req 本身就是 Body[T] 时直接装句柄,不走结构体扫描。
	// top-level shorthand: when Req itself is Body[T], install the handle
	// directly instead of scanning the struct.
	if t := v.Type().Elem(); t.Kind() == reflect.Struct {
		if info == nil {
			info = getStructInfo(t)
		}
		if info.isLazyBody {
			if setter, ok := input.(bodySourceSetter); ok {
				setter.setBodySource(newBodySource(r, c, codecMgr))
				return nil
			}
		}
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return nil
	}

	t := v.Type()
	if t == reflect.TypeOf(Params{}) {
		v.Set(reflect.ValueOf(paramsFromRequestWithPathParams(r, c, routeParams)))
		return nil
	}
	if info == nil {
		info = getStructInfo(t)
	}
	if info.err != nil {
		return info.err
	}
	// 结构体既无 Params 嵌入也无绑定 tag 时,完全跳过 Params 视图。
	// skip the Params view entirely when the struct uses neither.
	if info.usesParams {
		var params Params
		if info.pathOnly && routeParams.Len() > 0 {
			// 仅 path tag 的输入直接使用 matcher 已提取的内联参数，不构造
			// 请求级 Params 状态。
			// Path-only tagged input consumes the matcher's inline params without
			// constructing request-scoped Params state.
			params.path = routeParams
		} else {
			params = paramsFromRequestWithPathParams(r, c, routeParams)
		}
		if err := bindTaggedParams(v, info, &params); err != nil {
			return err
		}
		if info.paramsIdx >= 0 {
			// 按具体类型赋值,避免 reflect.Set 经 copyVal 为整个 Params 再分配。
			// assign by concrete type to avoid reflect.Set allocating a copy of
			// the whole Params via copyVal.
			*(v.Field(info.paramsIdx).Addr().Interface().(*Params)) = params
		}
	}

	// 解析请求体；parse body.
	if info.hasBody && info.bodyLazy {
		setter := v.Field(info.bodyIdx).Addr().Interface().(bodySourceSetter)
		setter.setBodySource(newBodySource(r, c, codecMgr))
	} else if info.hasBody {
		bodyField := v.Field(info.bodyIdx)

		// 先解析 form 以判断是否 multipart；parse the form first to detect multipart.
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/form-data") {
			if err := r.ParseMultipartForm(defaultMaxMemory); err != nil {
				return err
			}
			if err := fillMultipartBody(bodyField, r.MultipartForm); err != nil {
				return err
			}
		} else if r.Body != nil && r.Body != http.NoBody {
			if err := parseBody(r, bodyField, c, codecMgr); err != nil {
				return err
			}
		}
	}

	return nil
}

func bindTaggedParams(v reflect.Value, info *structInfo, params *Params) error {
	for _, binding := range info.tagged {
		field := v.Field(binding.index)
		if !field.CanSet() {
			continue
		}
		if binding.collection {
			var values []string
			switch binding.source {
			case taggedQuery:
				values = params.QueryList(binding.name)
			case taggedHeader:
				values = params.HeaderList(binding.name)
			default:
				value := taggedParamValue(params, binding)
				if value != "" {
					values = []string{value}
				}
			}
			if len(values) == 0 && binding.defaultVal != "" {
				values = []string{binding.defaultVal}
			}
			if len(values) == 0 {
				continue
			}
			if err := setValuesFromStrings(field, values); err != nil {
				return fmt.Errorf("bind %s parameter %q to %s: %w", binding.source, binding.name, binding.fieldName, err)
			}
			continue
		}
		value := taggedParamValue(params, binding)
		if value == "" {
			value = binding.defaultVal
		}
		if value == "" {
			continue
		}
		if err := setValueFromString(field, value); err != nil {
			return fmt.Errorf("bind %s parameter %q to %s: %w", binding.source, binding.name, binding.fieldName, err)
		}
	}
	return nil
}

// bindDirectPathInput 执行注册期确认的 path-only 绑定计划。
// bindDirectPathInput executes a registration-validated path-only binding plan.
func bindDirectPathInput(target any, info *structInfo, params pathParamList) error {
	if info == nil || !info.pathOnly {
		return nil
	}
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return nil
	}
	for _, binding := range info.tagged {
		field := v.Field(binding.index)
		if !field.CanSet() {
			continue
		}
		value := params.Get(binding.name)
		if value == "" {
			value = binding.defaultVal
		}
		if value == "" {
			continue
		}
		if binding.collection {
			if err := setValuesFromStrings(field, []string{value}); err != nil {
				return fmt.Errorf("bind path parameter %q to %s: %w", binding.name, binding.fieldName, err)
			}
			continue
		}
		if err := setValueFromString(field, value); err != nil {
			return fmt.Errorf("bind path parameter %q to %s: %w", binding.name, binding.fieldName, err)
		}
	}
	return nil
}

func (s taggedParamSource) String() string {
	switch s {
	case taggedPath:
		return "path"
	case taggedQuery:
		return "query"
	case taggedHeader:
		return "header"
	case taggedCookie:
		return "cookie"
	default:
		return "unknown"
	}
}

func taggedParamValue(params *Params, binding taggedParamField) string {
	switch binding.source {
	case taggedPath:
		if params == nil {
			return ""
		}
		if value := params.path.Get(binding.name); value != "" {
			return value
		}
		if params.state != nil && params.state.lazyPath != nil {
			// tag 绑定把已解码值写入 Params 的内联数组；随后复制给嵌入
			// Params，handler 再读取同一 key 时无需分配或重复解码。
			// Tag binding stores decoded values in Params' inline array; the
			// embedded Params copy can reuse them without allocation or re-decoding.
			return params.state.lazyPath.get(binding.name, &params.path)
		}
		return ""
	case taggedQuery:
		return params.Query(binding.name)
	case taggedHeader:
		return params.Header(binding.name)
	case taggedCookie:
		return params.Cookie(binding.name)
	default:
		return ""
	}
}

func isCollectionType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Kind() == reflect.Slice || t.Kind() == reflect.Array
}

func setValuesFromStrings(field reflect.Value, values []string) error {
	for field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}
	switch field.Kind() {
	case reflect.Slice:
		result := reflect.MakeSlice(field.Type(), len(values), len(values))
		for i, value := range values {
			if err := setValueFromString(result.Index(i), value); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		field.Set(result)
		return nil
	case reflect.Array:
		if len(values) != field.Len() {
			return fmt.Errorf("got %d values for array length %d", len(values), field.Len())
		}
		for i, value := range values {
			if err := setValueFromString(field.Index(i), value); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported collection kind %s", field.Kind())
	}
}

func bindingName(tag string) (string, bool) {
	if tag == "" || tag == "-" {
		return "", false
	}
	name := strings.Split(tag, ",")[0]
	if name == "" {
		return "", false
	}
	return name, true
}

func setValueFromString(field reflect.Value, value string) error {
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return setValueFromString(field.Elem(), value)
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
		return nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(parsed)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(parsed)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(value, 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(parsed)
		return nil
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(parsed)
		return nil
	default:
		return fmt.Errorf("unsupported kind %s", field.Kind())
	}
}

func parseBody(r *http.Request, bodyField reflect.Value, c *Config, codecMgr *CodecManager) error {
	if !bodyField.CanAddr() || !bodyField.CanSet() {
		return fmt.Errorf("ghttp: body field is not settable")
	}
	if bodyField.Kind() == reflect.Ptr {
		if bodyField.IsNil() {
			bodyField.Set(reflect.New(bodyField.Type().Elem()))
		}
	}

	ct := r.Header.Get("Content-Type")
	if c != nil && c.bodyDecoder != nil {
		return c.bodyDecoder(r.Body, ct, bodyField.Addr().Interface())
	}
	if codecMgr == nil {
		codecMgr = NewCodecManager()
	}
	mediaType := normalizeContentType(ct)
	var codec Codec
	var ok bool
	if mediaType == "" {
		// 缺失 Content-Type 是文档化的协议便利：按 JSON 解码。
		// Missing Content-Type is a documented convenience: decode as JSON.
		// 未知显式类型保持严格（415）。
		// unknown explicit types stay strict (415).
		codec, ok = codecMgr.Resolve(MIMEJSON)
	} else {
		codec, ok = codecMgr.Resolve(mediaType)
	}
	if !ok {
		if c != nil && c.lenientContentType {
			codec, ok = codecMgr.Resolve(MIMEJSON)
			if !ok {
				return fmt.Errorf("ghttp: json codec not registered")
			}
		} else {
			return Err(http.StatusUnsupportedMediaType, fmt.Sprintf("unsupported media type %q", ct), WithCause(ErrUnsupportedMediaType))
		}
	}
	return codec.Unmarshal(r.Body, bodyField.Addr().Interface())
}
