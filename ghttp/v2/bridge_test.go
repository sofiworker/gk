package v2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

func TestFromFuncReplyAndMetadata(t *testing.T) {
	route := FromFunc(http.MethodGet, "/reply", func(context.Context) (Reply[string], error) {
		return Reply[string]{Body: "ok", Status: http.StatusCreated, Headers: http.Header{"X-Test": {"yes"}}, Cookies: []*http.Cookie{{Name: "sid", Value: "1"}}}, nil
	})
	rec := httptest.NewRecorder()
	if err := route.Serve(context.Background(), &Request{Request: httptest.NewRequest(http.MethodGet, "/reply", nil)}, &Response{ResponseWriter: rec}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated || rec.Header().Get("X-Test") != "yes" || rec.Header().Get("Set-Cookie") == "" || rec.Body.String() != "\"ok\"\n" {
		t.Fatalf("response: code=%d headers=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
	}
}

func TestFromActionAndProcedure(t *testing.T) {
	action := FromAction(http.MethodPost, "/action", func(context.Context, struct{}) error { return nil })
	if err := action.Serve(context.Background(), &Request{Request: httptest.NewRequest(http.MethodPost, "/action", nil)}, &Response{ResponseWriter: httptest.NewRecorder()}); err != nil {
		t.Fatal(err)
	}
	procedure := FromProcedure(http.MethodGet, "/procedure", func(context.Context) error { return nil })
	if err := procedure.Serve(context.Background(), &Request{Request: httptest.NewRequest(http.MethodGet, "/procedure", nil)}, &Response{ResponseWriter: httptest.NewRecorder()}); err != nil {
		t.Fatal(err)
	}
}

func TestRouteMount(t *testing.T) {
	s := root.New()
	if err := FromFunc(http.MethodGet, "/mounted", func(context.Context) (string, error) { return "ok", nil }).Mount(s); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mounted", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "\"ok\"\n" {
		t.Fatalf("mounted response: %d %q", rec.Code, rec.Body.String())
	}
}

func TestReplyXMLAndGroup(t *testing.T) {
	type payload struct {
		Name string `xml:"name"`
	}
	r := FromFunc(http.MethodGet, "/reply", func(context.Context) (Reply[payload], error) {
		return Reply[payload]{Status: 201, Body: payload{Name: "alice"}}, nil
	}, WithOutput(ReplyOutput(XMLOutput[payload]())))
	s := root.New()
	if err := r.Mount(s.Group("/api")); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/api/reply", nil))
	if rec.Code != 201 || rec.Body.String() != "<payload><name>alice</name></payload>" || rec.Result().Header.Get("Content-Type") != "application/xml; charset=utf-8" {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), rec.Result().Header)
	}
}

func TestReplyInvalidStatus(t *testing.T) {
	r := FromFunc(http.MethodGet, "/", func(context.Context) (Reply[string], error) { return Reply[string]{Status: 99}, nil })
	resp := &Response{ResponseWriter: httptest.NewRecorder()}
	if err := r.Serve(context.Background(), &Request{Request: httptest.NewRequest("GET", "/", nil)}, resp); err == nil {
		t.Fatal("expected invalid status")
	}
	if resp.Written() {
		t.Fatal("response committed")
	}
}
