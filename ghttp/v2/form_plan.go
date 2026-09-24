package v2

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

type formField struct {
	index  []int
	name   string
	set    fieldSetter
	setOne fieldSetterOne
	err    error
}

type formPlan struct {
	fields []formField
}

func compileFormPlan(typ reflect.Type) (formPlan, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return formPlan{}, fmt.Errorf("ghttp/v2: form body requires struct, got %s", typ)
	}
	var plan formPlan
	if err := compileFormFields(typ, nil, map[reflect.Type]bool{}, &plan, nil); err != nil {
		return formPlan{}, err
	}
	return plan, nil
}

func compileFormFields(typ reflect.Type, prefix []int, visiting map[reflect.Type]bool, plan *formPlan, selectField func(reflect.StructField, []int, string) bool) error {
	if visiting[typ] {
		return fmt.Errorf("ghttp/v2: recursive form field type %s", typ)
	}
	visiting[typ] = true
	defer delete(visiting, typ)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			continue
		}
		index := append(append([]int(nil), prefix...), i)
		name := strings.Split(f.Tag.Get("form"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.Split(f.Tag.Get("json"), ",")[0]
		}
		if name == "" || name == "-" {
			name = f.Name
		}
		if selectField != nil && selectField(f, index, name) {
			continue
		}
		nested := f.Type
		if nested.Kind() == reflect.Pointer {
			nested = nested.Elem()
		}
		if nested.Kind() == reflect.Struct && !reflect.PointerTo(nested).Implements(textUnmarshalerType) {
			if err := compileFormFields(nested, index, visiting, plan, selectField); err != nil {
				return err
			}
			continue
		}
		setter, err := compileFieldSetter(f.Type)
		one, _ := compileFieldSetterOne(f.Type)
		plan.fields = append(plan.fields, formField{index: index, name: name, set: setter, setOne: one, err: err})
	}
	return nil
}

func bindFormPlan(dst reflect.Value, values url.Values, plan formPlan) error {
	for _, f := range plan.fields {
		raw := values[f.name]
		if len(raw) == 0 {
			continue
		}
		if f.err != nil {
			return fmt.Errorf("ghttp/v2: form field %s: %w", f.name, f.err)
		}
		field := fieldValue(dst, f.index)
		var err error
		if f.setOne != nil {
			err = f.setOne(field, raw[0])
		} else {
			err = f.set(field, raw)
		}
		if err != nil {
			return fmt.Errorf("ghttp/v2: form field %s: %w", f.name, err)
		}
	}
	return nil
}
