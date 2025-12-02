package gclient

import (
	"net/http"
	"testing"
)

func TestResponseHelpers(t *testing.T) {
	resp := &Response{
		StatusCode: 201,
		Header:     http.Header{"X-Test": []string{"ok"}},
		Body:       []byte("hello"),
	}
	if !resp.IsSuccess() {
		t.Fatalf("expected success for 201")
	}
	if resp.HeaderGet("X-Test") != "ok" {
		t.Fatalf("unexpected header lookup")
	}
	if resp.String() != "hello" {
		t.Fatalf("unexpected body string")
	}
}
