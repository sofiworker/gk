package v2

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

type bodyInputPayload struct {
	Name string `json:"name" xml:"name"`
}

func TestRequestOfDefaultAndPointer(t *testing.T) {
	for _, pointer := range []bool{false, true} {
		s := NewServer()
		check := func(in RequestOf[*bodyInputPayload]) (string, error) {
			if in.Path("id") != "42" || in.HeaderValue("X-Test") != "header" || in.CookieValue("sid") != "cookie" {
				t.Fatal("request sources missing")
			}
			if value, present := in.QueryFirst("empty"); !present || value != "" {
				t.Fatal("empty query presence lost")
			}
			return in.Data.Name, nil
		}
		var route Route
		if pointer {
			route = Patch("/{id}", func(_ context.Context, in *RequestOf[*bodyInputPayload]) (string, error) { return check(*in) })
		} else {
			route = Patch("/{id}", func(_ context.Context, in RequestOf[*bodyInputPayload]) (string, error) { return check(in) })
		}
		if err := s.Group("/users").Register(route); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"alice", "bob"} {
			req := httptest.NewRequest("PATCH", "/users/42?empty=&page=invalid", strings.NewReader(`{"name":"`+name+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Test", "header")
			req.AddCookie(&http.Cookie{Name: "sid", Value: "cookie"})
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != 200 || rec.Body.String() != `"`+name+`"`+"\n" {
				t.Fatalf("%d %q", rec.Code, rec.Body.String())
			}
		}
	}
}

func TestRequestOfCodecsAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, media, payload string
		codec                Input[bodyInputPayload]
		limit                int64
		want                 int
	}{
		{"json", "application/json", `{"name":"alice"}`, JSONInput[bodyInputPayload](), 100, 200},
		{"xml", "application/xml", `<user><name>alice</name></user>`, XMLInput[bodyInputPayload](), 100, 200},
		{"strict", "application/json", `{"extra":1}`, StrictJSONInput[bodyInputPayload](), 100, 400},
		{"multiple", "application/json", `{} {}`, JSONInput[bodyInputPayload](), 100, 400},
		{"media", "text/plain", `{}`, JSONInput[bodyInputPayload](), 100, 415},
		{"limit", "application/json", `{"name":"alice"}`, JSONInput[bodyInputPayload](), 2, 413},
		{"required", "application/json", "", RequireBody(JSONInput[bodyInputPayload]()), 100, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			s := NewServer()
			err := s.Register(Post("/", func(_ context.Context, in RequestOf[bodyInputPayload]) (string, error) {
				called = true
				return in.Data.Name, nil
			}, WithInput(DecodeRequest(tc.codec)), WithBodyLimit(tc.limit)))
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.payload))
			req.Header.Set("Content-Type", tc.media)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != tc.want || called != (tc.want == 200) {
				t.Fatalf("status=%d called=%v", rec.Code, called)
			}
		})
	}
	bad := Post("/", func(context.Context, RequestOf[string]) (string, error) { return "", nil }, WithInput(DecodeRequest(Input[string]{})))
	if bad.Err() == nil {
		t.Fatal("invalid body codec accepted")
	}
	if _, err := compileInput(RequireBody(Input[string]{})); !errors.Is(err, ErrNilDecoder) {
		t.Fatal(err)
	}
}

