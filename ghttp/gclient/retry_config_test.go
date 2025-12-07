package gclient

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestExponentialBackoff(t *testing.T) {
	bo := ExponentialBackoff(10 * time.Millisecond)
	for i := 0; i < 5; i++ {
		d := bo(i)
		min := 10 * time.Millisecond * time.Duration(1<<uint(i))
		if d < min {
			t.Errorf("attempt %d: expected >= %v, got %v", i, min, d)
		}
	}
}

func TestDefaultRetryCondition(t *testing.T) {
	if !DefaultRetryCondition(nil, fmt.Errorf("err")) {
		t.Error("should retry on error")
	}
	
	resp := &Response{StatusCode: 200}
	if DefaultRetryCondition(resp, nil) {
		t.Error("should not retry on 200")
	}

	resp.StatusCode = 500
	if !DefaultRetryCondition(resp, nil) {
		t.Error("should retry on 500")
	}

	resp.StatusCode = http.StatusTooManyRequests
	if !DefaultRetryCondition(resp, nil) {
		t.Error("should retry on 429")
	}
}
