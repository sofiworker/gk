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
	usesParams bool
	err        error
}

var (
	structCache sync.Map
)

func getStructInfo(t reflect.Type) *structInfo {
	if info, ok := structCache.Load(t); ok {
		return info.(*structInfo)
	}

	info := &structInfo{bodyIdx: -1, paramsIdx: -1}
	paramsType := reflect.TypeOf(Params{})
	paramsPtrType := reflect.TypeOf((*Params)(nil))
	info.usesParams = structHasBindingTags(t)
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
	}
	if info.paramsIdx >= 0 {
		info.usesParams = true
	}

	structCache.Store(t, info)
	return info
}

// structHasBindingTags 报告结构体是否有 path/query/header/cookie 绑定 tag。
// structHasBindingTags reports whether the struct carries any binding tag.
func structHasBindingTags(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		for _, tag := range []string{"path", "query", "header", "cookie"} {
			if name, ok := bindingName(f.Tag.Get(tag)); ok && name != "" {
				return true
			}
		}
	}
	return false
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

// BodyDecodeFunc 将请求体解码到 target。
// BodyDecodeFunc decodes a request body into target.
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
	if t := v.Type().Elem(); t.Kind() == reflect.Struct && t.Implements(bodyFieldMarkerType) {
		if setter, ok := input.(bodySourceSetter); ok {
			setter.setBodySource(newBodySource(r, c, codecMgr))
			return nil
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
	info := getStructInfo(t)
	if info.err != nil {
		return info.err
	}
	// 结构体既无 Params 嵌入也无绑定 tag 时,完全跳过 Params 视图。
	// skip the Params view entirely when the struct uses neither.
	if info.usesParams {
		params := paramsFromRequestWithPathParams(r, c, routeParams)
		if info.paramsIdx >= 0 {
			v.Field(info.paramsIdx).Set(reflect.ValueOf(params))
		}
		if err := bindTaggedParams(v, info, params); err != nil {
			return err
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

func bindTaggedParams(v reflect.Value, info *structInfo, params Params) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if i == info.bodyIdx || i == info.paramsIdx {
			continue
		}
		fieldInfo := t.Field(i)
		if !fieldInfo.IsExported() {
			continue
		}
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}
		if err := bindTaggedParamField(field, fieldInfo, params); err != nil {
			return err
		}
	}
	return nil
}

func bindTaggedParamField(field reflect.Value, fieldInfo reflect.StructField, params Params) error {
	for _, binding := range []struct {
		tag string
		get func(string) string
	}{
		{tag: "path", get: params.Path},
		{tag: "query", get: params.Query},
		{tag: "header", get: params.Header},
		{tag: "cookie", get: params.Cookie},
	} {
		name, ok := bindingName(fieldInfo.Tag.Get(binding.tag))
		if !ok {
			continue
		}
		value := binding.get(name)
		if value == "" {
			value = fieldInfo.Tag.Get("default")
		}
		if value == "" {
			return nil
		}
		if err := setValueFromString(field, value); err != nil {
			return fmt.Errorf("bind %s parameter %q to %s: %w", binding.tag, name, fieldInfo.Name, err)
		}
		return nil
	}
	return nil
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
	if bodyField.Kind() != reflect.Struct || bodyField.Type().NumField() == 0 {
		return nil
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
