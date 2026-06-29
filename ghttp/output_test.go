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
