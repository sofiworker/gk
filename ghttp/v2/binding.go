package v2

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

type bindingSource uint8

const (
	sourcePath bindingSource = iota + 1
	sourceQuery
	sourceHeader
	sourceCookie
)

type bindingField struct {
	set    fieldSetter
	setOne fieldSetterOne
	index  []int
	source bindingSource
	name   string
}

type bindingPlan struct {
	fields  []bindingField
	body    []int
	bodyTag string
	form    formPlan
}

type bindingDecoder[T any] struct {
	plan bindingPlan
}

func (d bindingDecoder[T]) ContentType() string {
	switch d.plan.bodyTag {
	case "json":
		return "application/json"
	case "xml":
		return "application/xml"
	case "form":
		return "application/x-www-form-urlencoded"
	case "text":
		return "text/plain"
	default:
		return ""
	}
}

func (d bindingDecoder[T]) Decode(req *Request, dst *T) error {
	if req == nil || req.Request == nil {
		return errors.New("ghttp/v2: binding decoder requires a request")
	}
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("ghttp/v2: binding decoder requires a non-nil destination")
	}
	v = v.Elem()
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("ghttp/v2: binding input requires a struct, got %s", v.Type())
	}
	if d.plan.bodyTag != "" {
		if err := decodeBindingBody(req, fieldValue(v, d.plan.body), d.plan.bodyTag, d.plan.form); err != nil {
			return err
		}
	}
	for _, f := range d.plan.fields {
		var raw []string
		var one string
		var present bool
		switch f.source {
		case sourcePath:
			one = req.Params.Get(f.name)
			present = one != ""
		case sourceQuery:
			if f.setOne != nil {
				one, present = req.QueryFirst(f.name)
			} else {
				raw = req.QueryValues(f.name)
				present = len(raw) > 0
				if present {
					one = raw[0]
				}
			}
		case sourceHeader:
			raw = req.Header.Values(f.name)
			if len(raw) > 0 {
				one, present = raw[0], true
			}
		case sourceCookie:
			if c, err := req.Cookie(f.name); err == nil {
				one, present = c.Value, true
			}
		}
		if present {
			field := fieldValue(v, f.index)
			var err error
			if f.setOne != nil {
				err = f.setOne(field, one)
			} else {
				if raw == nil {
					raw = []string{one}
				}
				err = f.set(field, raw)
			}
			if err != nil {
				return fmt.Errorf("ghttp/v2: field %s: %w", f.name, err)
			}
		}
	}
	return nil
}

func fieldValue(v reflect.Value, index []int) reflect.Value {
	for pos, i := range index {
		v = v.Field(i)
		if pos < len(index)-1 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
	}
	return v
}

func setBindingValue(dst reflect.Value, raw []string) error {
	if dst.Kind() == reflect.Pointer {
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return setBindingValue(dst.Elem(), raw)
	}
	return setFormValue(dst, raw)
}

func decodeBindingBody(req *Request, dst reflect.Value, codec string, plan formPlan) error {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	if dst.Kind() == reflect.Pointer {
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		dst = dst.Elem()
	}
	if !dst.IsValid() || !dst.CanAddr() {
		return errors.New("ghttp/v2: body field is not writable")
	}
	if codec == "text" {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return err
		}
		return setBindingValue(dst, []string{string(b)})
	}
	var err error
	switch codec {
	case "json":
		d := json.NewDecoder(req.Body)
		err = d.Decode(dst.Addr().Interface())
		if err == nil {
			var extra any
			if tail := d.Decode(&extra); tail != io.EOF {
				if tail == nil {
					err = errors.New("ghttp/v2: multiple JSON values")
				} else {
					err = tail
				}
			}
		}
	case "xml":
		err = xml.NewDecoder(req.Body).Decode(dst.Addr().Interface())
	case "form":
		data, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			return readErr
		}
		values, parseErr := url.ParseQuery(string(data))
		if parseErr != nil {
			return parseErr
		}
		if dst.Kind() != reflect.Struct {
			return fmt.Errorf("ghttp/v2: form body requires struct")
		}
		err = bindFormPlan(dst, values, plan)
	}
	return err
}

