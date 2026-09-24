package v2

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type bindingTextValue int

func (v *bindingTextValue) UnmarshalText(raw []byte) error {
	n, err := strconv.Atoi(string(raw))
	if err != nil {
		return err
	}
	*v = bindingTextValue(n + 1)
	return nil
}

type bindingTextStruct struct{ Value string }

func (v *bindingTextStruct) UnmarshalText(raw []byte) error {
	if len(raw) == 0 {
		return errors.New("empty text")
	}
	v.Value = strings.ToUpper(string(raw))
	return nil
}

type bindingInput struct {
	ID    int       `path:"id"`
	Term  string    `query:"q"`
	Token string    `header:"X-Token"`
	SID   string    `cookie:"sid"`
	Body  bodyInput `body:"json"`
}

type bodyInput struct {
	Name string `json:"name"`
}

func TestDefaultInputBindingSourcesAndJSONBody(t *testing.T) {
	in, err := defaultInput[bindingInput]()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/?q=hello", strings.NewReader(`{"name":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", "secret")
	req.AddCookie(httptestCookie("sid", "cookie"))
	var r Request
	r.Request = req
	var got bindingInput
	if err := in.Decode(&r, &got); err != nil {
		t.Fatal(err)
	}
	if got.Term != "hello" || got.Token != "secret" || got.SID != "cookie" || got.Body.Name != "alice" {
		t.Fatalf("got %+v", got)
	}
}

func TestDefaultInputPointerAndOptionalScalar(t *testing.T) {
	type input struct {
		ID *int `query:"id"`
	}
	in, err := defaultInput[*input]()
	if err != nil {
		t.Fatal(err)
	}
	r := &Request{Request: httptest.NewRequest("GET", "/?id=7", nil)}
	var got *input
	if err := in.Decode(r, &got); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID == nil || *got.ID != 7 {
		t.Fatalf("got %#v", got)
	}
}

func TestDefaultInputNoTagsUsesJSON(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	in, err := defaultInput[input]()
	if err != nil || in.ContentType() != "application/json" {
		t.Fatalf("input=%+v err=%v", in, err)
	}
}

func TestDefaultInputInvalidBodyTags(t *testing.T) {
	type bad struct {
		A string `body:"yaml"`
	}
	if _, err := defaultInput[bad](); err == nil {
		t.Fatal("expected codec error")
	}
	type many struct {
		A string `body:"json"`
		B string `body:"xml"`
	}
	if _, err := defaultInput[many](); err == nil {
		t.Fatal("expected duplicate body error")
	}
}

func TestDefaultInputFormBody(t *testing.T) {
	type input struct {
		Form struct {
			Name string `form:"name"`
		} `body:"form"`
	}
	in, err := defaultInput[input]()
	if err != nil {
		t.Fatal(err)
	}
	raw := httptest.NewRequest("POST", "/", strings.NewReader("name=alice"))
	raw.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r := &Request{Request: raw}
	var got input
	if err := in.Decode(r, &got); err != nil {
		t.Fatal(err)
	}
	if got.Form.Name != "alice" {
		t.Fatalf("got %+v", got)
	}
}

func httptestCookie(name, value string) *http.Cookie { return &http.Cookie{Name: name, Value: value} }