func TestBodyValidatorOrderAndCause(t *testing.T) {
	var order []string
	marker := errors.New("invalid name")
	validate := func(_ context.Context, body bodyInputPayload) error {
		order = append(order, "body")
		if body.Name == "bad" {
			return marker
		}
		return nil
	}
	for _, pointer := range []bool{false, true} {
		var route Route
		if pointer {
			route = Post("/", func(context.Context, *RequestOf[bodyInputPayload]) (string, error) {
				order = append(order, "handler")
				return "ok", nil
			}, WithDataValidator(validate), WithValidator(func(context.Context, *RequestOf[bodyInputPayload]) error { order = append(order, "input"); return nil }))
		} else {
			route = Post("/", func(context.Context, RequestOf[bodyInputPayload]) (string, error) {
				order = append(order, "handler")
				return "ok", nil
			}, WithDataValidator(validate), WithValidator(func(context.Context, RequestOf[bodyInputPayload]) error { order = append(order, "input"); return nil }))
		}
		if err := route.Err(); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"ok", "bad"} {
			order = nil
			err := route.Serve(context.Background(), &Request{Request: httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"`+name+`"}`))}, &Response{ResponseWriter: httptest.NewRecorder()})
			if name == "bad" {
				if !errors.Is(err, marker) || !errors.Is(err, root.ErrInvalidInput) || !reflect.DeepEqual(order, []string{"body"}) {
					t.Fatalf("%v %v", err, order)
				}
			} else if err != nil || !reflect.DeepEqual(order, []string{"body", "input", "handler"}) {
				t.Fatalf("%v %v", err, order)
			}
		}
	}
	h := func(context.Context, RequestOf[bodyInputPayload]) (string, error) { return "", nil }
	if Post("/", h, WithDataValidator[string](nil)).Err() == nil {
		t.Fatal("mismatched validator accepted")
	}
	if Post("/", h, WithDataValidator[bodyInputPayload](nil)).Err() == nil {
		t.Fatal("nil validator accepted")
	}
	if Post("/", func(context.Context, RequestInput) (string, error) { return "", nil }, WithDataValidator(validate)).Err() == nil {
		t.Fatal("non-body input accepted")
	}
	if Raw("POST", "/", func(context.Context, *Request, *Response) error { return nil }, WithDataValidator(validate)).Err() == nil {
		t.Fatal("raw body validator accepted")
	}
}

func TestReadBodyOnDemand(t *testing.T) {
	codec := RequireBody(StrictJSONInput[*bodyInputPayload]())
	s := NewServer()
	if err := s.Register(Post("/", func(ctx context.Context, req RequestInput) (string, error) {
		if req.HeaderValue("X-Skip") == "yes" {
			return "skipped", nil
		}
		body, err := ReadBody(ctx, req, codec)
		if err != nil {
			return "", err
		}
		return body.Name, nil
	}, WithBodyLimit(20))); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		payload, media, skip string
		status               int
	}{
		{`{"name":"ok"}`, "application/json", "", 200},
		{"invalid", "application/json", "yes", 200},
		{"invalid", "application/json", "", 400},
		{`{}`, "text/plain", "", 415},
		{`{"name":"long long long long"}`, "application/json", "", 413},
		{"", "application/json", "", 400},
	} {
		req := httptest.NewRequest("POST", "/", strings.NewReader(tc.payload))
		req.Header.Set("Content-Type", tc.media)
		req.Header.Set("X-Skip", tc.skip)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%q: %d %s", tc.payload, rec.Code, rec.Body.String())
		}
	}
	if _, err := ReadBody(context.Background(), RequestInput{}, codec); !errors.Is(err, root.ErrInvalidInput) {
		t.Fatal(err)
	}
	request := RequestInput{Request: &Request{Request: httptest.NewRequest("POST", "/", nil)}}
	if _, err := ReadBody(context.Background(), request, Input[string]{}); !errors.Is(err, ErrNilDecoder) {
		t.Fatal(err)
	}
	if _, err := ReadBody(context.Background(), request, RequireBody(Input[string]{})); !errors.Is(err, ErrNilDecoder) {
		t.Fatal(err)
	}
}

func TestRequireBodyPresenceAndNull(t *testing.T) {
	codec := RequireBody(JSONInput[*bodyInputPayload]())
	for _, payload := range []string{"", "null", `{}`} {
		req := RequestInput{Request: &Request{Request: httptest.NewRequest("POST", "/", io.NopCloser(strings.NewReader(payload)))}}
		value, err := ReadBody(context.Background(), req, codec)
		if payload == "" && !errors.Is(err, ErrMissingBody) {
			t.Fatal(err)
		}
		if payload == "null" && (err != nil || value != nil) {
			t.Fatalf("%v %v", value, err)
		}
		if payload == "{}" && (err != nil || value == nil) {
			t.Fatalf("%v %v", value, err)
		}
	}
	req := RequestInput{Request: &Request{Request: httptest.NewRequest("POST", "/", strings.NewReader("null"))}}
	if value, err := ReadBody(context.Background(), req, JSONInput[*bodyInputPayload]()); err != nil || value != nil {
		t.Fatalf("manual decoder null semantics: %v %v", value, err)
	}
}

func TestRequestOfOptionalBodyPreservesNil(t *testing.T) {
	for _, codec := range []Input[RequestOf[*bodyInputPayload]]{DecodeRequest(JSONInput[*bodyInputPayload]()), DecodeRequest(RequireBody(JSONInput[*bodyInputPayload]()))} {
		for _, payload := range []string{"", "null", `{}`} {
			var in RequestOf[*bodyInputPayload]
			var reader io.Reader
			if payload != "" {
				reader = strings.NewReader(payload)
			}
			err := codec.Decode(&Request{Request: httptest.NewRequest("POST", "/", reader)}, &in)
			if payload == "" && codec.required {
				if !errors.Is(err, ErrMissingBody) {
					t.Fatal(err)
				}
				continue
			}
			if err != nil || (in.Data == nil) != (payload != `{}`) {
				t.Fatalf("%q: %v %v", payload, in.Data, err)
			}
		}
	}
}

func TestRequestOfOtherCodecsAndContext(t *testing.T) {
	type form struct {
		Name string `form:"name"`
	}
	var value RequestOf[form]
	req := &Request{Request: httptest.NewRequest("POST", "/", strings.NewReader("name=alice"))}
	if err := DecodeRequest(FormInput[form]()).Decode(req, &value); err != nil || value.Data.Name != "alice" || value.Request != req {
		t.Fatalf("%v %v", value, err)
	}
	var text RequestOf[string]
	if err := DecodeRequest(TextInput[string]()).Decode(&Request{Request: httptest.NewRequest("POST", "/", strings.NewReader("hello"))}, &text); err != nil || text.Data != "hello" {
		t.Fatalf("%v %v", text, err)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "value")
	codec := DecodeWith(func(c context.Context, _ *Request) (string, error) { return c.Value(contextKey{}).(string), nil })
	decode, err := compileInput(DecodeRequest(codec))
	if err != nil {
		t.Fatal(err)
	}
	custom, err := decode(ctx, req)
	if err != nil || custom.Data != "value" {
		t.Fatalf("%v %v", custom, err)
	}
	if _, err := DecodeRequest(JSONInput[string]()).read(ctx, nil); err == nil {
		t.Fatal("nil request accepted")
	}
	if _, err := compileInput(DecodeRequest(FormInput[int]())); err == nil {
		t.Fatal("invalid form codec accepted")
	}
}
