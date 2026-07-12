package ghttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/sofiworker/gk/ghttp"
)

type externalTypedInput struct {
	ghttp.Params `json:"-"`
}

type externalTypedOutput struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

type externalHTTPFuncInput struct {
	ghttp.Params `json:"-"`
}

func TestServerRoutingPublicAPI(t *testing.T) {
	var middlewareOrder []string
	var errorCode int
	server := ghttp.New(
		ghttp.WithProduces(ghttp.MIMEJSON),
		ghttp.WithOpenAPI("routing external API", "1.0.0"),
		ghttp.WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err *ghttp.HTTPError) {
			errorCode = err.Code
			w.WriteHeader(http.StatusTeapot)
			_, _ = fmt.Fprintf(w, "handled:%d", err.Code)
		}),
	)
	server.Use(externalRoutingMiddleware(&middlewareOrder, "server"))

	api := server.Group("/api", externalRoutingMiddleware(&middlewareOrder, "api"))
	v1 := api.Group("/v1", externalRoutingMiddleware(&middlewareOrder, "v1"))

	ghttp.Route[externalTypedInput, externalTypedOutput](v1).
		GET("/users/{id}").
		Use(externalRoutingMiddleware(&middlewareOrder, "route")).
		To(func(_ context.Context, input externalTypedInput) (externalTypedOutput, error) {
			middlewareOrder = append(middlewareOrder, "handler")
			return externalTypedOutput{
				ID:     input.Path("id"),
				Source: input.Query("source"),
			}, nil
		})

	ghttp.Route[struct{}, struct{}](v1).
		GET("/raw/{id}").
		ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Path-Value", r.PathValue("id"))
			w.Header().Set("X-Request-URI", r.URL.RequestURI())
			w.WriteHeader(http.StatusNoContent)
		}))

	ghttp.Route[externalHTTPFuncInput, struct{}](v1).
		GET("/manual/{id}").
		ToHTTPFunc(func(w http.ResponseWriter, _ *http.Request, input externalHTTPFuncInput) error {
			w.WriteHeader(http.StatusAccepted)
			_, err := fmt.Fprintf(w, "%s:%s", input.Path("id"), input.Query("view"))
			return err
		})

	ghttp.Route[struct{}, struct{}](server).
		GET("/trigger-error").
		To(func(context.Context, struct{}) (struct{}, error) {
			return struct{}{}, errors.New("route failure")
		})

	testServer := httptest.NewServer(server)
	defer testServer.Close()

	response, body := externalGet(t, testServer.Client(), testServer.URL+"/api/v1/users/42?source=external")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("typed To status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var typed externalTypedOutput
	if err := json.Unmarshal(body, &typed); err != nil {
		t.Fatalf("typed To JSON = %q, unmarshal error = %v", body, err)
	}
	if want := (externalTypedOutput{ID: "42", Source: "external"}); typed != want {
		t.Fatalf("typed To response = %#v, want %#v", typed, want)
	}
	if want := []string{
		"server-in", "api-in", "v1-in", "route-in", "handler",
		"route-out", "v1-out", "api-out", "server-out",
	}; !reflect.DeepEqual(middlewareOrder, want) {
		t.Fatalf("middleware order = %#v, want %#v", middlewareOrder, want)
	}

	response, _ = externalGet(t, testServer.Client(), testServer.URL+"/api/v1/raw/42?mode=raw")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("ToHTTP status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if got := response.Header.Get("X-Path-Value"); got != "" {
		t.Fatalf("ToHTTP request PathValue(id) = %q, want empty original request value", got)
	}
	if got := response.Header.Get("X-Request-URI"); got != "/api/v1/raw/42?mode=raw" {
		t.Fatalf("ToHTTP request URI = %q, want original request URI", got)
	}

	response, body = externalGet(t, testServer.Client(), testServer.URL+"/api/v1/manual/42?view=full")
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("ToHTTPFunc status = %d, want %d", response.StatusCode, http.StatusAccepted)
	}
	if got := string(body); got != "42:full" {
		t.Fatalf("ToHTTPFunc parsed input = %q, want 42:full", got)
	}

	response, body = externalGet(t, testServer.Client(), testServer.URL+"/trigger-error")
	if response.StatusCode != http.StatusTeapot {
		t.Fatalf("ErrorHandler status = %d, want %d", response.StatusCode, http.StatusTeapot)
	}
	if errorCode != http.StatusInternalServerError {
		t.Fatalf("ErrorHandler code = %d, want %d", errorCode, http.StatusInternalServerError)
	}
	if got := string(body); got != "handled:500" {
		t.Fatalf("ErrorHandler body = %q, want handled:500", got)
	}

	response, body = externalGet(t, testServer.Client(), testServer.URL+"/openapi.json")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("OpenAPI endpoint status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var document struct {
		OpenAPI string                     `json:"openapi"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("OpenAPI document = %q, unmarshal error = %v", body, err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("OpenAPI version = %q, want 3.1.0", document.OpenAPI)
	}
	if _, ok := document.Paths["/api/v1/users/{id}"]; !ok {
		t.Fatalf("OpenAPI paths = %#v, want /api/v1/users/{id}", document.Paths)
	}
}

func externalRoutingMiddleware(order *[]string, name string) ghttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, name+"-in")
			next.ServeHTTP(w, r)
			*order = append(*order, name+"-out")
		})
	}
}

func externalGet(t *testing.T, client *http.Client, target string) (*http.Response, []byte) {
	t.Helper()

	response, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s error = %v", target, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s response body error = %v", target, err)
	}
	return response, body
}
