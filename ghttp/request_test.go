package ghttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"unsafe"
)

// TestRequest_QueryCache 验证 Query() 首次调用解析 URL.Query(),后续复用同一指针。
func TestRequest_QueryCache(t *testing.T) {
	req := &Request{Request: httptest.NewRequest(http.MethodGet, "/?a=1&b=2", nil)}
	q1 := req.Query()
	if q1.Get("a") != "1" {
		t.Fatal("first Query failed")
	}
	q2 := req.Query()
	if *(*unsafe.Pointer)(unsafe.Pointer(&q1)) != *(*unsafe.Pointer)(unsafe.Pointer(&q2)) {
		t.Errorf("second Query returned different pointer; cache not working")
	}
}
