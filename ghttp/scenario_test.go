package ghttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type scenarioLogger struct {
	lines []string
}

func (l *scenarioLogger) Debugf(format string, args ...interface{}) {}

func (l *scenarioLogger) Infof(format string, args ...interface{}) {}

func (l *scenarioLogger) Warnf(format string, args ...interface{}) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *scenarioLogger) Errorf(format string, args ...interface{}) {}

type scenarioValidator struct{}

func (scenarioValidator) Validate(context.Context, interface{}) error {
	return nil
}

func TestScenario_ServerGroupRouteMiddlewareOpenAPIAndStatic(t *testing.T) {
	app := New(WithOpenAPI("scenario", "1.0.0"), WithValidator(scenarioValidator{}))

	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Scenario-MW", "server")
			next.ServeHTTP(w, r)
		})
	})

	api := app.Group("/api", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Scenario-Group", "api")
			next.ServeHTTP(w, r)
		})
	})

	type input struct {
		Path struct {
			ID string `path:"id"`
		}
		Body struct {
			Name string `json:"name"`
		}
	}

	type output struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	mustScenarioRoute(t, Route[input, output](api).
		POST("/users/{id}").
		Doc("create user").
		Tags("users").
		OperationID("createUser").
		Responds(http.StatusCreated).With(output{}).Desc("created").End().
		To(func(ctx context.Context, req *input) (*output, error) {
			return &output{ID: req.Path.ID, Name: req.Body.Name}, nil
		}))

	mustScenarioRoute(t, Route[struct{}, struct {
		OK bool `json:"ok"`
	}](app).
		ANY("/health").
		To(func(ctx context.Context, req *struct{}) (*struct {
			OK bool `json:"ok"`
		}, error) {
			return &struct {
				OK bool `json:"ok"`
			}{OK: true}, nil
		}))

	mustScenarioRoute(t, Route[struct{}, struct {
		Verb string `json:"verb"`
	}](app).
		CUSTOM("PROPFIND", "/custom").
		To(func(ctx context.Context, req *struct{}) (*struct {
			Verb string `json:"verb"`
		}, error) {
			return &struct {
				Verb string `json:"verb"`
			}{Verb: "PROPFIND"}, nil
		}))

	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "hello.txt"), []byte("hello from scenario"), 0o600); err != nil {
		t.Fatalf("write static file failed: %v", err)
	}
	app.Static("/public", staticDir)
	app.SSE("/events", func(ctx Context, stream *SSEWriter) error {
		return stream.WriteEvent("ready", "ok")
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/users/42", "application/json", strings.NewReader(`{"name":"alice"}`))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/users/42 status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header.Get("X-Scenario-MW") != "server" || resp.Header.Get("X-Scenario-Group") != "api" {
		t.Fatalf("middleware headers missing: %v", resp.Header)
	}

	req, _ := http.NewRequest("HEAD", ts.URL+"/health", nil)
	headResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD failed: %v", err)
	}
	headResp.Body.Close()
	if headResp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD /health status = %d, want %d", headResp.StatusCode, http.StatusOK)
	}

	customReq, _ := http.NewRequest("PROPFIND", ts.URL+"/custom", nil)
	customResp, err := http.DefaultClient.Do(customReq)
	if err != nil {
		t.Fatalf("CUSTOM failed: %v", err)
	}
	defer customResp.Body.Close()
	if customResp.StatusCode != http.StatusOK {
		t.Fatalf("PROPFIND /custom status = %d, want %d", customResp.StatusCode, http.StatusOK)
	}

	staticResp, err := http.Get(ts.URL + "/public/hello.txt")
	if err != nil {
		t.Fatalf("static GET failed: %v", err)
	}
	defer staticResp.Body.Close()
	body, _ := io.ReadAll(staticResp.Body)
	if staticResp.StatusCode != http.StatusOK || !strings.Contains(string(body), "hello from scenario") {
		t.Fatalf("static response = %d %q", staticResp.StatusCode, string(body))
	}

	sseReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/events", nil)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE failed: %v", err)
	}
	defer sseResp.Body.Close()
	buf := make([]byte, 64)
	n, err := sseResp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("SSE read failed: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "event: ready") {
		t.Fatalf("SSE payload = %q, want event ready", string(buf[:n]))
	}

	openAPIResp, err := http.Get(ts.URL + "/openapi.json")
	if err != nil {
		t.Fatalf("openapi fetch failed: %v", err)
	}
	defer openAPIResp.Body.Close()
	if openAPIResp.StatusCode != http.StatusOK {
		t.Fatalf("openapi status = %d, want %d", openAPIResp.StatusCode, http.StatusOK)
	}
}

func mustScenarioRoute(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("route registration failed: %v", err)
	}
}
