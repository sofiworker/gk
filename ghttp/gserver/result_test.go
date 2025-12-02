package gserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newResultContext() (*Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx := &Context{
		Writer:       wrapResponseWriter(rec),
		handlerIndex: -1,
		pathParams:   make(map[string]string),
		valueCtx:     context.Background(),
	}
	return ctx, rec
}

func TestJsonResult(t *testing.T) {
	ctx, rec := newResultContext()

	JSON(map[string]string{"foo": "bar"}).Execute(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"foo":"bar"`) {
		t.Fatalf("unexpected body %q", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected json content type, got %q", ct)
	}
}

func TestStringAndHTMLResult(t *testing.T) {
	ctx, rec := newResultContext()
	String("hello %s", "gk").Execute(ctx)

	if rec.Code != http.StatusOK || rec.Body.String() != "hello gk" {
		t.Fatalf("unexpected string result code=%d body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("expected text/plain, got %q", ct)
	}

	ctx2, rec2 := newResultContext()
	HTML("page", "body").Execute(ctx2)
	if rec2.Code != http.StatusOK || rec2.Body.Len() == 0 {
		t.Fatalf("unexpected html result code=%d body=%q", rec2.Code, rec2.Body.String())
	}
}

func TestErrorRedirectAndEmptyResult(t *testing.T) {
	ctx, rec := newResultContext()
	Error(errors.New("boom")).Execute(ctx)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("unexpected error body %q", rec.Body.String())
	}

	ctx2, rec2 := newResultContext()
	Redirect("https://example.com").Execute(ctx2)
	if rec2.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec2.Code)
	}
	if loc := rec2.Header().Get("Location"); loc != "https://example.com" {
		t.Fatalf("unexpected location %q", loc)
	}

	ctx3, rec3 := newResultContext()
	(&EmptyResult{}).Execute(ctx3)
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec3.Code)
	}
	if rec3.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", rec3.Body.String())
	}
}
