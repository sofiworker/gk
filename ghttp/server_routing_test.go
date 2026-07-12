package ghttp

import (
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
	Route[struct{}, struct{}](server).
		GET("/health").
		ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != "ok" {
		t.Fatalf("body = %q, want ok", got)
	}

	assertRoutePanic(t, ErrServerFrozen, func() {
		Route[struct{}, struct{}](server).
			GET("/after-freeze").
			ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	})
}

func TestServerReportsCompiledMethodOutcomes(t *testing.T) {
	t.Parallel()

	server := New()
	Route[struct{}, struct{}](server).
		GET("/resources/{id}").
		ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))

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
	server.Use(func(next http.Handler) http.Handler { return next })
}

func TestServerStrictRoutingDistinguishesTrailingSlash(t *testing.T) {
	t.Parallel()

	server := New(WithStrictRouting())
	Route[struct{}, struct{}](server).
		GET("/health").
		ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))

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

func TestServerFreezeRejectsExistingRouteBuilderMutation(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	builder := Route[struct{}, struct{}](server)
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	assertRoutePanic(t, ErrServerFrozen, func() {
		builder.GET("/after-freeze")
	})
}

func TestServerFreezeFailureIsTerminal(t *testing.T) {
	t.Parallel()

	want := errors.New("middleware factory failure")
	server := New()
	calls := 0
	server.Use(func(http.Handler) http.Handler {
		calls++
		panic(want)
	})

	for attempt := 0; attempt < 2; attempt++ {
		assertRoutePanic(t, want, func() {
			server.finalizeRoutes()
		})
	}
	if calls != 1 {
		t.Fatalf("middleware factory calls = %d, want 1", calls)
	}
}

func TestServerDoesNotRecoverRouteFreezePanic(t *testing.T) {
	t.Parallel()

	want := errors.New("middleware factory failure")
	server := New()
	server.Use(func(http.Handler) http.Handler {
		panic(want)
	})

	assertRoutePanic(t, want, func() {
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
}

func TestGroupConcurrentUseRetainsEveryMiddleware(t *testing.T) {
	server := New()
	group := server.Group("/api")
	const writers = 64
	var calls atomic.Int64

	Route[struct{}, struct{}](group).
		GET("/health").
		ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for range writers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			group.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					next.ServeHTTP(w, r)
				})
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
			Route[struct{}, struct{}](server).
				GET("/concurrent").
				ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNoContent)
				}))
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
