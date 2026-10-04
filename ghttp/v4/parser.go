package v4

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"sync"
)

// 解析器缓存（注册期编译一次，运行期复用）
// Parser caches (compiled once at registration time, reused at runtime)
var (
	pathParsers   = make(map[reflect.Type]func(*Request) (any, error))
	queryParsers  = make(map[reflect.Type]func(*Request) (any, error))
	formParsers   = make(map[reflect.Type]func(*Request) (any, error))
	headerParsers = make(map[reflect.Type]func(*Request) (any, error))
	parserMu      sync.RWMutex
)

// getPathParser 获取或编译路径参数解析器。
// getPathParser gets or compiles a path parameter parser.
func getPathParser[T any]() func(*Request) (T, error) {
	typ := reflect.TypeFor[T]()

	parserMu.RLock()
	if cached, ok := pathParsers[typ]; ok {
		parserMu.RUnlock()
		return func(r *Request) (T, error) {
			val, err := cached(r)
			if err != nil {
				var zero T
				return zero, err
			}
			return val.(T), nil
		}
	}
	parserMu.RUnlock()

	// 编译新的解析器
	// Compile new parser
	parser := compilePathParser[T]()

	parserMu.Lock()
	pathParsers[typ] = func(r *Request) (any, error) {
		return parser(r)
	}
	parserMu.Unlock()

	return parser
}

// compilePathParser 编译路径参数解析器。
// compilePathParser compiles a path parameter parser.
func compilePathParser[T any]() func(*Request) (T, error) {
	typ := reflect.TypeFor[T]()

	// EmptyPath 特殊处理
	// Special handling for EmptyPath
	if typ == reflect.TypeFor[EmptyPath]() {
		return func(r *Request) (T, error) {
			return any(EmptyPath{}).(T), nil
		}
	}

	// 非结构体类型不支持
	// Non-struct types are not supported
	if typ.Kind() != reflect.Struct {
		return func(r *Request) (T, error) {
			var zero T
			return zero, fmt.Errorf("path type must be struct, got %s", typ.Kind())
		}
	}

	// 构建字段解析计划
	// Build field parsing plan
	type fieldPlan struct {
		index    int
		name     string
		parser   func(string) (any, error)
		required bool
	}

	var plans []fieldPlan
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("path")
		if tag == "" {
			continue
		}

		parser := getFieldParser(field.Type)
		required := field.Tag.Get("required") != "false"

		plans = append(plans, fieldPlan{
			index:    i,
			name:     tag,
			parser:   parser,
			required: required,
		})
	}

	// 返回优化的解析器
	// Return optimized parser
	return func(r *Request) (T, error) {
		var result T
		resultValue := reflect.ValueOf(&result).Elem()

		for _, plan := range plans {
			rawValue := r.Params.Get(plan.name)
			if rawValue == "" {
				if plan.required {
					return result, BadRequest(fmt.Sprintf("missing path parameter: %s", plan.name))
				}
				continue
			}

			parsed, err := plan.parser(rawValue)
			if err != nil {
				return result, BadRequest(fmt.Sprintf("invalid path parameter %s: %v", plan.name, err))
			}

			resultValue.Field(plan.index).Set(reflect.ValueOf(parsed))
		}

		return result, nil
	}
}

// getQueryParser 获取或编译查询参数解析器。
// getQueryParser gets or compiles a query parameter parser.
func getQueryParser[T any]() func(*Request) (T, error) {
	typ := reflect.TypeFor[T]()

	parserMu.RLock()
	if cached, ok := queryParsers[typ]; ok {
		parserMu.RUnlock()
		return func(r *Request) (T, error) {
			val, err := cached(r)
			if err != nil {
				var zero T
				return zero, err
			}
			return val.(T), nil
		}
	}
	parserMu.RUnlock()

	parser := compileQueryParser[T]()

	parserMu.Lock()
	queryParsers[typ] = func(r *Request) (any, error) {
		return parser(r)
	}
	parserMu.Unlock()

	return parser
}

// compileQueryParser 编译查询参数解析器。
// compileQueryParser compiles a query parameter parser.
func compileQueryParser[T any]() func(*Request) (T, error) {
	typ := reflect.TypeFor[T]()

	if typ == reflect.TypeFor[EmptyQuery]() {
		return func(r *Request) (T, error) {
			return any(EmptyQuery{}).(T), nil
		}
	}

	if typ.Kind() != reflect.Struct {
		return func(r *Request) (T, error) {
			var zero T
			return zero, fmt.Errorf("query type must be struct, got %s", typ.Kind())
		}
	}

	type fieldPlan struct {
		index    int
		name     string
		parser   func(string) (any, error)
		required bool
		defVal   string
	}

	var plans []fieldPlan
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("query")
		if tag == "" {
			continue
		}

		parser := getFieldParser(field.Type)
		required := field.Tag.Get("required") != "false"
		defVal := field.Tag.Get("default")

		plans = append(plans, fieldPlan{
			index:    i,
			name:     tag,
			parser:   parser,
			required: required,
			defVal:   defVal,
		})
	}

	return func(r *Request) (T, error) {
		var result T
		resultValue := reflect.ValueOf(&result).Elem()
		query := r.URL.Query()

		for _, plan := range plans {
			rawValue := query.Get(plan.name)
			if rawValue == "" {
				if plan.defVal != "" {
					rawValue = plan.defVal
				} else if plan.required {
					return result, BadRequest(fmt.Sprintf("missing query parameter: %s", plan.name))
				} else {
					continue
				}
			}

			parsed, err := plan.parser(rawValue)
			if err != nil {
				return result, BadRequest(fmt.Sprintf("invalid query parameter %s: %v", plan.name, err))
			}

			resultValue.Field(plan.index).Set(reflect.ValueOf(parsed))
		}

		return result, nil
	}
}

