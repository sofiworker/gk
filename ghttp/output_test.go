package ghttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnvelopeDefault_Success(t *testing.T) {
	app := New()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	app.envelope(&responseContext{w: w, r: r, codecMgr: app.codecMgr},
		http.StatusOK, map[string]string{"id": "1"}, nil, app.codecMgr)

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
	app := New()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	app.envelope(&responseContext{w: w, r: r, codecMgr: app.codecMgr},
		http.StatusNotFound, nil, Err(http.StatusNotFound, "user not found"), app.codecMgr)

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

type statusCodeOutput struct {
	Code int
}

func (o statusCodeOutput) StatusCode() int {
	return o.Code
}

func TestResolveStatusCodeUsesStatusCoder(t *testing.T) {
	if got := resolveStatusCode(statusCodeOutput{Code: http.StatusCreated}); got != http.StatusCreated {
		t.Fatalf("status = %d, want %d", got, http.StatusCreated)
	}
	if got := resolveStatusCode(statusCodeOutput{}); got != http.StatusOK {
		t.Fatalf("zero status = %d, want %d", got, http.StatusOK)
	}
}

func TestResolveStatusCodeUsesCachedStatusField(t *testing.T) {
	type response struct {
		Status int
	}

	if got := resolveStatusCode(&response{Status: http.StatusAccepted}); got != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", got, http.StatusAccepted)
	}
	if got := resolveStatusCode(&response{}); got != http.StatusOK {
		t.Fatalf("zero status = %d, want %d", got, http.StatusOK)
	}
	if got := resolveStatusCode((*response)(nil)); got != http.StatusOK {
		t.Fatalf("nil status = %d, want %d", got, http.StatusOK)
	}
}