func TestDefaultInputNestedBindingsAndSlices(t *testing.T) {
	type level struct {
		Term   *string            `query:"q"`
		Values []bindingTextValue `query:"v"`
		Flags  []bool             `header:"X-Flag"`
		Token  *bindingTextStruct `cookie:"token"`
	}
	type input struct {
		Level *level
		ID    []int `path:"id"`
	}
	in, err := defaultInput[input]()
	if err != nil {
		t.Fatal(err)
	}
	missing := &Request{Request: httptest.NewRequest("GET", "/", nil)}
	var absent input
	if err := in.Decode(missing, &absent); err != nil || absent.Level != nil {
		t.Fatalf("missing fields: value=%+v err=%v", absent, err)
	}
	req := httptest.NewRequest("GET", "/?q=&v=2&v=3", nil)
	req.Header.Add("X-Flag", "true")
	req.Header.Add("X-Flag", "false")
	req.AddCookie(httptestCookie("token", "hello"))
	var got input
	if err := in.Decode(&Request{Request: req}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Level == nil || got.Level.Term == nil || *got.Level.Term != "" ||
		!reflect.DeepEqual(got.Level.Values, []bindingTextValue{3, 4}) ||
		!reflect.DeepEqual(got.Level.Flags, []bool{true, false}) ||
		got.Level.Token == nil || got.Level.Token.Value != "HELLO" {
		t.Fatalf("got %+v", got)
	}
}

func TestDefaultInputBindingFailurePreservesValues(t *testing.T) {
	type input struct {
		Nums []int              `query:"n"`
		Text *bindingTextStruct `query:"text"`
		More *[]uint8           `query:"more"`
	}
	in, err := defaultInput[input]()
	if err != nil {
		t.Fatal(err)
	}
	got := input{Nums: []int{9}}
	err = in.Decode(&Request{Request: httptest.NewRequest("GET", "/?n=1&n=oops", nil)}, &got)
	if err == nil || !strings.Contains(err.Error(), "item 1") || !reflect.DeepEqual(got.Nums, []int{9}) {
		t.Fatalf("value=%+v err=%v", got, err)
	}
	err = in.Decode(&Request{Request: httptest.NewRequest("GET", "/?text=", nil)}, &got)
	if err == nil || got.Text != nil {
		t.Fatalf("value=%+v err=%v", got, err)
	}
	err = in.Decode(&Request{Request: httptest.NewRequest("GET", "/?more=2&more=3", nil)}, &got)
	if err != nil || got.More == nil || !reflect.DeepEqual(*got.More, []uint8{2, 3}) {
		t.Fatalf("value=%+v err=%v", got, err)
	}
}

func TestDefaultInputRejectsRecursiveType(t *testing.T) {
	type node struct {
		ID   int `query:"id"`
		Next *node
	}
	type input struct {
		Node   *node
		hidden *node
	}
	if _, err := defaultInput[input](); err == nil || !strings.Contains(err.Error(), "recursive") {
		t.Fatalf("expected recursive type error, got %v", err)
	}
}

func TestDefaultInputRecursiveWithoutBindingsUsesJSON(t *testing.T) {
	type node struct {
		Name string `json:"name"`
		Next *node  `json:"next"`
	}
	in, err := defaultInput[node]()
	if err != nil || in.ContentType() != "application/json" {
		t.Fatalf("input=%+v err=%v", in, err)
	}
	var got node
	req := &Request{Request: httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"root","next":{"name":"child"}}`))}
	if err := in.Decode(req, &got); err != nil || got.Name != "root" || got.Next == nil || got.Next.Name != "child" {
		t.Fatalf("value=%+v err=%v", got, err)
	}
}

func TestDefaultInputRejectsUnexportedTaggedField(t *testing.T) {
	type input struct {
		hidden int `query:"id"`
	}
	if _, err := defaultInput[input](); err == nil || !strings.Contains(err.Error(), "unexported") {
		t.Fatalf("expected unexported field error, got %v", err)
	}
}

func TestDefaultInputSkipsUnexportedPointers(t *testing.T) {
	type nested struct {
		ID int `query:"id"`
	}
	type input struct {
		Nested *nested
		hidden *nested
	}
	in, err := defaultInput[input]()
	if err != nil {
		t.Fatal(err)
	}
	var got input
	err = in.Decode(&Request{Request: httptest.NewRequest("GET", "/?id=4", nil)}, &got)
	if err != nil || got.Nested == nil || got.Nested.ID != 4 || got.hidden != nil {
		t.Fatalf("value=%+v err=%v", got, err)
	}
}