func defaultInput[T any]() (Input[T], error) {
	typ := reflect.TypeFor[T]()
	if typ == reflect.TypeFor[RequestInput]() {
		return any(DecodeWith(func(_ context.Context, req *Request) (RequestInput, error) {
			if req == nil || req.Request == nil {
				return RequestInput{}, errors.New("ghttp/v2: request input requires a request")
			}
			return RequestInput{Request: req}, nil
		})).(Input[T]), nil
	}
	if typ == reflect.TypeFor[*RequestInput]() {
		return any(DecodeWith(func(_ context.Context, req *Request) (*RequestInput, error) {
			if req == nil || req.Request == nil {
				return nil, errors.New("ghttp/v2: request input requires a request")
			}
			return &RequestInput{Request: req}, nil
		})).(Input[T]), nil
	}
	root := typ
	if root.Kind() == reflect.Pointer {
		root = root.Elem()
	}
	if root.Kind() != reflect.Struct {
		return JSONInput[T](), nil
	}
	if !hasBindingTags(root, map[reflect.Type]bool{}) {
		return JSONInput[T](), nil
	}
	plan := bindingPlan{}
	if err := compileBindingFields(root, nil, map[reflect.Type]bool{}, &plan); err != nil {
		return Input[T]{}, err
	}
	if len(plan.fields) == 0 && plan.bodyTag == "" {
		return JSONInput[T](), nil
	}
	return CustomInput[T](bindingDecoder[T]{plan: plan}), nil
}

func hasBindingTags(typ reflect.Type, visiting map[reflect.Type]bool) bool {
	if visiting[typ] {
		return false
	}
	visiting[typ] = true
	defer delete(visiting, typ)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		for _, key := range []string{"path", "query", "header", "cookie", "body"} {
			if _, ok := f.Tag.Lookup(key); ok {
				return true
			}
		}
		if f.PkgPath != "" {
			continue
		}
		nested := f.Type
		if nested.Kind() == reflect.Pointer {
			nested = nested.Elem()
		}
		if nested.Kind() == reflect.Struct && !reflect.PointerTo(nested).Implements(textUnmarshalerType) && hasBindingTags(nested, visiting) {
			return true
		}
	}
	return false
}

func compileBindingFields(typ reflect.Type, prefix []int, visiting map[reflect.Type]bool, plan *bindingPlan) error {
	if visiting[typ] {
		return fmt.Errorf("ghttp/v2: recursive binding field type %s", typ)
	}
	visiting[typ] = true
	defer delete(visiting, typ)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			for _, key := range []string{"path", "query", "header", "cookie", "body"} {
				if _, ok := f.Tag.Lookup(key); ok {
					return fmt.Errorf("ghttp/v2: binding on unexported field %s", f.Name)
				}
			}
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		bound := false
		for _, item := range []struct {
			key    string
			source bindingSource
		}{
			{"path", sourcePath}, {"query", sourceQuery}, {"header", sourceHeader}, {"cookie", sourceCookie},
		} {
			if value, ok := f.Tag.Lookup(item.key); ok {
				name := strings.Split(value, ",")[0]
				if name == "" || name == "-" {
					return fmt.Errorf("ghttp/v2: invalid %s binding on field %s", item.key, f.Name)
				}
				if bound {
					return fmt.Errorf("ghttp/v2: multiple source bindings on field %s", f.Name)
				}
				bound = true
				setter, err := compileFieldSetter(f.Type)
				if err != nil {
					return fmt.Errorf("ghttp/v2: field %s: %w", f.Name, err)
				}
				setterOne, _ := compileFieldSetterOne(f.Type)
				plan.fields = append(plan.fields, bindingField{index: index, source: item.source, name: name, set: setter, setOne: setterOne})
			}
		}
		if value, ok := f.Tag.Lookup("body"); ok {
			codec := strings.Split(value, ",")[0]
			if plan.bodyTag != "" {
				return errors.New("ghttp/v2: multiple body bindings")
			}
			if bound {
				return fmt.Errorf("ghttp/v2: multiple bindings on field %s", f.Name)
			}
			if codec != "json" && codec != "xml" && codec != "form" && codec != "text" {
				return fmt.Errorf("ghttp/v2: unsupported body codec %q", codec)
			}
			if codec == "form" {
				form, err := compileFormPlan(f.Type)
				if err != nil {
					return fmt.Errorf("ghttp/v2: body field %s: %w", f.Name, err)
				}
				plan.form = form
			}
			plan.body, plan.bodyTag = index, codec
			bound = true
		}
		if !bound {
			nested := f.Type
			if nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			if nested.Kind() == reflect.Struct && !reflect.PointerTo(nested).Implements(textUnmarshalerType) {
				if err := compileBindingFields(nested, index, visiting, plan); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
