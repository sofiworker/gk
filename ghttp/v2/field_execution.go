package v2

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
)

type fieldSetter func(reflect.Value, []string) error
type fieldSetterOne func(reflect.Value, string) error

var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

// 字段转换器在注册时确定，请求期间只执行预先选定的转换。
// Field converters are chosen at registration; requests only run the selected conversion.
func compileFieldSetter(t reflect.Type) (fieldSetter, error) {
	if one, err := compileFieldSetterOne(t); err == nil {
		return func(v reflect.Value, raw []string) error { return one(v, raw[0]) }, nil
	}
	if t.Kind() == reflect.Pointer {
		next, err := compileFieldSetter(t.Elem())
		if err != nil {
			return nil, err
		}
		return func(v reflect.Value, raw []string) error {
			value := reflect.New(t.Elem())
			if !v.IsNil() {
				value.Elem().Set(v.Elem())
			}
			if err := next(value.Elem(), raw); err != nil {
				return err
			}
			v.Set(value)
			return nil
		}, nil
	}
	if t.Kind() != reflect.Slice {
		return nil, fmt.Errorf("ghttp/v2: unsupported source field type %s", t)
	}
	one, err := compileFieldSetterOne(t.Elem())
	if err != nil {
		return nil, fmt.Errorf("ghttp/v2: unsupported source field type %s: %w", t, err)
	}
	return func(v reflect.Value, raw []string) error {
		result := reflect.MakeSlice(t, len(raw), len(raw))
		for i, value := range raw {
			if err := one(result.Index(i), value); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
		v.Set(result)
		return nil
	}, nil
}

func compileFieldSetterOne(t reflect.Type) (fieldSetterOne, error) {
	if t.Kind() == reflect.Pointer {
		if t.Implements(textUnmarshalerType) {
			return func(v reflect.Value, raw string) error {
				value := reflect.New(t.Elem())
				if !v.IsNil() {
					value.Elem().Set(v.Elem())
				}
				if err := value.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(raw)); err != nil {
					return err
				}
				v.Set(value)
				return nil
			}, nil
		}
		next, err := compileFieldSetterOne(t.Elem())
		if err != nil {
			return nil, err
		}
		return func(v reflect.Value, raw string) error {
			value := reflect.New(t.Elem())
			if !v.IsNil() {
				value.Elem().Set(v.Elem())
			}
			if err := next(value.Elem(), raw); err != nil {
				return err
			}
			v.Set(value)
			return nil
		}, nil
	}
	if t.Implements(textUnmarshalerType) {
		return func(v reflect.Value, raw string) error {
			return v.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(raw))
		}, nil
	}
	if reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return func(v reflect.Value, raw string) error {
			value := reflect.New(t)
			value.Elem().Set(v)
			if err := value.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(raw)); err != nil {
				return err
			}
			v.Set(value.Elem())
			return nil
		}, nil
	}
	switch t.Kind() {
	case reflect.String:
		return func(v reflect.Value, raw string) error { v.SetString(raw); return nil }, nil
	case reflect.Bool:
		return func(v reflect.Value, raw string) error {
			x, err := strconv.ParseBool(raw)
			if err == nil {
				v.SetBool(x)
			}
			return err
		}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := t.Bits()
		return func(v reflect.Value, raw string) error {
			x, err := strconv.ParseInt(raw, 10, bits)
			if err == nil {
				v.SetInt(x)
			}
			return err
		}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		bits := t.Bits()
		return func(v reflect.Value, raw string) error {
			x, err := strconv.ParseUint(raw, 10, bits)
			if err == nil {
				v.SetUint(x)
			}
			return err
		}, nil
	case reflect.Float32, reflect.Float64:
		bits := t.Bits()
		return func(v reflect.Value, raw string) error {
			x, err := strconv.ParseFloat(raw, bits)
			if err == nil {
				v.SetFloat(x)
			}
			return err
		}, nil
	}
	return nil, fmt.Errorf("ghttp/v2: unsupported scalar source field type %s", t)
}
