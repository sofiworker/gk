package gserver

import (
	"bytes"
	"errors"
	"net/http"
	"testing"

	"github.com/valyala/fasthttp"
)

func newTestResponseContext(t *testing.T) *Context {
	t.Helper()

	req := fasthttp.AcquireRequest()
	t.Cleanup(func() {
		fasthttp.ReleaseRequest(req)
	})
	req.Header.SetMethod(http.MethodGet)
	req.SetRequestURI("/test")

	var fastCtx fasthttp.RequestCtx
	fastCtx.Init(req, benchAddr, nil)

	return &Context{
		fastCtx: &fastCtx,
		Writer:  &respWriter{ctx: &fastCtx},
		codec:   newCodecFactory(),
	}
}

func TestContextOkWritesJSONStatusOK(t *testing.T) {
	ctx := newTestResponseContext(t)

	ctx.Ok(struct {
		Message string `json:"message"`
	}{Message: "ok"})

	if ctx.StatusCode() != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, ctx.StatusCode())
	}

	if got := string(ctx.Response().Header.Peek("Content-Type")); got != "application/json" {
		t.Fatalf("expected JSON content type, got %q", got)
	}

	if !bytes.Contains(ctx.Response().Body(), []byte(`"message":"ok"`)) {
		t.Fatalf("expected JSON body to contain message, got %s", ctx.Response().Body())
	}
}

func TestResponseBuilderSupportsStatusHeadersAndOk(t *testing.T) {
	ctx := newTestResponseContext(t)

	ctx.Resp().
		Status(http.StatusCreated).
		Header("X-Request-ID", "req-1").
		Ok(map[string]string{"message": "created"})

	if ctx.StatusCode() != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, ctx.StatusCode())
	}

	if got := string(ctx.Response().Header.Peek("X-Request-ID")); got != "req-1" {
		t.Fatalf("expected X-Request-ID header, got %q", got)
	}

	if !bytes.Contains(ctx.Response().Body(), []byte(`"message":"created"`)) {
		t.Fatalf("expected JSON body to contain message, got %s", ctx.Response().Body())
	}
}

func TestResponseBuilderSupportsAudit(t *testing.T) {
	ctx := newTestResponseContext(t)
	payload := map[string]string{"trace": "t-1"}

	ctx.Resp().
		Audit(payload).
		AuditAction("user.create").
		AuditUserID("u-1").
		AuditResource("user", "u-2").
		Ok(map[string]string{"message": "created"})

	if ctx.StatusCode() != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, ctx.StatusCode())
	}

	if got := string(ctx.Response().Header.Peek(HeaderAuditAction)); got != "" {
		t.Fatalf("expected audit not to write response header, got %q", got)
	}

	event, ok := ctx.AuditEvent()
	if !ok {
		t.Fatal("expected audit event")
	}
	if event.Action != "user.create" {
		t.Fatalf("expected audit action user.create, got %q", event.Action)
	}
	if event.UserID != "u-1" {
		t.Fatalf("expected audit user u-1, got %q", event.UserID)
	}
	if event.ResourceType != "user" || event.ResourceID != "u-2" {
		t.Fatalf("expected user resource u-2, got %q %q", event.ResourceType, event.ResourceID)
	}
	if event.Payload == nil {
		t.Fatal("expected custom audit payload")
	}
	customPayload, ok := event.Payload.(map[string]string)
	if !ok {
		t.Fatalf("expected custom payload map, got %T", event.Payload)
	}
	if customPayload["trace"] != "t-1" {
		t.Fatalf("expected custom trace t-1, got %q", customPayload["trace"])
	}
}

func TestContextAuditCanBeUsedWithoutResponseBuilder(t *testing.T) {
	ctx := newTestResponseContext(t)

	ctx.Audit(map[string]string{"trace": "t-2"}).
		Action("user.delete").
		UserID("u-1").
		Resource("user", "u-2")

	event, ok := ctx.AuditEvent()
	if !ok {
		t.Fatal("expected audit event")
	}
	if event.Action != "user.delete" {
		t.Fatalf("expected audit action user.delete, got %q", event.Action)
	}
	if event.UserID != "u-1" {
		t.Fatalf("expected audit user u-1, got %q", event.UserID)
	}
	payload, ok := event.Payload.(map[string]string)
	if !ok {
		t.Fatalf("expected map payload, got %T", event.Payload)
	}
	if payload["trace"] != "t-2" {
		t.Fatalf("expected audit trace t-2, got %q", payload["trace"])
	}
}

func TestAuditPayloadKeepsInterfaceCompatibility(t *testing.T) {
	ctx := newTestResponseContext(t)
	payload := struct {
		RequestID string
	}{RequestID: "req-1"}

	ctx.Audit(payload)

	got, ok := ctx.AuditPayload()
	if !ok {
		t.Fatal("expected audit payload")
	}
	if got != payload {
		t.Fatalf("expected audit payload %#v, got %#v", payload, got)
	}
}

func TestResponseBuilderAutoHonorsAcceptHeader(t *testing.T) {
	type autoXMLMessage struct {
		Message string `xml:"message"`
	}

	ctx := newTestResponseContext(t)
	ctx.Request().Header.Set("Accept", "application/xml")

	ctx.Resp().Status(http.StatusAccepted).Auto(autoXMLMessage{Message: "accepted"})

	if ctx.StatusCode() != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, ctx.StatusCode())
	}

	if got := string(ctx.Response().Header.Peek("Content-Type")); got != "application/xml" {
		t.Fatalf("expected XML content type, got %q", got)
	}

	if !bytes.Contains(ctx.Response().Body(), []byte(`<message>accepted</message>`)) {
		t.Fatalf("expected XML body to contain message, got %s", ctx.Response().Body())
	}
}

func TestResponseBuilderErrorWritesStatusAndMessage(t *testing.T) {
	ctx := newTestResponseContext(t)

	ctx.Resp().Status(http.StatusBadRequest).Error(errors.New("bad input"))

	if ctx.StatusCode() != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, ctx.StatusCode())
	}

	if got := string(ctx.Response().Body()); got != "bad input" {
		t.Fatalf("expected error body, got %q", got)
	}
}

func TestResponseBuilderRedirectUsesConfiguredStatus(t *testing.T) {
	ctx := newTestResponseContext(t)

	ctx.Resp().Status(http.StatusTemporaryRedirect).Redirect("/next")

	if ctx.StatusCode() != http.StatusTemporaryRedirect {
		t.Fatalf("expected status %d, got %d", http.StatusTemporaryRedirect, ctx.StatusCode())
	}

	if got := string(ctx.Response().Header.Peek("Location")); got != "/next" {
		t.Fatalf("expected redirect location, got %q", got)
	}
}

func TestResponseBuilderNoContentAlwaysUsesNoContentStatus(t *testing.T) {
	ctx := newTestResponseContext(t)

	ctx.Resp().Status(http.StatusOK).NoContent()

	if ctx.StatusCode() != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, ctx.StatusCode())
	}

	if body := ctx.Response().Body(); len(body) != 0 {
		t.Fatalf("expected empty body, got %s", body)
	}
}
