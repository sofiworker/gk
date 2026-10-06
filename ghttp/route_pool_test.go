package ghttp

import (
	"context"
	"net/http"
	"testing"
)

// TestWithUnlimitedBodySetsRouteLimit verifies the helper is exactly the
// explicit unlimited route setting and does not alter unrelated options.
func TestWithUnlimitedBodySetsRouteLimit(t *testing.T) {
	var opts routeOptions
	WithBodyLimit(64)(&opts)
	WithUnlimitedBody()(&opts)
	if opts.bodyLimit != -1 {
		t.Fatalf("bodyLimit = %d, want -1", opts.bodyLimit)
	}
}

type routePoolBody struct {
	Name string `json:"name"`
}

// TestTypedBodyPoolIntegration exercises the compiled endpoint and action
// paths through Server.ServeHTTP. It deliberately alternates valid and invalid
// bodies so a pooled lazy state cannot leak between requests.
func TestTypedBodyPoolIntegration(t *testing.T) {
	var actionName string
	s := coreNewServer(t, []ServerOption{WithMaxBodyBytes(8)},
		Post("/echo", func(ctx context.Context, req RequestOf[routePoolBody]) (routePoolBody, error) {
			return req.Data(ctx)
		}, WithUnlimitedBody()),
		HandleAction(http.MethodPost, "/action", func(ctx context.Context, req RequestOf[routePoolBody]) error {
			body, err := req.Data(ctx)
			if err == nil {
				actionName = body.Name
			}
			return err
		}, WithUnlimitedBody()),
	)

	tests := []struct {
		name       string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{name: "first valid endpoint", path: "/echo", body: `{"name":"first-value"}`, wantStatus: http.StatusOK, wantBody: `{"name":"first-value"}`},
		{name: "second valid endpoint", path: "/echo", body: `{"name":"second-value"}`, wantStatus: http.StatusOK, wantBody: `{"name":"second-value"}`},
		{name: "invalid endpoint", path: "/echo", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "valid after endpoint error", path: "/echo", body: `{"name":"after-error"}`, wantStatus: http.StatusOK, wantBody: `{"name":"after-error"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := coreDo(s, http.MethodPost, tt.path, tt.body, "Content-Type", "application/json")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}

	actionCases := []struct {
		name       string
		body       string
		wantStatus int
		wantName   string
	}{
		{name: "valid action", body: `{"name":"action-value"}`, wantStatus: http.StatusNoContent, wantName: "action-value"},
		{name: "invalid action", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "valid action after error", body: `{"name":"action-after-error"}`, wantStatus: http.StatusNoContent, wantName: "action-after-error"},
	}
	for _, tt := range actionCases {
		t.Run(tt.name, func(t *testing.T) {
			actionName = ""
			rec := coreDo(s, http.MethodPost, "/action", tt.body, "Content-Type", "application/json")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if actionName != tt.wantName {
				t.Fatalf("actionName = %q, want %q", actionName, tt.wantName)
			}
		})
	}
}
