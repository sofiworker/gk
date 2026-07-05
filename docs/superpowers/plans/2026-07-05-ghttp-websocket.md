# ghttp WebSocket Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current WebSocket stubs with working server and client WebSocket support for JSON message exchange.

**Architecture:** Move WebSocket-specific code out of `builder.go` and `client.go` into focused files. `RouteBuilder.ToWebSocket` should perform a real HTTP upgrade, pass `Params` to the user handler, and close the connection after the handler returns. `Client.WebSocket` should perform a real dial and return a non-nil `WebSocketConn` that supports `ReadJSON`, `WriteJSON`, and `Close`.

**Tech Stack:** Go `net/http`, existing `ghttp` route and params machinery, existing `golang.org/x/net/websocket` dependency unless the implementer explicitly chooses and justifies adding `github.com/gorilla/websocket` with `go mod tidy`.

---

## Scope

Implement now:
- Server-side `ToWebSocket` upgrade instead of returning `501 Not Implemented`.
- Client-side `Client.WebSocket` dial instead of returning an empty `WebSocketConn`.
- JSON read/write and close through the existing `WebSocketConn` API.
- Path/query/header/cookie access through `Params` in `WebSocketHandler`.
- Client options already exposed today: TLS config, subprotocols, handshake timeout, read buffer size, write buffer size.
- README/example updates so WebSocket is documented as real support.

Do not implement now:
- Connection registries, rooms, broadcast helpers, automatic reconnect, heartbeats, backpressure queues, or concurrent write serialization.
- A large new WebSocket framework on top of the raw connection.
- A new public API unless tests prove the current API is insufficient.

## File Structure

- Create: `ghttp/websocket.go`
  - Owns server-side WebSocket upgrade, `WebSocketConn`, `WebSocketHandler`, JSON helpers, close behavior, and any small private adapter needed for the chosen WebSocket library.
- Create: `ghttp/websocket_test.go`
  - Owns server/client WebSocket integration tests.
- Modify: `ghttp/builder.go`
  - Remove the stub `buildWebSocketHandler` implementation or move it to `websocket.go`.
  - Keep `ToWebSocket` as the public route builder terminal method.
- Modify: `ghttp/client.go`
  - Remove WebSocket-specific types and stub dial from the generic HTTP client file after moving them into `websocket.go` or a dedicated client file.
- Optional Create: `ghttp/client_websocket.go`
  - Use this if client code would make `websocket.go` too large.
- Modify: `ghttp/README.md`
  - Update WebSocket docs after implementation.
- Modify: `example/server_review_v3/main_test.go`
  - Replace the current stub-observation test with a working WebSocket behavior test or remove the stale issue assertion.

## Dependency Decision

Before writing implementation code, choose one path:

1. Preferred for offline/minimal dependency: use existing `golang.org/x/net/websocket`.
   - It is already in `go.mod`.
   - It avoids adding dependencies.
   - Wrap its JSON send/receive functions behind the existing `WebSocketConn` methods.

2. Alternative if `x/net/websocket` blocks required behavior: add `github.com/gorilla/websocket`.
   - Must be justified in the commit message or PR notes.
   - Must run `go mod tidy`.
   - Must keep the public `ghttp.WebSocketConn` API stable.

### Task 1: Server Upgrade Echo Test

**Files:**
- Test: `ghttp/websocket_test.go`
- Modify later: `ghttp/builder.go`
- Create later: `ghttp/websocket.go`

- [ ] **Step 1: Write the failing server echo test**

Add a test that registers a WebSocket echo route and dials it with a real WebSocket client:

```go
func TestWebSocketServerEchoJSON(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type message struct {
		Text string `json:"text"`
	}

	Route[struct{}, struct{}](app).
		GET("/ws").
		ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
			var in message
			if err := conn.ReadJSON(&in); err != nil {
				return err
			}
			return conn.WriteJSON(message{Text: "echo:" + in.Text})
		})

	ts := httptest.NewServer(app)
	defer ts.Close()

	conn := dialTestWebSocket(t, ts.URL+"/ws")
	defer conn.Close()

	if err := conn.WriteJSON(message{Text: "hello"}); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}
	var out message
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out.Text != "echo:hello" {
		t.Fatalf("Text = %q, want echo:hello", out.Text)
	}
}
```

Use a test helper backed by the selected WebSocket library.

- [ ] **Step 2: Run test to verify RED**

Run:

```bash
go test ./ghttp -run TestWebSocketServerEchoJSON -count=1
```

Expected: fail because the current server returns `501 Not Implemented` or the client cannot complete the upgrade.

- [ ] **Step 3: Implement minimal server upgrade**

Create `ghttp/websocket.go` and move server WebSocket behavior there:

```go
func buildWebSocketHandler(s *Server, handler WebSocketHandler) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, routeParams pathParamList) {
		params := paramsFromRequestWithPathParams(r, s.config, routeParams)
		// Upgrade using selected WebSocket library.
		// Wrap the library connection in *WebSocketConn.
		// Call handler(r.Context(), params, conn).
		// Close conn after handler returns.
	})
}
```

