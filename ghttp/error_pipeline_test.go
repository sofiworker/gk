package ghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sofiworker/gk/gerr"
)

func TestTypedHandlerUsesStructuredErrorPipeline(t *testing.T) {
	server := New(WithProduces(MIMEJSON), WithErrorRenderer(JSONErrorRenderer()))
	Route[struct{}, struct{}](server).GET("/users").To(func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, gerr.New("user.not_found", gerr.KindNotFound, gerr.WithParam("user_id", 42))
	})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users", nil))
	var document ErrorDocument
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != 404 || document.MessageID != "user.not_found" || document.Args["user_id"] != float64(42) {
		t.Fatalf("response = %d %#v", recorder.Code, document)
	}
}

func TestUnknownHandlerErrorDoesNotLeakText(t *testing.T) {
	server := New(WithProduces(MIMEJSON), WithErrorRenderer(JSONErrorRenderer()))
	Route[struct{}, struct{}](server).GET("/secret").To(func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, errors.New("password=secret")
	})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/secret", nil))
	if recorder.Code != 500 || recorder.Body.String() == "" || containsString(recorder.Body.String(), "password") {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestRespondErrorWithoutServerUsesSafeFallback(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	RespondError(recorder, request, errors.New("password=secret"))
	if recorder.Code != 500 || containsString(recorder.Body.String(), "password") {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func containsString(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
