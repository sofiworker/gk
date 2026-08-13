package ghttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerErrorHandlerReceivesSanitizedInternalError(t *testing.T) {
	t.Parallel()

	cause := errors.New("database password leaked")
	var received *HTTPError
	server := New(
		WithProduces(MIMEJSON),
		WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err *HTTPError) {
			received = err
			w.WriteHeader(http.StatusTeapot)
		}),
	)
	Route[struct{}, struct{}](server).
		GET("/broken").
		To(func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, cause
		})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/broken", nil))
	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTeapot)
	}
	if received == nil {
		t.Fatal("ErrorHandler did not receive an error")
	}
	if received.Message != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("message = %q, want generic 500 message", received.Message)
	}
	if !errors.Is(received.Err, cause) {
		t.Fatalf("cause = %v, want %v", received.Err, cause)
	}
}

func TestServerErrorHandlerHandlesRouteOutcomes(t *testing.T) {
	t.Parallel()

	var received []int
	server := New(WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err *HTTPError) {
		received = append(received, err.Code)
		w.WriteHeader(http.StatusTeapot)
	}))
	Route[struct{}, struct{}](server).
		GET("/users/{id}").
		ToHTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/missing", nil),
		httptest.NewRequest(http.MethodPost, "/users/42", nil),
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusTeapot {
			t.Fatalf("%s %s status = %d, want %d", request.Method, request.URL.Path, recorder.Code, http.StatusTeapot)
		}
	}
	if len(received) != 2 || received[0] != http.StatusNotFound || received[1] != http.StatusMethodNotAllowed {
		t.Fatalf("received codes = %#v, want [404 405]", received)
	}
}

func TestServerRecoversPanicsWithoutLeakingDetails(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](server).GET("/panic").To(func(context.Context, struct{}) (struct{}, error) {
		panic("secret detail")
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if body := recorder.Body.String(); body == "" || strings.Contains(body, "secret detail") {
		t.Fatalf("panic response body = %q", body)
	}
}

func TestServerFallsBackWhenErrorHandlerPanics(t *testing.T) {
	t.Parallel()

	server := New(
		WithProduces(MIMEJSON),
		WithErrorHandler(func(http.ResponseWriter, *http.Request, *HTTPError) {
			panic("error handler panic")
		}),
	)
	Route[struct{}, struct{}](server).GET("/panic").To(func(context.Context, struct{}) (struct{}, error) {
		panic("handler panic")
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestServerDoesNotReenterErrorHandlerAfterItsPanic(t *testing.T) {
	t.Parallel()

	calls := 0
	server := New(
		WithProduces(MIMEJSON),
		WithErrorHandler(func(http.ResponseWriter, *http.Request, *HTTPError) {
			calls++
			panic("error handler panic")
		}),
	)
	Route[struct{}, struct{}](server).
		GET("/error").
		To(func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, errors.New("handler error")
		})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/error", nil))
	if calls != 1 {
		t.Fatalf("ErrorHandler calls = %d, want 1", calls)
	}
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestDefaultErrorResponseDoesNotExposeInternalError(t *testing.T) {
	t.Parallel()

	server := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](server).GET("/broken").To(func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, errors.New("database password leaked")
	})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/broken", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if body := recorder.Body.String(); strings.Contains(body, "database password leaked") {
		t.Fatalf("internal error leaked in response: %q", body)
	}
}

func TestServerDoesNotRewriteCommittedSelfWrittenError(t *testing.T) {
	t.Parallel()

	var errorHandlerCalls int
	server := New(WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, _ *HTTPError) {
		errorHandlerCalls++
		http.Error(w, "replacement", http.StatusInternalServerError)
	}))
	Route[struct{}, struct{}](server).
		GET("/write-then-fail").
		ToHTTPFunc(func(w http.ResponseWriter, _ *http.Request, _ struct{}) error {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("accepted"))
			return errors.New("after write")
		})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/write-then-fail", nil))

	if errorHandlerCalls != 0 {
		t.Fatalf("ErrorHandler calls = %d, want 0", errorHandlerCalls)
	}
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
	}
	if got := recorder.Body.String(); got != "accepted" {
		t.Fatalf("body = %q, want accepted", got)
	}
}

