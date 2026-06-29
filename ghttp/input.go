package ghttp

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// structInfo caches reflection metadata for input struct types.
type structInfo struct {
	fields  []fieldInfo
	bodyIdx int
	hasBody bool
}

type fieldInfo struct {
	parentIdx int    // index of Path/Query/Header/Body field in top-level struct
	fieldName string // "Path", "Query", "Header", "Body"
	tagName   string // the tag value (e.g. "id", "name")
	fieldIdx  int    // index inside the nested struct
}

var (
	structCache sync.Map
)

func getStructInfo(t reflect.Type) *structInfo {
	if info, ok := structCache.Load(t); ok {
		return info.(*structInfo)
	}

	info := &structInfo{bodyIdx: -1}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		switch f.Name {
		case "Path", "Query", "Header":
			if f.Type.Kind() == reflect.Struct {
				tagKey := strings.ToLower(f.Name)
				for j := 0; j < f.Type.NumField(); j++ {
					sf := f.Type.Field(j)
					tag := sf.Tag.Get(tagKey)
					if tag == "" {
						continue
					}
					info.fields = append(info.fields, fieldInfo{
						parentIdx: i,
						fieldName: f.Name,
						tagName:   tag,
						fieldIdx:  j,
					})
				}
			}
		case "Body":
			info.bodyIdx = i
			info.hasBody = true
		}
	}

	structCache.Store(t, info)
	return info
}

// BodyDecodeFunc allows users to override default body decoding.
// Return nil to fall through to default.
var defaultBodyDecodeFn BodyDecodeFunc

// BodyDecodeFunc is a function that decodes request body.
type BodyDecodeFunc func(r io.Reader, ct string, target interface{}) error

// SetBodyDecoder overrides the default body decoding logic.
func SetBodyDecoder(fn BodyDecodeFunc) {
	defaultBodyDecodeFn = fn
}

func ParseInput(r *http.Request, input interface{}) error {
	return parseInput(r, input)
}

func parseInput(r *http.Request, input interface{}) error {
	v := reflect.ValueOf(input)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return nil
	}

	t := v.Type()
	info := getStructInfo(t)

	for _, fi := range info.fields {
		parent := v.Field(fi.parentIdx)
		field := parent.Field(fi.fieldIdx)

		switch fi.fieldName {
		case "Path":
			if params := Params(r); params != nil {
				if val, ok := params[fi.tagName]; ok {
					setFieldDirect(field, val)
				}
			}
		case "Query":
			if val := r.URL.Query().Get(fi.tagName); val != "" {
				setFieldDirect(field, val)
			}
		case "Header":
			if val := r.Header.Get(fi.tagName); val != "" {
				setFieldDirect(field, val)
			}
		}
	}

	// Parse Body
	if info.hasBody {
		bodyField := v.Field(info.bodyIdx)

		// Check if multipart — parse the form first
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/form-data") {
			if err := r.ParseMultipartForm(defaultMaxMemory); err != nil {
				return err
			}
			if err := fillMultipartBody(bodyField, r); err != nil {
				return err
			}
		} else if r.Body != nil && r.Body != http.NoBody {
			if err := parseBody(r, bodyField); err != nil {
				return err
			}
		}
	}

	return nil
}

func parseBody(r *http.Request, bodyField reflect.Value) error {
	if bodyField.Kind() != reflect.Struct {
		return nil
	}

	// When Body is a nested struct (e.g. `Body struct { Name string }`),
	// we decode the JSON into this nested struct directly.
	if bodyField.Type().NumField() == 0 {
		return nil
	}

	ct := r.Header.Get("Content-Type")

	// Try user-defined decoder first
	if defaultBodyDecodeFn != nil {
		return defaultBodyDecodeFn(r.Body, ct, bodyField.Addr().Interface())
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

func setFieldDirect(field reflect.Value, val string) {
	if !field.CanSet() {
		return
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(val)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, _ := strconv.ParseInt(val, 10, 64)
		field.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, _ := strconv.ParseUint(val, 10, 64)
		field.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, _ := strconv.ParseFloat(val, 64)
		field.SetFloat(n)
	case reflect.Bool:
		b, _ := strconv.ParseBool(val)
		field.SetBool(b)
	}
}
