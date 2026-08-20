package ghttp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestURLEncodedFormAgainstJSONBodyReturns415(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	type input struct {
		Name string `json:"name"`
	}
	app.MustMount(Handle(Post("/users"), JSONBody[input](), JSONOutput[struct{}](), func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/users", bytes.NewBufferString("name=alice"))
	req.Header.Set("Content-Type", MIMEPOSTForm)
	app.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body = %s", w.Code, w.Body.String())
	}
}
