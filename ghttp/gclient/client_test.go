package gclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryCache struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemoryCache() *memoryCache {
	return &memoryCache{data: make(map[string][]byte)}
}

func (m *memoryCache) Get(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.data[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func (m *memoryCache) Set(key string, data []byte, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), data...)
}

type handlerExecutor struct {
	handler http.Handler
}

func (h *handlerExecutor) Do(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func newMockExecutor(t *testing.T, handler http.Handler) HTTPExecutor {
	t.Helper()
	return &handlerExecutor{handler: handler}
}

func TestClientRequestBuildAndDecode(t *testing.T) {
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users/9" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("q") != "go" || query.Get("lang") != "en" {
			t.Fatalf("unexpected query %v", query)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("unexpected content type %q", ct)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body error: %v", err)
		}
		if payload["name"] != "gk" {
			t.Fatalf("unexpected payload %+v", payload)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":   true,
			"name": payload["name"],
		})
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
	)
	resp, err := client.R().
		SetPathParam("id", "9").
		AddQueryParams(map[string]string{"q": "go", "lang": "en"}).
		SetBody(map[string]interface{}{"name": "gk"}).
		Post("/api/users/:id")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	if !resp.IsSuccess() {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}

	var out struct {
		OK   bool   `json:"ok"`
		Name string `json:"name"`
	}
	if err := resp.Decode(&out); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if !out.OK || out.Name != "gk" {
		t.Fatalf("unexpected response %+v", out)
	}
	if !strings.HasPrefix(resp.ContentType, "application/json") {
		t.Fatalf("unexpected response content type %q", resp.ContentType)
	}
}

func TestClientRetryAndMiddleware(t *testing.T) {
	var attempts int32
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"attempt": int(n)})
	}))

	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
		WithRetry(&RetryConfig{
			MaxRetries:      3,
			RetryConditions: []RetryCondition{DefaultRetryCondition},
			Backoff: func(int) time.Duration {
				return 0
			},
		}),
	)

	var reqMW, respMW int32
	client.UseRequest(func(_ *Client, _ *Request) error {
		atomic.AddInt32(&reqMW, 1)
		return nil
	})
	client.UseResponse(func(_ *Client, _ *Response) error {
		atomic.AddInt32(&respMW, 1)
		return nil
	})

	resp, err := client.R().Get("/retry")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	if reqMW != 1 {
		t.Fatalf("request middleware should run once, got %d", reqMW)
	}
	if respMW != 1 {
		t.Fatalf("response middleware should run once, got %d", respMW)
	}

	var result map[string]int
	if err := resp.Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if result["attempt"] != 3 {
		t.Fatalf("unexpected decoded attempt %+v", result)
	}
}

func TestClientCache(t *testing.T) {
	var hits int32
	executor := newMockExecutor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"hit": int(current)})
	}))

	cache := newMemoryCache()
	client := NewClient(
		WithBaseURL("http://example.com"),
		WithExecutor(executor),
		WithCache(cache),
	)

	resp1, err := client.R().UseCache("", time.Minute).Get("/cache")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	var first map[string]int
	if err := resp1.Decode(&first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	resp2, err := client.R().UseCache("", time.Minute).Get("/cache")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	var second map[string]int
	if err := resp2.Decode(&second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	if hits != 1 {
		t.Fatalf("cache not used, hits=%d", hits)
	}
	if first["hit"] != second["hit"] {
		t.Fatalf("cached content mismatch %+v vs %+v", first, second)
	}
}
