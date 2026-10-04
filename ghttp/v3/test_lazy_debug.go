package v3

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLazyDebug(t *testing.T) {
	s := NewServer()
	handler := func(ctx context.Context, req RequestOf[*apiData]) (string, error) {
		fmt.Println("✅ Handler called")
		return "ok", nil
	}
	
	if err := s.Register(
		Post("/required", handler, WithInput(RequireBody(StrictJSONInput[*apiData]()))),
	); err != nil {
		t.Fatal(err)
	}
	
	// 测试空 body (应该返回 400)
	req := httptest.NewRequest("POST", "/required", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	
	s.ServeHTTP(resp, req)
	
	t.Logf("Status: %d (expected 400), Body: %s", resp.Code, resp.Body.String())
	
	if resp.Code != 400 {
		t.Errorf("Expected 400, got %d", resp.Code)
	}
}
