package v2

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type contractInput struct{ Name string }
type contractOutput struct{ Name string }

func TestEndpointSupportsValueAndPointerCombinations(t *testing.T) {
	var _ Endpoint[contractInput, contractOutput] = func(context.Context, contractInput) (contractOutput, error) {
		return contractOutput{}, nil
	}
	var _ Endpoint[*contractInput, contractOutput] = func(context.Context, *contractInput) (contractOutput, error) {
		return contractOutput{}, nil
	}
	var _ Endpoint[contractInput, *contractOutput] = func(context.Context, contractInput) (*contractOutput, error) {
		return nil, nil
	}
	var _ Endpoint[*contractInput, *contractOutput] = func(context.Context, *contractInput) (*contractOutput, error) {
		return nil, nil
	}
}

func TestReplyCarriesHTTPMetadata(t *testing.T) {
	reply := Reply[*contractOutput]{
		Body:    &contractOutput{Name: "ok"},
		Status:  http.StatusCreated,
		Headers: http.Header{"X-Test": {"ok"}},
		Cookies: []*http.Cookie{{Name: "sid", Value: "x"}},
	}
	if reply.Status != http.StatusCreated || reply.Headers.Get("X-Test") != "ok" || reply.Cookies[0].Name != "sid" {
		t.Fatalf("reply metadata was not preserved: %+v", reply)
	}
}

func TestCustomInputAndOutput(t *testing.T) {
	decoder := customDecoder{}
	input := CustomInput[contractInput](decoder)
	if !input.HasDecoder() || input.ContentType() != "application/example" {
		t.Fatalf("unexpected input metadata: decoder=%v type=%q", input.HasDecoder(), input.ContentType())
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	wrapped := &Request{Request: req}
	var got contractInput
	if err := input.Decode(wrapped, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "decoded" {
		t.Fatalf("decoded value = %+v", got)
	}

	output := CustomOutput[contractOutput](customEncoder{})
	if output.ContentType() != "application/example" || !output.HasBody() {
		t.Fatalf("unexpected output metadata: %+v", output)
	}
}

func TestNilContractsReturnErrors(t *testing.T) {
	var input Input[contractInput]
	if !errors.Is(input.Decode(nil, new(contractInput)), ErrNilDecoder) {
		t.Fatal("nil input decoder did not return ErrNilDecoder")
	}
	var output Output[contractOutput]
	if !errors.Is(output.Encode(nil, contractOutput{}), ErrNilEncoder) {
		t.Fatal("nil output encoder did not return ErrNilEncoder")
	}
}

func TestBuiltinFacadeMetadata(t *testing.T) {
	if JSONInput[contractInput]().ContentType() != "application/json" {
		t.Fatal("unexpected JSON input content type")
	}
	if XMLInput[contractInput]().ContentType() != "application/xml" {
		t.Fatal("unexpected XML input content type")
	}
	if TextInput[string]().ContentType() != "text/plain" {
		t.Fatal("unexpected text input content type")
	}
	if JSONOutput[contractOutput]().ContentType() != "application/json; charset=utf-8" {
		t.Fatal("unexpected JSON output content type")
	}
	if EmptyOutput().Status() != http.StatusNoContent || EmptyOutput().HasBody() {
		t.Fatal("unexpected empty output contract")
	}
	if HTMLOutput[string]().ContentType() != "text/html; charset=utf-8" {
		t.Fatal("unexpected HTML output content type")
	}
}

func TestFormInputDecodesURLValues(t *testing.T) {
	type formInput struct {
		Name   string   `form:"name"`
		Count  int      `form:"count"`
		Admin  bool     `form:"admin"`
		Labels []string `form:"label"`
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("name=alice&count=2&admin=true&label=a&label=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var got formInput
	if err := FormInput[formInput]().Decode(&Request{Request: req}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "alice" || got.Count != 2 || !got.Admin || len(got.Labels) != 2 {
		t.Fatalf("decoded form = %+v", got)
	}
}

func TestOutputEncodeAppliesStatusAndEmptyBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	resp := &Response{ResponseWriter: recorder}
	if err := JSONOutput[contractOutput]().WithStatus(http.StatusCreated).Encode(resp, contractOutput{Name: "ok"}); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusCreated || recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("response metadata: code=%d headers=%v", recorder.Code, recorder.Header())
	}

	emptyRecorder := httptest.NewRecorder()
	emptyResp := &Response{ResponseWriter: emptyRecorder}
	if err := EmptyOutput().Encode(emptyResp, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if emptyRecorder.Code != http.StatusNoContent || emptyRecorder.Body.Len() != 0 {
		t.Fatalf("empty response: code=%d body=%q", emptyRecorder.Code, emptyRecorder.Body.String())
	}
}

type customDecoder struct{}

func (customDecoder) ContentType() string { return "application/example" }
func (customDecoder) Decode(_ *Request, dst *contractInput) error {
	dst.Name = "decoded"
	return nil
}

type customEncoder struct{}

func (customEncoder) ContentType() string                        { return "application/example" }
func (customEncoder) Encode(_ *Response, _ contractOutput) error { return nil }
