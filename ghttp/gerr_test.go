package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sofiworker/gk/gerr"
)

func TestHTTPErrorUnwrapTraversesToGerr(t *testing.T) {
	ge := gerr.New("db down", gerr.WithKind(gerr.KindUnavailable))
	he := Err(http.StatusInternalServerError, "internal", WithCause(ge))

	var target *gerr.Error
	if !errors.As(he, &target) {
		t.Fatal("errors.As through HTTPError should reach gerr.Error")
	}
	if target != ge {
		t.Fatalf("target = %p, want %p", target, ge)
	}
	if !gerr.IsKind(he, gerr.KindUnavailable) {
		t.Fatal("gerr.IsKind through HTTPError should match")
	}
}

func TestFromGerrMapsKindToStatus(t *testing.T) {
	tests := []struct {
		kind   gerr.Kind
		status int
	}{
		{gerr.KindInvalid, http.StatusBadRequest},
		{gerr.KindNotFound, http.StatusNotFound},
		{gerr.KindConflict, http.StatusConflict},
		{gerr.KindPermission, http.StatusForbidden},
		{gerr.KindUnavailable, http.StatusServiceUnavailable},
		{gerr.KindTimeout, http.StatusGatewayTimeout},
		{gerr.KindCanceled, http.StatusRequestTimeout},
		{gerr.KindInternal, http.StatusInternalServerError},
		{gerr.KindUnknown, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			he, ok := FromGerr(gerr.New("boom", gerr.WithKind(tt.kind)))
			if !ok {
				t.Fatal("FromGerr should convert gerr.Error")
			}
			if he.Code != tt.status {
				t.Fatalf("status = %d, want %d", he.Code, tt.status)
			}
			if he.Message != "boom" {
				t.Fatalf("message = %q, want boom", he.Message)
			}
		})
	}

	if _, ok := FromGerr(errors.New("plain")); ok {
		t.Fatal("FromGerr should reject non-gerr errors")
	}
}

func TestToGerrMapsStatusToKind(t *testing.T) {
	tests := []struct {
		status int
		kind   gerr.Kind
	}{
		{http.StatusBadRequest, gerr.KindInvalid},
		{http.StatusUnauthorized, gerr.KindPermission},
		{http.StatusForbidden, gerr.KindPermission},
		{http.StatusNotFound, gerr.KindNotFound},
		{http.StatusConflict, gerr.KindConflict},
		{http.StatusRequestTimeout, gerr.KindTimeout},
		{http.StatusGatewayTimeout, gerr.KindTimeout},
		{http.StatusServiceUnavailable, gerr.KindUnavailable},
		{http.StatusTeapot, gerr.KindInternal},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			ge, ok := ToGerr(Err(tt.status, "boom"))
			if !ok {
				t.Fatal("ToGerr should convert HTTPError")
			}
			if !gerr.IsKind(ge, tt.kind) {
				t.Fatalf("kind = %q, want %q", ge.Kind, tt.kind)
			}
			if ge.Code != strconv.Itoa(tt.status) {
				t.Fatalf("code = %q, want %q", ge.Code, strconv.Itoa(tt.status))
			}
		})
	}
}

func TestServerWritesGerrStatusAndMessage(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/missing").To(func(context.Context, struct{}) (struct{}, error) {
		he, _ := FromGerr(gerr.New("user not found", gerr.WithKind(gerr.KindNotFound)))
		return struct{}{}, he
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "user not found") {
		t.Fatalf("body = %q, want user not found", w.Body.String())
	}
}
