package v4

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// PathAccessor 实现
// PathAccessor implementation

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

func (p *PathAccessor[T]) MustGet() T {
	val, err := p.Get()
	if err != nil {
		panic(fmt.Sprintf("PathAccessor.MustGet failed: %v", err))
	}
	return val
}

func (p *PathAccessor[T]) Has() bool {
	typ := reflect.TypeFor[T]()

	// EmptyPath 总是返回 true
	// EmptyPath always returns true
	if typ == reflect.TypeFor[EmptyPath]() {
		return true
	}

	if typ.Kind() != reflect.Struct {
		return false
	}

	// 检查所有带 path tag 的字段是否存在
	// Check if all fields with path tag exist
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("path")
		if tag != "" {
			if p.req.Params.Get(tag) == "" {
				return false
			}
		}
	}
	return true
}

// QueryAccessor 实现
// QueryAccessor implementation

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

func (q *QueryAccessor[T]) MustGet() T {
	val, err := q.Get()
	if err != nil {
		panic(fmt.Sprintf("QueryAccessor.MustGet failed: %v", err))
	}
	return val
}

func (q *QueryAccessor[T]) Has() bool {
	typ := reflect.TypeFor[T]()

	// EmptyQuery 总是返回 true
	// EmptyQuery always returns true
	if typ == reflect.TypeFor[EmptyQuery]() {
		return true
	}

	return len(q.req.URL.Query()) > 0
}

// JsonAccessor 实现
// JsonAccessor implementation

func (j *JsonAccessor[T]) Get() (T, error) {
	j.once.Do(func() {
		var val T

		// EmptyBody 不需要读取
		// EmptyBody doesn't need to read
		typ := reflect.TypeFor[T]()
		if typ == reflect.TypeFor[EmptyBody]() {
			j.cached = &val
			return
		}

		// 读取并解析 JSON
		// Read and parse JSON
		if err := json.NewDecoder(j.req.Body).Decode(&val); err != nil {
			j.err = BadRequest(fmt.Sprintf("invalid JSON: %v", err))
			return
		}

		// 可选验证
		// Optional validation
		if validator, ok := any(&val).(interface{ Validate() error }); ok {
			if err := validator.Validate(); err != nil {
				j.err = BadRequest(fmt.Sprintf("validation failed: %v", err))
				return
			}
		}

		j.cached = &val
	})

	if j.err != nil {
		var zero T
		return zero, j.err
	}
	return *j.cached, nil
}

func (j *JsonAccessor[T]) MustGet() T {
	val, err := j.Get()
	if err != nil {
		panic(fmt.Sprintf("JsonAccessor.MustGet failed: %v", err))
	}
	return val
}

func (j *JsonAccessor[T]) Has() bool {
	typ := reflect.TypeFor[T]()

	// EmptyBody 总是返回 true
	// EmptyBody always returns true
	if typ == reflect.TypeFor[EmptyBody]() {
		return true
	}

	ct := j.req.Header.Get("Content-Type")
	return strings.Contains(ct, "application/json")
}

// FormAccessor 实现
// FormAccessor implementation

func (f *FormAccessor[T]) Get() (T, error) {
	f.once.Do(func() {
		parser := getFormParser[T]()
		val, err := parser(f.req)
		if err != nil {
			f.err = err
			return
		}
		f.cached = &val
	})

	if f.err != nil {
		var zero T
		return zero, f.err
	}
	return *f.cached, nil
}

func (f *FormAccessor[T]) MustGet() T {
	val, err := f.Get()
	if err != nil {
		panic(fmt.Sprintf("FormAccessor.MustGet failed: %v", err))
	}
	return val
}

func (f *FormAccessor[T]) Has() bool {
	ct := f.req.Header.Get("Content-Type")
	return strings.Contains(ct, "application/x-www-form-urlencoded") ||
		strings.Contains(ct, "multipart/form-data")
}

// HeaderAccessor 实现
// HeaderAccessor implementation

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

func (h *HeaderAccessor[T]) MustGet() T {
	val, err := h.Get()
	if err != nil {
		panic(fmt.Sprintf("HeaderAccessor.MustGet failed: %v", err))
	}
	return val
}

func (h *HeaderAccessor[T]) Has() bool {
	typ := reflect.TypeFor[T]()
	if typ.Kind() != reflect.Struct {
		return false
	}

	// 检查所有带 header tag 的字段是否存在
	// Check if all fields with header tag exist
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("header")
		if tag != "" {
			if h.req.Header.Get(tag) == "" {
				return false
			}
		}
	}
	return true
}
