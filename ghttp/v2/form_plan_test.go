package v2

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestFormPlanNestedPointersAndConversions(t *testing.T) {
	type inner struct {
		Amounts *[]uint16          `form:"amount"`
		Code    *bindingTextStruct `json:"code"`
	}
	type input struct {
		Inner   *inner
		Ignored string `form:"-"`
	}
	plan, err := compileFormPlan(reflect.TypeFor[input]())
	if err != nil {
		t.Fatal(err)
	}
	var got input
	if err := bindFormPlan(reflect.ValueOf(&got).Elem(), url.Values{}, plan); err != nil || got.Inner != nil {
		t.Fatalf("missing values: value=%+v err=%v", got, err)
	}
	values := url.Values{"amount": {"2", "3"}, "code": {"hi"}, "Ignored": {"bad"}}
	if err := bindFormPlan(reflect.ValueOf(&got).Elem(), values, plan); err != nil {
		t.Fatal(err)
	}
	if got.Inner == nil || got.Inner.Amounts == nil || !reflect.DeepEqual(*got.Inner.Amounts, []uint16{2, 3}) ||
		got.Inner.Code == nil || got.Inner.Code.Value != "HI" || got.Ignored != "" {
		t.Fatalf("value=%+v", got)
	}
}

func TestFormPlanRejectsUnsupportedTypeAndPreservesSlice(t *testing.T) {
	type invalid struct {
		Value chan int `form:"value"`
	}
	unsupported, err := compileFormPlan(reflect.TypeFor[invalid]())
	if err != nil {
		t.Fatal(err)
	}
	var invalidValue invalid
	if err := bindFormPlan(reflect.ValueOf(&invalidValue).Elem(), url.Values{}, unsupported); err != nil {
		t.Fatal(err)
	}
	if err := bindFormPlan(reflect.ValueOf(&invalidValue).Elem(), url.Values{"value": {"1"}}, unsupported); err == nil {
		t.Fatal("expected unsupported field error when present")
	}
	type input struct {
		Numbers []int `form:"number"`
	}
	plan, err := compileFormPlan(reflect.TypeFor[input]())
	if err != nil {
		t.Fatal(err)
	}
	got := input{Numbers: []int{9}}
	err = bindFormPlan(reflect.ValueOf(&got).Elem(), url.Values{"number": {"1", "bad"}}, plan)
	if err == nil || !strings.Contains(err.Error(), "item 1") || !reflect.DeepEqual(got.Numbers, []int{9}) {
		t.Fatalf("value=%+v err=%v", got, err)
	}
}

func TestFormPlanRejectsRecursiveType(t *testing.T) {
	type recursive struct {
		Next *recursive
	}
	if _, err := compileFormPlan(reflect.TypeFor[recursive]()); err == nil || !strings.Contains(err.Error(), "recursive") {
		t.Fatalf("expected recursive type error, got %v", err)
	}
}

func TestDefaultInputNestedFormBodyPlan(t *testing.T) {
	type body struct {
		Numbers []int `form:"number"`
	}
	type input struct {
		Nested *struct {
			Body *body `body:"form"`
		}
	}
	in, err := defaultInput[input]()
	if err != nil {
		t.Fatal(err)
	}
	var got input
	req := &Request{Request: httptest.NewRequest("POST", "/", strings.NewReader("number=1&number=2"))}
	if err := in.Decode(req, &got); err != nil {
		t.Fatal(err)
	}
	if got.Nested == nil || got.Nested.Body == nil || !reflect.DeepEqual(got.Nested.Body.Numbers, []int{1, 2}) {
		t.Fatalf("value=%+v", got)
	}
}
