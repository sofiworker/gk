package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestServerFreezesCompiledRoutesOnFirstRequest(t *testing.T) {
	t.Parallel()

	server := New()
	server.MustMount(RawOperation(http.MethodGet, "/health",
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != "ok" {
		t.Fatalf("body = %q, want ok", got)
	}

	assertRoutePanic(t, ErrServerFrozen, func() {
		server.MustMount(RawOperation(http.MethodGet, "/after-freeze",
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	})
}

func TestServerReportsCompiledMethodOutcomes(t *testing.T) {
	t.Parallel()

	server := New()
	server.MustMount(RawOperation(http.MethodGet, "/resources/{id}",
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/resources/42", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if got := recorder.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want GET, HEAD", got)
	}

	notFound := httptest.NewRecorder()
	server.ServeHTTP(notFound, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want %d", notFound.Code, http.StatusNotFound)
	}
}

func TestServerRejectsRoutesAfterFreeze(t *testing.T) {
	t.Parallel()

	server := New()
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	defer func() {
		got := recover()
		err, ok := got.(error)
		if !ok || !errors.Is(err, ErrServerFrozen) {
			t.Fatalf("panic = %#v, want errors.Is(_, ErrServerFrozen)", got)
		}
	}()
	server.Use(func(c *Ctx) { c.Next() })
}

func TestServerStrictRoutingDistinguishesTrailingSlash(t *testing.T) {
	t.Parallel()

	server := New(WithStrictRouting())
	server.MustMount(RawOperation(http.MethodGet, "/health",
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})))

	withoutSlash := httptest.NewRecorder()
	server.ServeHTTP(withoutSlash, httptest.NewRequest(http.MethodGet, "/health", nil))
	if withoutSlash.Code != http.StatusNoContent {
		t.Fatalf("without slash status = %d, want %d", withoutSlash.Code, http.StatusNoContent)
	}

	withSlash := httptest.NewRecorder()
	server.ServeHTTP(withSlash, httptest.NewRequest(http.MethodGet, "/health/", nil))
	if withSlash.Code != http.StatusNotFound {
		t.Fatalf("with slash status = %d, want %d", withSlash.Code, http.StatusNotFound)
	}
}

func TestServerFreezeRejectsOperationMount(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	assertRoutePanic(t, ErrServerFrozen, func() {
		server.MustMount(GetJSON("/after-freeze", NoInput(), func(context.Context, EmptyInput) (struct{}, error) {
			return struct{}{}, nil
		}))
	})
}

// 新执行模型没有中间件工厂:Use 直接接收 HandlerFunc,freeze 只做纯编译,
// 不再调用用户代码。中间件 panic 发生在请求期,由 ServeHTTP 的 recover 兜底。
// the new execution model has no middleware factories: Use takes a HandlerFunc
// directly and freeze only compiles, so user code no longer runs at freeze.
// middleware panics happen at request time and are recovered by ServeHTTP.
func TestServerRecoversMiddlewarePanic(t *testing.T) {
	t.Parallel()

	want := errors.New("middleware panic at request time")
	server := New()
	server.Use(func(c *Ctx) {
		panic(want)
	})
	server.MustMount(GetJSON("/boom", NoInput(), func(context.Context, EmptyInput) (struct{}, error) {
		return struct{}{}, nil
	}))

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestGroupConcurrentUseRetainsEveryMiddleware(t *testing.T) {
	server := New()
	group := server.Group("/api")
	const writers = 64
	var calls atomic.Int64

	group.MustMount(RawOperation(http.MethodGet, "/health",
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))

	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for range writers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			group.Use(func(c *Ctx) {
				calls.Add(1)
				c.Next()
			})
		}()
	}
	close(start)
	waitGroup.Wait()

	if got := len(group.middlewares); got != writers {
		t.Fatalf("middleware count = %d, want %d", got, writers)
	}

	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if got := calls.Load(); got != writers {
		t.Fatalf("compiled middleware calls = %d, want %d", got, writers)
	}
}

func TestServerRegistrationFreezeIsAtomic(t *testing.T) {
	const attempts = 64
	for attempt := 0; attempt < attempts; attempt++ {
		server := New()
		start := make(chan struct{})
		registered := make(chan any, 1)
		frozen := make(chan struct{})

		go func() {
			defer func() { registered <- recover() }()
			<-start
			server.MustMount(RawOperation(http.MethodGet, "/concurrent",
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				})))
		}()
		go func() {
			<-start
			server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/concurrent", nil))
			close(frozen)
		}()

		close(start)
		registrationPanic := <-registered
		<-frozen
		definitions := server.registry.snapshot()
		switch registrationPanic {
		case nil:
			if len(definitions) != 1 {
				t.Fatalf("attempt %d: successful registration left %d definitions, want 1", attempt, len(definitions))
			}
		default:
			err, ok := registrationPanic.(error)
			if !ok || !errors.Is(err, ErrServerFrozen) {
				t.Fatalf("attempt %d: registration panic = %#v, want ErrServerFrozen", attempt, registrationPanic)
			}
			if len(definitions) != 0 {
				t.Fatalf("attempt %d: rejected registration left %d definitions, want 0", attempt, len(definitions))
			}
		}
	}
}