func TestServerDoesNotRewriteCommittedPanic(t *testing.T) {
	t.Parallel()

	var errorHandlerCalls int
	server := New(WithErrorHandler(func(http.ResponseWriter, *http.Request, *HTTPError) {
		errorHandlerCalls++
	}))
	Route[struct{}, struct{}](server).
		GET("/write-then-panic").
		ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("accepted"))
			panic("after write")
		}))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/write-then-panic", nil))
	if errorHandlerCalls != 0 {
		t.Fatalf("ErrorHandler calls = %d, want 0", errorHandlerCalls)
	}
	if recorder.Code != http.StatusAccepted || recorder.Body.String() != "accepted" {
		t.Fatalf("response = %d %q, want 202 accepted", recorder.Code, recorder.Body.String())
	}
}

func TestServerDoesNotRewriteCommittedErrorInsideTimeout(t *testing.T) {
	t.Parallel()

	var errorHandlerCalls int
	server := New(WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, _ *HTTPError) {
		errorHandlerCalls++
		http.Error(w, "replacement", http.StatusInternalServerError)
	}))
	server.Use(Timeout(time.Second))
	Route[struct{}, struct{}](server).
		GET("/write-then-fail").
		ToHTTPFunc(func(w http.ResponseWriter, _ *http.Request, _ struct{}) error {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("accepted"))
			return errors.New("after write")
		})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/write-then-fail", nil))

	if errorHandlerCalls != 0 {
		t.Fatalf("ErrorHandler calls = %d, want 0", errorHandlerCalls)
	}
	if recorder.Code != http.StatusAccepted || recorder.Body.String() != "accepted" {
		t.Fatalf("response = %d %q, want 202 accepted", recorder.Code, recorder.Body.String())
	}
}

func TestServerSuppressesHEADOutcomeBody(t *testing.T) {
	t.Parallel()

	server := New(WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, _ *HTTPError) {
		w.Header().Set("X-Outcome", "missing")
		http.Error(w, "response body must be suppressed", http.StatusTeapot)
	}))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/missing", nil))

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTeapot)
	}
	if got := recorder.Header().Get("X-Outcome"); got != "missing" {
		t.Fatalf("X-Outcome = %q, want missing", got)
	}
	if got := recorder.Body.String(); got != "" {
		t.Fatalf("HEAD body = %q, want empty", got)
	}
}

func TestServerRoutesMatchOnceDespiteMiddlewarePathMutation(t *testing.T) {
	t.Parallel()

	server := New(
		WithProduces(MIMEJSON),
	)
	server.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = "/changed-after-match"
			next.ServeHTTP(w, r)
		})
	})
	Route[struct{}, struct{}](server).
		GET("/users/{id}").
		To(func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, nil
		})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (params extracted once before middleware)", recorder.Code)
	}
}

func TestServerRoutesExtractorFallbackFailureThroughErrorHandler(t *testing.T) {
	t.Parallel()

	server := New(
		WithProduces(MIMEJSON),
		WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err *HTTPError) {
			if err.Code != http.StatusBadRequest {
				t.Errorf("error code = %d, want %d", err.Code, http.StatusBadRequest)
			}
			w.WriteHeader(http.StatusTeapot)
		}),
	)
	server.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = "/changed-after-match"
			ctx := context.WithValue(context.Background(), requestStateContextKey{}, &requestState{server: server})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	Route[struct{}, struct{}](server).
		GET("/users/{id}").
		To(func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, nil
		})

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTeapot)
	}
}

func TestServerLogsRecoveredPanicWithStack(t *testing.T) {
	t.Parallel()

	logger := &testLogger{}
	server := New(WithProduces(MIMEJSON), WithLogger(logger))
	Route[struct{}, struct{}](server).
		GET("/panic").
		To(func(context.Context, struct{}) (struct{}, error) {
			panic("boom")
		})

	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))
	if logger.lastLevel != "error" || logger.lastMsg != "panic recovered" {
		t.Fatalf("last log = %s %q, want error panic recovered", logger.lastLevel, logger.lastMsg)
	}
	if !hasLogKey(logger.lastArgs, "stack") {
		t.Fatalf("panic log args = %#v, want stack", logger.lastArgs)
	}
}
