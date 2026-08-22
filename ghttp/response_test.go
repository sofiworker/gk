package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Response 状态追踪:WriteHeader 记录 status/written;首次 Write 隐式提交 200。
func TestResponseTracking(t *testing.T) {
	t.Run("WriteHeader", func(t *testing.T) {
		r := &Response{ResponseWriter: httptest.NewRecorder()}
		if r.Written() || r.Status() != 0 {
			t.Fatal("fresh Response should be unwritten with status 0")
		}
		r.WriteHeader(http.StatusTeapot)
		if !r.Written() || r.Status() != http.StatusTeapot {
			t.Errorf("after WriteHeader(418): written=%v status=%d", r.Written(), r.Status())
		}
		// 重复 WriteHeader 被忽略(首个生效)。
		r.WriteHeader(http.StatusOK)
		if r.Status() != http.StatusTeapot {
			t.Errorf("second WriteHeader leaked: status=%d, want 418", r.Status())
		}
	})

	t.Run("implicit200OnWrite", func(t *testing.T) {
		r := &Response{ResponseWriter: httptest.NewRecorder()}
		if _, err := r.Write([]byte("hi")); err != nil {
			t.Fatal(err)
		}
		if !r.Written() || r.Status() != http.StatusOK {
			t.Errorf("after Write: written=%v status=%d, want committed 200", r.Written(), r.Status())
		}
	})

	t.Run("reset", func(t *testing.T) {
		r := &Response{ResponseWriter: httptest.NewRecorder()}
		r.WriteHeader(http.StatusTeapot)
		r.reset()
		if r.Written() || r.Status() != 0 || r.ResponseWriter != nil {
			t.Errorf("after reset: written=%v status=%d writer=%v, want clean", r.Written(), r.Status(), r.ResponseWriter)
		}
	})
}

// 池化卫生:交替请求不因池化复用而串扰状态码(前一请求的 status 不泄漏给后一请求)。
func TestResponsePoolHygiene(t *testing.T) {
	m := New()
	// /a 写 418,/b 只写 200——若 reset 失效,复用同一 Request 时 /b 可能读到残留。
	if err := m.RawHandle(http.MethodGet, "/a", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusTeapot)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.RawHandle(http.MethodGet, "/b", func(ctx context.Context, req *Request, resp *Response) error {
		if resp.Written() {
			t.Error("/b saw a pre-written Response (pool leak)")
		}
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		doRequest(t, m, http.MethodGet, "/a")
		rec := doRequest(t, m, http.MethodGet, "/b")
		if rec.Code != http.StatusOK {
			t.Fatalf("/b status = %d, want 200", rec.Code)
		}
	}
}

// 并发命中 typed 路由,-race 下无竞争、参数无跨协程串扰。
// Concurrent hits on a typed route: no race under -race, params do not leak
// across goroutines.
func TestConcurrentTypedHits(t *testing.T) {
	type idParam struct {
		ID int64 `path:"id"`
	}
	m := New()
	if err := GetParams(m, "/users/{id}", JSON[int64](),
		func(ctx context.Context, p idParam) (int64, error) {
			return p.ID, nil
		}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				rec := httptest.NewRecorder()
				m.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/123", nil))
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, want 200", rec.Code)
					return
				}
				if got := rec.Body.String(); got != "123\n" {
					t.Errorf("body = %q, want \"123\\n\"", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// doRequest 辅助函数：发送请求并返回记录器供断言。
// doRequest is a helper that issues a request and returns the recorder for
// assertions.
func doRequest(t *testing.T, m http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	m.ServeHTTP(rec, req)
	return rec
}