Keep errors simple:
- Bad upgrade request should return `400 Bad Request`.
- Handler errors after a successful upgrade should close the WebSocket connection; do not attempt to write an HTTP error after upgrade.

- [ ] **Step 4: Run test to verify GREEN**

Run:

```bash
go test ./ghttp -run TestWebSocketServerEchoJSON -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ghttp/websocket.go ghttp/websocket_test.go ghttp/builder.go
git commit -m "feat(ghttp): implement websocket server upgrade"
```

### Task 2: Params Propagation and Upgrade Rejection

**Files:**
- Test: `ghttp/websocket_test.go`
- Modify: `ghttp/websocket.go`

- [ ] **Step 1: Write failing params propagation test**

Add:

```go
func TestWebSocketServerPassesParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type output struct {
		ID    string `json:"id"`
		Trace string `json:"trace"`
	}

	Route[struct{}, struct{}](app).
		GET("/ws/{id}").
		ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
			return conn.WriteJSON(output{
				ID:    params.Path("id"),
				Trace: params.Query("trace"),
			})
		})

	ts := httptest.NewServer(app)
	defer ts.Close()

	conn := dialTestWebSocket(t, ts.URL+"/ws/42?trace=abc")
	defer conn.Close()

	var out output
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out.ID != "42" || out.Trace != "abc" {
		t.Fatalf("out = %#v, want id and trace", out)
	}
}
```

- [ ] **Step 2: Write failing non-upgrade request test**

Add:

```go
func TestWebSocketServerRejectsPlainHTTP(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(context.Context, Params, *WebSocketConn) error {
		return nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
```

- [ ] **Step 3: Run tests to verify RED**

Run:

```bash
go test ./ghttp -run "TestWebSocketServerPassesParams|TestWebSocketServerRejectsPlainHTTP" -count=1
```

Expected: fail until server error handling and params propagation are correct.

- [ ] **Step 4: Implement params and rejection behavior**

Use existing `paramsFromRequestWithPathParams(r, s.config, routeParams)` for params.

Before or during upgrade, ensure a plain HTTP request returns `400 Bad Request`. Exact detection depends on selected library:
- If the library returns a handshake error, translate it to `400`.
- Do not panic or return `501`.

- [ ] **Step 5: Run tests to verify GREEN**

Run:

```bash
go test ./ghttp -run "TestWebSocketServerPassesParams|TestWebSocketServerRejectsPlainHTTP" -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ghttp/websocket.go ghttp/websocket_test.go
git commit -m "test(ghttp): cover websocket params and bad upgrades"
```

### Task 3: Client Dial Implementation

**Files:**
- Test: `ghttp/websocket_test.go`
- Modify: `ghttp/client.go`
- Create or Modify: `ghttp/client_websocket.go`
- Modify: `ghttp/websocket.go`

- [ ] **Step 1: Write failing client dial test**

Add:

```go
func TestClientWebSocketEchoJSON(t *testing.T) {
	app := New(WithProduces(MIMEJSON))

	type message struct {
		Text string `json:"text"`
	}

	Route[struct{}, struct{}](app).GET("/ws").ToWebSocket(func(ctx context.Context, params Params, conn *WebSocketConn) error {
		var in message
		if err := conn.ReadJSON(&in); err != nil {
			return err
		}
		return conn.WriteJSON(message{Text: "server:" + in.Text})
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	client := NewClient()
	conn, err := client.WebSocket(httpToWSURL(ts.URL + "/ws"))
	if err != nil {
		t.Fatalf("WebSocket failed: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(message{Text: "hello"}); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}
	var out message
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON failed: %v", err)
	}
	if out.Text != "server:hello" {
		t.Fatalf("Text = %q, want server:hello", out.Text)
	}
}
```

- [ ] **Step 2: Run test to verify RED**

Run:

```bash
go test ./ghttp -run TestClientWebSocketEchoJSON -count=1
```

Expected: fail because `Client.WebSocket` returns an empty connection today.

- [ ] **Step 3: Implement real client dial**

Move the current WebSocket types out of `ghttp/client.go` into `ghttp/client_websocket.go` or `ghttp/websocket.go`.

Implement:

```go
func (c *Client) WebSocket(url string, opts ...WebSocketOption) (*WebSocketConn, error) {
	cfg := &clientWebSocketConfig{handshakeTimeout: 10 * time.Second}
	for _, opt := range opts {
		opt(cfg)
	}
	// Dial with selected WebSocket library.
	// Apply TLS config, subprotocols, timeout, read/write buffer sizes where supported.
	// Return &WebSocketConn{...}, nil on success.
}
```

Return errors with context, for example:

```go
return nil, fmt.Errorf("websocket dial %s: %w", url, err)
```

- [ ] **Step 4: Run test to verify GREEN**

Run:

