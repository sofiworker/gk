package ghttp

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"time"
)

const (
	maxPublicParamDepth      = 8
	maxPublicParamCount      = 32
	maxPublicContainerItems  = 64
	maxPublicParamStringSize = 4 << 10
	maxPublicArgsJSONSize    = 32 << 10
)

type PublicParamMarshaler interface {
	MarshalPublicParam() (any, error)
}

type paramSanitizer struct {
	diagnostic bool
	cyclic     bool
	visiting   map[visitKey]struct{}
}

type visitKey struct {
	typeName reflect.Type
	pointer  uintptr
}

func sanitizePublicParams(params map[string]any) (map[string]any, bool) {
	if len(params) == 0 {
		return map[string]any{}, false
	}
	sanitizer := &paramSanitizer{visiting: make(map[visitKey]struct{})}
	keys := sortedMapKeys(params)
	if len(keys) > maxPublicParamCount {
		keys = keys[:maxPublicParamCount]
		sanitizer.diagnostic = true
	}
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		sanitizer.cyclic = false
		value, ok := sanitizer.sanitize(reflect.ValueOf(params[key]), 0)
		if sanitizer.cyclic {
			ok = false
		}
		if ok {
			out[key] = value
		} else {
			sanitizer.diagnostic = true
		}
	}
	encoded, err := json.Marshal(out)
	if err != nil || len(encoded) > maxPublicArgsJSONSize {
		return map[string]any{}, true
	}
	return out, sanitizer.diagnostic
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *paramSanitizer) sanitize(value reflect.Value, depth int) (out any, ok bool) {
	if depth > maxPublicParamDepth || !value.IsValid() {
		return nil, depth <= maxPublicParamDepth
	}
	if isNilValue(value) {
		if value.Kind() == reflect.Interface {
			return nil, true
		}
		return nil, false
	}
	if value.CanInterface() {
		if marshaler, implements := value.Interface().(PublicParamMarshaler); implements {
			marshaled, err, panicked := marshalPublicParam(marshaler)
			if err != nil || panicked {
				return nil, false
			}
			return s.sanitize(reflect.ValueOf(marshaled), depth+1)
		}
		if _, unsafe := value.Interface().(error); unsafe {
			return nil, false
		}
		switch typed := value.Interface().(type) {
		case time.Time:
			return typed.Format(time.RFC3339Nano), true
		case time.Duration:
			return typed.String(), true
		case json.Number:
			if _, err := strconv.ParseFloat(string(typed), 64); err != nil {
				return nil, false
			}
			return typed, true
		}
	}
	if value.Kind() == reflect.Interface {
		return s.sanitize(value.Elem(), depth)
	}
	switch value.Kind() {
	case reflect.Bool:
		return value.Bool(), true
	case reflect.String:
		if value.Len() > maxPublicParamStringSize {
			return nil, false
		}
		return value.String(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value.Uint(), true
	case reflect.Float32, reflect.Float64:
		floating := value.Float()
		if math.IsNaN(floating) || math.IsInf(floating, 0) {
			return nil, false
		}
		return floating, true
	case reflect.Slice, reflect.Array:
		return s.sanitizeSlice(value, depth)
	case reflect.Map:
		return s.sanitizeMap(value, depth)
	default:
		return nil, false
	}
}

func (s *paramSanitizer) sanitizeSlice(value reflect.Value, depth int) (any, bool) {
	if value.Len() > maxPublicContainerItems {
		return nil, false
	}
	release, ok := s.enter(value)
	if !ok {
		return nil, false
	}
	defer release()
	out := make([]any, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		item, valid := s.sanitize(value.Index(i), depth+1)
		if !valid {
			s.diagnostic = true
			continue
		}
		out = append(out, item)
	}
	return out, true
}

func (s *paramSanitizer) sanitizeMap(value reflect.Value, depth int) (any, bool) {
	if value.Type().Key().Kind() != reflect.String || value.Len() > maxPublicContainerItems {
		return nil, false
	}
	release, ok := s.enter(value)
	if !ok {
		return nil, false
	}
	defer release()
	keys := value.MapKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		item, valid := s.sanitize(value.MapIndex(key), depth+1)
		if !valid {
			s.diagnostic = true
			continue
		}
		out[key.String()] = item
	}
	return out, true
}

func (s *paramSanitizer) enter(value reflect.Value) (func(), bool) {
	if value.Kind() == reflect.Array {
		return func() {}, true
	}
	key := visitKey{typeName: value.Type(), pointer: value.Pointer()}
	if _, exists := s.visiting[key]; exists {
		s.cyclic = true
		return nil, false
	}
	s.visiting[key] = struct{}{}
	return func() { delete(s.visiting, key) }, true
}

func isNilValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func marshalPublicParam(marshaler PublicParamMarshaler) (value any, err error, panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	value, err = marshaler.MarshalPublicParam()
	return value, err, false
}
