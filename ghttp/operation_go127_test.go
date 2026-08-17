//go:build go1.27

package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGo127EndpointHandleInfersOperationTypes(t *testing.T) {
	operation := Get("/users/{id}").Handle(
		PathInt64("id"),
		JSONOutput[operationUser](),
		func(_ context.Context, id int64) (operationUser, error) {
			return operationUser{ID: id}, nil
		},
	)

	server := New()
	server.MustMount(operation)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"id":42}` {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestGo127PackageHandleRemainsAvailable(t *testing.T) {
	operation := Handle(
		Get("/health"),
		NoInput(),
		TextOutput(),
		func(_ context.Context, _ EmptyInput) (string, error) { return "ok", nil },
	)

	server := New()
	server.MustMount(operation)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}
