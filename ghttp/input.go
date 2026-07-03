package ghttp

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
)

// structInfo caches reflection metadata for input struct types.
type structInfo struct {
	bodyIdx   int
	paramsIdx int
	hasBody   bool
	err       error
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
		if f.Name == "Body" {
			info.bodyIdx = i
			info.hasBody = true
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

// BodyDecodeFunc decodes a request body into target.
type BodyDecodeFunc func(r io.Reader, ct string, target interface{}) error

func ParseInput(r *http.Request, input interface{}) error {
	return parseInputWithConfig(r, input, nil)
}

func parseInput(r *http.Request, input interface{}) error {
	return parseInputWithConfig(r, input, nil)
}

func parseInputWithConfig(r *http.Request, input interface{}, c *Config) error {
	return parseInputWithConfigAndPathParams(r, input, c, pathParamList{})
}

func parseInputWithConfigAndPathParams(r *http.Request, input interface{}, c *Config, routeParams pathParamList) error {
	v := reflect.ValueOf(input)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	if v.Type().Elem() == reflect.TypeOf((*Params)(nil)) {
		return directPointerParamsUsageError(v.Type().Elem())
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
	if info.paramsIdx >= 0 {
		v.Field(info.paramsIdx).Set(reflect.ValueOf(paramsFromRequestWithPathParams(r, c, routeParams)))
	}

	// Parse Body
	if info.hasBody {
		bodyField := v.Field(info.bodyIdx)

		// Check if multipart - parse the form first
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/form-data") {
			if err := r.ParseMultipartForm(defaultMaxMemory); err != nil {
				return err
			}
			if err := fillMultipartBody(bodyField, r); err != nil {
				return err
			}
		} else if r.Body != nil && r.Body != http.NoBody {
			if err := parseBody(r, bodyField, c); err != nil {
				return err
			}
		}
	}

	return nil
}

func parseBody(r *http.Request, bodyField reflect.Value, c *Config) error {
	if bodyField.Kind() != reflect.Struct {
		return nil
	}

	// When Body is a nested struct (e.g. `Body struct { Name string }`),
	// we decode the JSON into this nested struct directly.
	if bodyField.Type().NumField() == 0 {
		return nil
	}

	ct := r.Header.Get("Content-Type")

	if c != nil && c.bodyDecoder != nil {
		return c.bodyDecoder(r.Body, ct, bodyField.Addr().Interface())
	}

	// Default streaming handling
	ct = strings.Split(ct, ";")[0] // strip charset etc.
	switch ct {
	case "application/json":
		return json.NewDecoder(r.Body).Decode(bodyField.Addr().Interface())
	case "application/xml", "text/xml":
		return xml.NewDecoder(r.Body).Decode(bodyField.Addr().Interface())
	default:
		// Default to JSON
		return json.NewDecoder(r.Body).Decode(bodyField.Addr().Interface())
	}
}
