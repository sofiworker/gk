package v3

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"sync"
)

// PathAccessor 提供路径参数的类型化延迟访问。
// PathAccessor provides typed lazy access to path parameters.
type PathAccessor[T any] struct {
	req    *Request
	cached *T
	err    error
	once   sync.Once
}

// Get 获取路径参数（首次调用时才解析）。
// Get retrieves path parameters (parsed on first call).
func (p *PathAccessor[T]) Get() (T, error) {
	p.once.Do(func() {
		parser := getPathParser[T]()
		val, err := parser(p.req)
		if err != nil {
			p.err = err
			return
		}
		p.cached = &val
	})

	if p.err != nil {
		var zero T
		return zero, p.err
	}
	return *p.cached, nil
}

// MustGet 获取路径参数，失败时 panic。
// MustGet retrieves path parameters, panics on error.
func (p *PathAccessor[T]) MustGet() T {
	val, err := p.Get()
	if err != nil {
		panic(fmt.Sprintf("PathAccessor.MustGet failed: %v", err))
	}
	return val
}

// QueryAccessor 提供查询参数的类型化延迟访问。
// QueryAccessor provides typed lazy access to query parameters.
type QueryAccessor[T any] struct {
	req    *Request
	cached *T
	err    error
	once   sync.Once
}

// Get 获取查询参数（首次调用时才解析）。
// Get retrieves query parameters (parsed on first call).
func (q *QueryAccessor[T]) Get() (T, error) {
	q.once.Do(func() {
		parser := getQueryParser[T]()
		val, err := parser(q.req)
		if err != nil {
			q.err = err
			return
		}
		q.cached = &val
	})

	if q.err != nil {
		var zero T
		return zero, q.err
	}
	return *q.cached, nil
}

// MustGet 获取查询参数，失败时 panic。
// MustGet retrieves query parameters, panics on error.
func (q *QueryAccessor[T]) MustGet() T {
	val, err := q.Get()
	if err != nil {
		panic(fmt.Sprintf("QueryAccessor.MustGet failed: %v", err))
	}
	return val
}

// HeaderAccessor 提供 HTTP 头的类型化延迟访问。
// HeaderAccessor provides typed lazy access to HTTP headers.
type HeaderAccessor[T any] struct {
	req    *Request
	cached *T
	err    error
	once   sync.Once
}

// Get 获取 HTTP 头（首次调用时才解析）。
// Get retrieves HTTP headers (parsed on first call).
func (h *HeaderAccessor[T]) Get() (T, error) {
	h.once.Do(func() {
		parser := getHeaderParser[T]()
		val, err := parser(h.req)
		if err != nil {
			h.err = err
			return
		}
		h.cached = &val
	})

	if h.err != nil {
		var zero T
		return zero, h.err
	}
	return *h.cached, nil
}

// MustGet 获取 HTTP 头，失败时 panic。
// MustGet retrieves HTTP headers, panics on error.
func (h *HeaderAccessor[T]) MustGet() T {
	val, err := h.Get()
	if err != nil {
		panic(fmt.Sprintf("HeaderAccessor.MustGet failed: %v", err))
	}
	return val
}

// 解析器缓存（注册期编译一次，运行期复用）
// Parser caches (compiled once at registration, reused at runtime)
var (
	pathParsers   = make(map[reflect.Type]func(*Request) (any, error))
	queryParsers  = make(map[reflect.Type]func(*Request) (any, error))
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

	if typ.Kind() != reflect.Struct {
		return func(r *Request) (T, error) {
			var zero T
			return zero, fmt.Errorf("path type must be struct, got %s", typ.Kind())
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

	return func(r *Request) (T, error) {
		var result T
		resultValue := reflect.ValueOf(&result).Elem()

		for _, plan := range plans {
			rawValue := r.Params.Get(plan.name)
			if rawValue == "" {
				if plan.required {
					return result, fmt.Errorf("missing path parameter: %s", plan.name)
				}
				continue
			}

			parsed, err := plan.parser(rawValue)
			if err != nil {
				return result, fmt.Errorf("invalid path parameter %s: %v", plan.name, err)
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
					return result, fmt.Errorf("missing query parameter: %s", plan.name)
				} else {
					continue
				}
			}

			parsed, err := plan.parser(rawValue)
			if err != nil {
				return result, fmt.Errorf("invalid query parameter %s: %v", plan.name, err)
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
					return result, fmt.Errorf("missing header: %s", plan.name)
				}
				continue
			}

			parsed, err := plan.parser(rawValue)
			if err != nil {
				return result, fmt.Errorf("invalid header %s: %v", plan.name, err)
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

// Path 创建类型化的路径参数访问器。
// Path creates a typed path parameter accessor.
//
// 用法 / Usage:
//   type UserID struct { ID int64 `path:"id"` }
//   pathAccessor := v3.Path[UserID](&req)
//   pathData, err := pathAccessor.Get()
func Path[P any, T any](req *RequestOf[T]) *PathAccessor[P] {
	return &PathAccessor[P]{req: req.Request}
}

// Query 创建类型化的查询参数访问器。
// Query creates a typed query parameter accessor.
//
// 用法 / Usage:
//   type ListQuery struct { Page int `query:"page" default:"1"` }
//   queryAccessor := v3.Query[ListQuery](&req)
//   queryData, err := queryAccessor.Get()
func Query[Q any, T any](req *RequestOf[T]) *QueryAccessor[Q] {
	return &QueryAccessor[Q]{req: req.Request}
}

// Header 创建类型化的 HTTP 头访问器。
// Header creates a typed HTTP header accessor.
//
// 用法 / Usage:
//   type AuthHeader struct { Token string `header:"Authorization"` }
//   headerAccessor := v3.Header[AuthHeader](&req)
//   headerData, err := headerAccessor.Get()
func Header[H any, T any](req *RequestOf[T]) *HeaderAccessor[H] {
	return &HeaderAccessor[H]{req: req.Request}
}
