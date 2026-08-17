package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type renderResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type statusRenderResp struct {
	V string `json:"v"`
}

func (s statusRenderResp) StatusCode() int { return http.StatusCreated }

func TestRenderJSONForcesJSON(t *testing.T) {
	app := New(WithProduces(MIMEXML)) // 默认只声明 XML,显式 JSON 应覆盖
	app.MustMount(Handle(Get("/r"), StructInput[struct{}](), RenderOutput[renderResp](), func(ctx context.Context, _ struct{}) (Render[renderResp], error) {
		return RenderJSON(renderResp{ID: "1", Name: "x"}), nil
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != MIMEJSON {
		t.Errorf("content-type = %q, want %q", ct, MIMEJSON)
	}
	if !strings.Contains(rec.Body.String(), `"id":"1"`) {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestRenderXMLForcesXML(t *testing.T) {
	app := New(WithProduces(MIMEJSON)) // 默认 JSON,显式 XML 应覆盖
	app.MustMount(Handle(Get("/r"), StructInput[struct{}](), RenderOutput[renderResp](), func(ctx context.Context, _ struct{}) (Render[renderResp], error) {
		return RenderXML(renderResp{ID: "1", Name: "x"}), nil
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != MIMEXML {
		t.Errorf("content-type = %q, want %q", ct, MIMEXML)
	}
	if !strings.Contains(rec.Body.String(), "<renderResp>") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestRenderRespectsAccept(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/r"), StructInput[struct{}](), RenderOutput[renderResp](), func(ctx context.Context, _ struct{}) (Render[renderResp], error) {
		return RenderJSON(renderResp{ID: "1"}), nil
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	r.Header.Set("Accept", MIMEXML) // 明确只接受 XML
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotAcceptable, rec.Body.String())
	}
}

func TestRenderBytesRawOutput(t *testing.T) {
	app := New()
	app.MustMount(Handle(Get("/r"), StructInput[struct{}](), RenderOutput[[]byte](), func(ctx context.Context, _ struct{}) (Render[[]byte], error) {
		return RenderBytes([]byte("hello, raw"), "text/custom"), nil
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/custom" {
		t.Errorf("content-type = %q, want %q", ct, "text/custom")
	}
	if rec.Body.String() != "hello, raw" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestRenderStatusCoderFromData(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	app.MustMount(Handle(Get("/r"), StructInput[struct{}](), RenderOutput[statusRenderResp](), func(ctx context.Context, _ struct{}) (Render[statusRenderResp], error) {
		return RenderJSON(statusRenderResp{V: "x"}), nil
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
}

func TestRenderWithEnvelope(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithEnvelope(DefaultEnvelope))
	app.MustMount(Handle(Get("/r"), StructInput[struct{}](), RenderOutput[renderResp](), func(ctx context.Context, _ struct{}) (Render[renderResp], error) {
		return RenderJSON(renderResp{ID: "1", Name: "x"}), nil
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/r", nil)
	app.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// Envelope 包的是解包后的 Data,不是 Render[T] 包装本身。
	// the envelope wraps the unwrapped Data, not the Render[T] wrapper.
	body := rec.Body.String()
	if !strings.Contains(body, `"data":{"id":"1","name":"x"}`) {
		t.Errorf("body = %q", body)
	}
}

func TestRenderTypeArgUnwraps(t *testing.T) {
	got := renderTypeArg(reflect.TypeOf(Render[renderResp]{}))
	if got != reflect.TypeOf(renderResp{}) {
		t.Errorf("renderTypeArg(Render) = %v, want %v", got, reflect.TypeOf(renderResp{}))
	}
	if got := renderTypeArg(reflect.TypeOf(renderResp{})); got != reflect.TypeOf(renderResp{}) {
		t.Errorf("renderTypeArg(non-Render) = %v", got)
	}
}
