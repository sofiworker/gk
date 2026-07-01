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
	fields    []fieldInfo
	bodyIdx   int
	paramsIdx int
	hasBody   bool
}

type fieldInfo struct {
	parentIdx    int    // index of Path/Query/Header/Body field in top-level struct
	fieldName    string // "Path", "Query", "Header", "Body"
	tagName      string // the tag value (e.g. "id", "name")
	fieldIdx     int    // index inside the nested struct
	defaultValue string
	hasDefault   bool
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
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type == paramsType {
			info.paramsIdx = i
			continue
		}
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
						parentIdx:    i,
						fieldName:    f.Name,
						tagName:      tag,
						fieldIdx:     j,
						defaultValue: sf.Tag.Get("default"),
						hasDefault:   sf.Tag.Get("default") != "",
					})
				}
			}
		case "Body":
			info.bodyIdx = i
			info.hasBody = true
		default:
			if tag := f.Tag.Get("path"); tag != "" {
				info.fields = append(info.fields, fieldInfo{
					parentIdx:    -1,
					fieldName:    "Path",
					tagName:      tag,
					fieldIdx:     i,
					defaultValue: f.Tag.Get("default"),
					hasDefault:   f.Tag.Get("default") != "",
				})
			}
			if tag := f.Tag.Get("query"); tag != "" {
				info.fields = append(info.fields, fieldInfo{
					parentIdx:    -1,
					fieldName:    "Query",
					tagName:      tag,
					fieldIdx:     i,
					defaultValue: f.Tag.Get("default"),
					hasDefault:   f.Tag.Get("default") != "",
				})
			}
			if tag := f.Tag.Get("header"); tag != "" {
				info.fields = append(info.fields, fieldInfo{
					parentIdx:    -1,
					fieldName:    "Header",
					tagName:      tag,
					fieldIdx:     i,
					defaultValue: f.Tag.Get("default"),
					hasDefault:   f.Tag.Get("default") != "",
				})
			}
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
	return parseInputWithConfig(r, input, nil)
}

func parseInput(r *http.Request, input interface{}) error {
	return parseInputWithConfig(r, input, nil)
}

func parseInputWithConfig(r *http.Request, input interface{}, c *Config) error {
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
	if info.paramsIdx >= 0 {
		clientIP := defaultClientIPResolver(r)
		if c != nil && c.clientIPResolver != nil {
			clientIP = c.clientIPResolver(r)
		}
		v.Field(info.paramsIdx).Set(reflect.ValueOf(newParams(pathParams(r), r.URL.Query(), r.Header, clientIP)))
	}

	for _, fi := range info.fields {
		field := v.Field(fi.fieldIdx)
		if fi.parentIdx >= 0 {
			field = v.Field(fi.parentIdx).Field(fi.fieldIdx)
		}

		switch fi.fieldName {
		case "Path":
			if params := pathParams(r); params != nil {
				if val, ok := params[fi.tagName]; ok {
					setFieldDirect(field, val)
				} else if fi.hasDefault {
					setFieldDirect(field, fi.defaultValue)
				}
			} else if fi.hasDefault {
				setFieldDirect(field, fi.defaultValue)
			}
		case "Query":
			values := r.URL.Query()[fi.tagName]
			if len(values) > 0 {
				setFieldValues(field, values)
			} else if fi.hasDefault {
				setFieldDirect(field, fi.defaultValue)
			}
		case "Header":
			if val := r.Header.Get(fi.tagName); val != "" {
				setFieldDirect(field, val)
			} else if fi.hasDefault {
				setFieldDirect(field, fi.defaultValue)
			}
		}
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

func setFieldValues(field reflect.Value, values []string) {
	if !field.CanSet() || len(values) == 0 {
		return
	}
	if field.Kind() != reflect.Slice {
		setFieldDirect(field, values[0])
		return
	}

	slice := reflect.MakeSlice(field.Type(), 0, len(values))
	for _, value := range values {
		elem := reflect.New(field.Type().Elem()).Elem()
		setFieldDirect(elem, value)
		slice = reflect.Append(slice, elem)
	}
	field.Set(slice)
}