```bash
go test ./ghttp -run TestClientWebSocketEchoJSON -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ghttp/client.go ghttp/client_websocket.go ghttp/websocket.go ghttp/websocket_test.go
git commit -m "feat(ghttp): implement websocket client dial"
```

### Task 4: Client Options and TLS Coverage

**Files:**
- Test: `ghttp/websocket_test.go`
- Modify: `ghttp/client_websocket.go`
- Modify: `ghttp/websocket.go`

- [ ] **Step 1: Write failing subprotocol test**

Add a test where server accepts a subprotocol and client passes:

```go
conn, err := client.WebSocket(httpToWSURL(ts.URL+"/ws"), WithWebSocketSubprotocols([]string{"json"}))
```

Assert the server can observe or negotiate the subprotocol if the selected library exposes it. If the chosen library cannot expose negotiated subprotocol cleanly through the existing public API, assert at least that the handshake succeeds with the requested subprotocol and document the limitation.

- [ ] **Step 2: Write failing TLS test**

Use `httptest.NewTLSServer(app)` and:

```go
conn, err := client.WebSocket(httpToWSURL(ts.URL+"/ws"), WithWebSocketTLS(ts.Client().Transport.(*http.Transport).TLSClientConfig))
```

If the TLS config cannot be extracted safely from the test client, build an explicit `*tls.Config{InsecureSkipVerify: true}` only inside the test.

- [ ] **Step 3: Run tests to verify RED**

Run:

```bash
go test ./ghttp -run "TestClientWebSocketSubprotocols|TestClientWebSocketTLS" -count=1
```

Expected: fail until options are wired into the dialer.

- [ ] **Step 4: Implement option wiring**

Apply:
- `WithWebSocketTLS` to the client dialer.
- `WithWebSocketSubprotocols` to the handshake config.
- `handshakeTimeout` to the dial deadline or context.
- read/write buffer sizes only if the selected library supports them.

Do not add public option methods unless tests prove current options are not enough.

- [ ] **Step 5: Run tests to verify GREEN**

Run:

```bash
go test ./ghttp -run "TestClientWebSocketSubprotocols|TestClientWebSocketTLS" -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ghttp/client_websocket.go ghttp/websocket.go ghttp/websocket_test.go
git commit -m "test(ghttp): cover websocket client options"
```

### Task 5: Remove Stale Stub Documentation and Review Tests

**Files:**
- Modify: `ghttp/README.md`
- Modify: `example/server_review_v3/main_test.go`
- Optional Modify: `example/server_review/main.go`

- [ ] **Step 1: Update README**

Remove any wording implying WebSocket is stub or unsupported. Keep the example small:

```go
ghttp.Route[struct{}, struct{}](s).GET("/ws/{room}").ToWebSocket(func(ctx context.Context, params ghttp.Params, conn *ghttp.WebSocketConn) error {
    var msg map[string]string
    if err := conn.ReadJSON(&msg); err != nil {
        return err
    }
    msg["room"] = params.Path("room")
    return conn.WriteJSON(msg)
})
```

- [ ] **Step 2: Update review test**

Replace `Test_WebSocket_Stub` in `example/server_review_v3/main_test.go` with a behavior-oriented test that expects a successful upgrade or JSON echo.

- [ ] **Step 3: Run tests to verify docs/examples compile**

Run:

```bash
go test ./ghttp ./example/ghttp_usage ./example/server_review_v2 ./example/server_review_v3 -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add ghttp/README.md example/server_review_v3/main_test.go example/server_review/main.go
git commit -m "docs(ghttp): document websocket support"
```

### Task 6: Final Verification

**Files:**
- Modify only if verification exposes issues.

- [ ] **Step 1: Format touched Go files**

Run only on touched Go files to avoid unrelated formatting churn:

```bash
gofmt -w ghttp/websocket.go ghttp/client_websocket.go ghttp/builder.go ghttp/client.go ghttp/websocket_test.go example/server_review_v3/main_test.go example/server_review/main.go
```

If `client_websocket.go` is not created, omit it.

- [ ] **Step 2: Tidy dependencies if dependency changed**

Run only if `go.mod` or `go.sum` changed:

```bash
go mod tidy
```

- [ ] **Step 3: Run focused tests**

```bash
go test ./ghttp -run "TestWebSocket|TestClientWebSocket" -count=1
```

Expected: PASS.

- [ ] **Step 4: Run package tests**

```bash
go test ./ghttp -count=1
```

Expected: PASS.

- [ ] **Step 5: Run root tests**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 6: Run nested example module tests**

```bash
cd example/server_review
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Run lint if available**

```bash
golangci-lint run ./...
```

Expected: PASS. If `golangci-lint` is not installed, record that explicitly.

- [ ] **Step 8: Final commit if prior tasks were not committed separately**

```bash
git status --short
git add <remaining touched files>
git commit -m "feat(ghttp): add websocket support"
```

Expected: working tree clean after commit.