// getFormParser 获取或编译表单解析器。
// getFormParser gets or compiles a form parser.
func getFormParser[T any]() func(*Request) (T, error) {
	typ := reflect.TypeFor[T]()

	parserMu.RLock()
	if cached, ok := formParsers[typ]; ok {
		parserMu.RUnlock()
		return func(r *Request) (T, error) {
			val, err := cached(r)
			if err != nil {
				var zero T
				return zero, err
			}
			return val.(T), nil
		}
	}
	parserMu.RUnlock()

	parser := compileFormParser[T]()

	parserMu.Lock()
	formParsers[typ] = func(r *Request) (any, error) {
		return parser(r)
	}
	parserMu.Unlock()

	return parser
}

// compileFormParser 编译表单解析器。
// compileFormParser compiles a form parser.
func compileFormParser[T any]() func(*Request) (T, error) {
	typ := reflect.TypeFor[T]()

	if typ.Kind() != reflect.Struct {
		return func(r *Request) (T, error) {
			var zero T
			return zero, fmt.Errorf("form type must be struct, got %s", typ.Kind())
		}
	}

	type fieldPlan struct {
		index    int
		name     string
		parser   func(string) (any, error)
		required bool
	}

	var plans []fieldPlan
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("form")
		if tag == "" {
			continue
		}

		parser := getFieldParser(field.Type)
		required := field.Tag.Get("required") != "false"

		plans = append(plans, fieldPlan{
			index:    i,
			name:     tag,
			parser:   parser,
			required: required,
		})
	}

	return func(r *Request) (T, error) {
		var result T

		if err := r.ParseForm(); err != nil {
			return result, BadRequest(fmt.Sprintf("failed to parse form: %v", err))
		}

		resultValue := reflect.ValueOf(&result).Elem()

		for _, plan := range plans {
			rawValue := r.Form.Get(plan.name)
			if rawValue == "" {
				if plan.required {
					return result, BadRequest(fmt.Sprintf("missing form field: %s", plan.name))
				}
				continue
			}

			parsed, err := plan.parser(rawValue)
			if err != nil {
				return result, BadRequest(fmt.Sprintf("invalid form field %s: %v", plan.name, err))
			}

			resultValue.Field(plan.index).Set(reflect.ValueOf(parsed))
		}

		return result, nil
	}
}

// getHeaderParser 获取或编译 header 解析器。
// getHeaderParser gets or compiles a header parser.
func getHeaderParser[T any]() func(*Request) (T, error) {
	typ := reflect.TypeFor[T]()

	parserMu.RLock()
	if cached, ok := headerParsers[typ]; ok {
		parserMu.RUnlock()
		return func(r *Request) (T, error) {
			val, err := cached(r)
			if err != nil {
				var zero T
				return zero, err
			}
			return val.(T), nil
		}
	}
	parserMu.RUnlock()

	parser := compileHeaderParser[T]()

	parserMu.Lock()
	headerParsers[typ] = func(r *Request) (any, error) {
		return parser(r)
	}
	parserMu.Unlock()

	return parser
}

// compileHeaderParser 编译 header 解析器。
// compileHeaderParser compiles a header parser.
func compileHeaderParser[T any]() func(*Request) (T, error) {
	typ := reflect.TypeFor[T]()

	if typ.Kind() != reflect.Struct {
		return func(r *Request) (T, error) {
			var zero T
			return zero, fmt.Errorf("header type must be struct, got %s", typ.Kind())
		}
	}

	type fieldPlan struct {
		index    int
		name     string
		parser   func(string) (any, error)
		required bool
	}

	var plans []fieldPlan
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("header")
		if tag == "" {
			continue
		}

		parser := getFieldParser(field.Type)
		required := field.Tag.Get("required") != "false"

		plans = append(plans, fieldPlan{
			index:    i,
			name:     tag,
			parser:   parser,
			required: required,
		})
	}

	return func(r *Request) (T, error) {
		var result T
		resultValue := reflect.ValueOf(&result).Elem()

		for _, plan := range plans {
			rawValue := r.Header.Get(plan.name)
			if rawValue == "" {
				if plan.required {
					return result, Unauthorized(fmt.Sprintf("missing header: %s", plan.name))
				}
				continue
			}

			parsed, err := plan.parser(rawValue)
			if err != nil {
				return result, BadRequest(fmt.Sprintf("invalid header %s: %v", plan.name, err))
			}

			resultValue.Field(plan.index).Set(reflect.ValueOf(parsed))
		}

		return result, nil
	}
}

// getFieldParser 根据字段类型返回解析函数。
// getFieldParser returns a parsing function based on field type.
func getFieldParser(typ reflect.Type) func(string) (any, error) {
	switch typ.Kind() {
	case reflect.String:
		return func(s string) (any, error) {
			decoded, err := url.QueryUnescape(s)
			if err != nil {
				return nil, err
			}
			return decoded, nil
		}
	case reflect.Int, reflect.Int64:
		return func(s string) (any, error) {
			return strconv.ParseInt(s, 10, 64)
		}
	case reflect.Int32:
		return func(s string) (any, error) {
			val, err := strconv.ParseInt(s, 10, 32)
			return int32(val), err
		}
	case reflect.Uint, reflect.Uint64:
		return func(s string) (any, error) {
			return strconv.ParseUint(s, 10, 64)
		}
	case reflect.Bool:
		return func(s string) (any, error) {
			return strconv.ParseBool(s)
		}
	case reflect.Float64:
		return func(s string) (any, error) {
			return strconv.ParseFloat(s, 64)
		}
	default:
		return func(s string) (any, error) {
			return nil, fmt.Errorf("unsupported type: %s", typ.Kind())
		}
	}
}
