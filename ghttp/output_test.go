package ghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnvelopeDefault_Success(t *testing.T) {
	app := New(WithEnvelope(DefaultEnvelope), WithProduces(MIMEJSON))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	app.envelope(w, r, http.StatusOK, map[string]string{"id": "1"}, nil, app.codecMgr)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["code"].(float64) != 0 {
		t.Fatalf("expected code=0, got %v", body["code"])
	}
	if body["msg"] != "success" {
		t.Fatalf("expected msg=success, got %v", body["msg"])
	}
}

func TestEnvelopeDefault_Error(t *testing.T) {
	app := New(WithEnvelope(DefaultEnvelope), WithProduces(MIMEJSON))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	app.envelope(w, r, http.StatusNotFound, nil, Err(http.StatusNotFound, "user not found"), app.codecMgr)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var body map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &body)
	code := body["code"].(float64)
	if int(code) != http.StatusNotFound {
		t.Fatalf("expected code=404, got %v", code)
	}
}

func TestErrorConstruction(t *testing.T) {
	err := Err(http.StatusConflict, "already exists", WithCause(ErrConflict))
	if err.Code != http.StatusConflict {
		t.Fatalf("expected code 409, got %d", err.Code)
	}
}

func TestTypedRouteAlwaysWritesOK(t *testing.T) {
	type response struct {
		Status int
	}

	server := New(WithProduces(MIMEJSON))
	Route[struct{}, response](server).GET("/created").To(func(context.Context, struct{}) (response, error) {
		return response{Status: http.StatusCreated}, nil
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/created", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}
