package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	gwebsocket "github.com/gorilla/websocket"
)

type operationUser struct {
	ID int64 `json:"id"`
}

type operationLookup struct {
	ID     int64
	Locale string
}

type operationCreateUser struct {
	Name string `json:"name"`
}

type operationDocumentedInput struct {
	ID    int64
	Tags  []string
	Trace string
	Body  operationCreateUser
}

type operationValidatorFunc func(context.Context, interface{}) error

func (f operationValidatorFunc) Validate(ctx context.Context, input interface{}) error {
	return f(ctx, input)
}

func TestOperationGetJSONMountsAsFirstClassValue(t *testing.T) {
	operation := GetJSON("/users/{id}", PathInt64("id"), func(_ context.Context, id int64) (operationUser, error) {
		return operationUser{ID: id}, nil
	})
	if operation.Method() != http.MethodGet {
		t.Fatalf("method = %q, want %q", operation.Method(), http.MethodGet)
	}
	if operation.Path() != "/users/{id}" {
		t.Fatalf("path = %q, want %q", operation.Path(), "/users/{id}")
	}

	server := New()
	if err := server.Mount(operation); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != `{"id":42}` {
		t.Fatalf("body = %q, want %q", got, `{"id":42}`)
	}
	if got := recorder.Header().Get("Content-Type"); got != MIMEJSON {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestOperationStaticFastPathAllocationBudget(t *testing.T) {
	tests := []struct {
		name      string
		operation *Operation
	}{
		{
			name: "text convenience",
			operation: GetText("/health", NoInput(), func(context.Context, EmptyInput) (string, error) {
				return "ok", nil
			}),
		},
		{
			name: "explicit contract",
			operation: Handle(Get("/health"), NoInput(), TextOutput(), func(context.Context, EmptyInput) (string, error) {
				return "ok", nil
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := New()
			server.MustMount(test.operation)
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			writer := &rewriteBenchmarkWriter{header: make(http.Header)}
			server.ServeHTTP(writer, request)

			allocations := testing.AllocsPerRun(1000, func() {
				server.ServeHTTP(writer, request)
			})
			if allocations > 1 {
				t.Fatalf("allocations = %.0f, want <= 1", allocations)
			}
		})
	}
}

func TestOperationPathFastPathAllocationBudget(t *testing.T) {
	server := New()
	server.MustMount(Handle(Get("/users/{id}"), PathInt64("id"), TextOutput(), func(_ context.Context, id int64) (string, error) {
		_ = id
		return "42", nil
	}))
	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	writer := &rewriteBenchmarkWriter{header: make(http.Header)}
	server.ServeHTTP(writer, request)

	allocations := testing.AllocsPerRun(1000, func() {
		server.ServeHTTP(writer, request)
	})
	if allocations > 1 {
		t.Fatalf("allocations = %.0f, want <= 1", allocations)
	}
}

func TestOperationMapInputsBuildsBusinessInput(t *testing.T) {
	input := MapInputs(PathInt64("id"), QueryString("locale"), func(id int64, locale string) operationLookup {
		return operationLookup{ID: id, Locale: locale}
	})
	server := New()
	server.MustMount(GetJSON("/users/{id}", input, func(_ context.Context, lookup operationLookup) (map[string]any, error) {
		return map[string]any{"id": lookup.ID, "locale": lookup.Locale}, nil
	}))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/7?locale=zh-CN", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != `{"id":7,"locale":"zh-CN"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestOperationHandleUsesExplicitInputAndOutputContracts(t *testing.T) {
	operation := Handle(
		Post("/users"),
		JSONBody[operationCreateUser](),
		WithResponseHeader("X-Endpoint", "create-user", WithStatus(http.StatusCreated, JSONOutput[operationCreateUser]())),
		func(_ context.Context, input operationCreateUser) (operationCreateUser, error) {
			return input, nil
		},
	)
	server := New()
	server.MustMount(operation)

	request := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"Ada"}`))
	request.Header.Set("Content-Type", MIMEJSON)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Endpoint"); got != "create-user" {
		t.Fatalf("X-Endpoint = %q", got)
	}
	if got := recorder.Body.String(); got != `{"name":"Ada"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestOperationOpenAPIComesFromInputAndOutputContracts(t *testing.T) {
	parameters := MapInputs3(
		PathInt64("id"),
		QueryStrings("tag"),
		HeaderString("X-Trace"),
		func(id int64, tags []string, trace string) operationDocumentedInput {
			return operationDocumentedInput{ID: id, Tags: tags, Trace: trace}
		},
	)
	input := MapInputs(parameters, JSONBody[operationCreateUser](), func(parameters operationDocumentedInput, body operationCreateUser) operationDocumentedInput {
		parameters.Body = body
		return parameters
	})
	operation := Handle(
		Patch("/users/{id}"),
		input,
		WithStatus(http.StatusAccepted, JSONOutput[operationUser]()),
		func(_ context.Context, input operationDocumentedInput) (operationUser, error) {
			return operationUser{ID: input.ID}, nil
		},
	).Doc(Summary("Update user"), OperationID("updateUser"))

	server := New(WithOpenAPI("operations", "1.0.0"))
	server.MustMount(operation)
	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	text := string(document)
	for _, fragment := range []string{
		`"summary":"Update user"`,
		`"operationId":"updateUser"`,
		`"name":"id"`,
		`"in":"path"`,
		`"format":"int64"`,
		`"name":"tag"`,
		`"in":"query"`,
		`"type":"array"`,
		`"name":"X-Trace"`,
		`"in":"header"`,
		`"requestBody"`,
		`"202"`,
		`"name":{"type":"string"}`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("OpenAPI missing %s: %s", fragment, text)
		}
	}
}

func TestOperationCopyOnWriteWorksAcrossServerAndGroup(t *testing.T) {
	base := GetText("/health", NoInput(), func(_ context.Context, _ EmptyInput) (string, error) {
		return "ok", nil
	})
	wrapped := base.
		Doc(Summary("Versioned health"), Tags("system")).
		WithMiddleware(func(c *Ctx) {
			c.W.Header().Set("X-Operation", "wrapped")
			c.Next()
		})

	server := New(WithOpenAPI("operations", "1.0.0"))
	server.MustMount(base)
	server.Group("/v1").MustMount(wrapped)

	baseRecorder := httptest.NewRecorder()
	server.ServeHTTP(baseRecorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if baseRecorder.Code != http.StatusOK || baseRecorder.Body.String() != "ok" {
		t.Fatalf("base response = %d %q", baseRecorder.Code, baseRecorder.Body.String())
	}
	if value := baseRecorder.Header().Get("X-Operation"); value != "" {
		t.Fatalf("base operation mutated, X-Operation = %q", value)
	}

	wrappedRecorder := httptest.NewRecorder()
	server.ServeHTTP(wrappedRecorder, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if wrappedRecorder.Code != http.StatusOK || wrappedRecorder.Body.String() != "ok" {
		t.Fatalf("wrapped response = %d %q", wrappedRecorder.Code, wrappedRecorder.Body.String())
	}
	if value := wrappedRecorder.Header().Get("X-Operation"); value != "wrapped" {
		t.Fatalf("wrapped X-Operation = %q", value)
	}
	if base.Method() != http.MethodGet || base.Path() != "/health" {
		t.Fatalf("base operation changed: %s %s", base.Method(), base.Path())
	}
}

func TestOperationJSONUsesConfiguredEnvelope(t *testing.T) {
	server := New(WithEnvelope(DefaultEnvelope))
	server.MustMount(GetJSON("/users/{id}", PathInt64("id"), func(_ context.Context, id int64) (operationUser, error) {
		return operationUser{ID: id}, nil
	}))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/9", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Code int           `json:"code"`
		Msg  string        `json:"msg"`
		Data operationUser `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if body.Code != 0 || body.Msg != "success" || body.Data.ID != 9 {
		t.Fatalf("envelope = %#v", body)
	}
}

func TestOperationJSONUsesFixedContentTypeAndUnifiedErrors(t *testing.T) {
	server := New()
	server.MustMount(GetJSON("/users/{id}", PathInt64("id"), func(_ context.Context, id int64) (operationUser, error) {
		if id == 404 {
			return operationUser{}, NotFound("user not found")
		}
		return operationUser{ID: id}, nil
	}))

	tests := []struct {
		name   string
		path   string
		accept string
		status int
	}{
		{name: "invalid path input", path: "/users/not-an-int", status: http.StatusBadRequest},
		{name: "typed business error", path: "/users/404", status: http.StatusNotFound},
		{name: "fixed JSON ignores Accept", path: "/users/1", accept: MIMEXML, status: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Accept", test.accept)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d, body = %q", recorder.Code, test.status, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, MIMEJSON) {
				t.Fatalf("Content-Type = %q", got)
			}
		})
	}
}

func TestOperationCustomInputMetadataIsFrozenAndOutputHeadersAreDocumented(t *testing.T) {
	metadata := InputMetadata{Parameters: []InputParameter{{
		Name: "tenant", Location: ParameterLocationQuery, Required: true,
		Schema: map[string]any{"type": "string"},
	}}}
	input := InputFuncWithMetadata(func(request RequestView) (string, error) {
		return request.Query().Get("tenant"), nil
	}, metadata)
	operation := Handle(
		Get("/tenants"),
		input,
		WithResponseHeader("X-Contract", "stable", JSONOutput[map[string]string]()),
		func(_ context.Context, tenant string) (map[string]string, error) {
			return map[string]string{"tenant": tenant}, nil
		},
	)
	metadata.Parameters[0].Name = "mutated"
	metadata.Parameters[0].Schema.(map[string]any)["type"] = "integer"

	server := New(WithOpenAPI("operations", "1.0.0"))
	server.MustMount(operation)
	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	text := string(document)
	for _, fragment := range []string{
		`"summary":"GET /tenants"`,
		`"operationId":"get_tenants"`,
		`"name":"tenant"`,
		`"X-Contract"`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("OpenAPI missing %s: %s", fragment, text)
		}
	}
	if strings.Contains(text, "mutated") {
		t.Fatalf("OpenAPI observed caller mutation: %s", text)
	}
}

func TestOperationStandardOutputContracts(t *testing.T) {
	server := New()
	server.MustMount(
		Handle(Delete("/sessions/current"), NoInput(), NoContentOutput[EmptyInput](), func(_ context.Context, input EmptyInput) (EmptyInput, error) {
			return input, nil
		}),
		Handle(Get("/moved"), NoInput(), RedirectOutput(http.StatusTemporaryRedirect), func(_ context.Context, _ EmptyInput) (RedirectResponse, error) {
			return RedirectResponse{Location: "/target"}, nil
		}),
		Handle(Get("/blob"), NoInput(), WithResponseCookie(&http.Cookie{Name: "download", Value: "1", Path: "/"}, BytesOutput("application/octet-stream")), func(_ context.Context, _ EmptyInput) ([]byte, error) {
			return []byte{0, 1, 2, 3}, nil
		}),
	)

	tests := []struct {
		method   string
		path     string
		status   int
		body     string
		header   string
		contains string
	}{
		{method: http.MethodDelete, path: "/sessions/current", status: http.StatusNoContent, body: ""},
		{method: http.MethodGet, path: "/moved", status: http.StatusTemporaryRedirect, header: "Location", contains: "/target"},
		{method: http.MethodGet, path: "/blob", status: http.StatusOK, body: string([]byte{0, 1, 2, 3}), header: "Set-Cookie", contains: "download=1"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		if recorder.Code != test.status || recorder.Body.String() != test.body {
			t.Fatalf("%s %s = %d %q", test.method, test.path, recorder.Code, recorder.Body.String())
		}
		if test.header != "" && !strings.Contains(recorder.Header().Get(test.header), test.contains) {
			t.Fatalf("%s = %q", test.header, recorder.Header().Get(test.header))
		}
	}
}

func TestOperationSSEOutputUsesExistingStreamWriter(t *testing.T) {
	server := New()
	server.MustMount(Handle(
		Get("/events"),
		NoInput(),
		SSEOutput(),
		func(_ context.Context, _ EmptyInput) (func(*SSEWriter) error, error) {
			return func(stream *SSEWriter) error {
				return stream.WriteJSONWithID("user", "7", operationUser{ID: 7})
			}, nil
		},
	))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/events", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	for _, fragment := range []string{"id: 7\n", "event: user\n", `data: {"id":7}` + "\n"} {
		if !strings.Contains(recorder.Body.String(), fragment) {
			t.Fatalf("SSE body missing %q: %q", fragment, recorder.Body.String())
		}
	}
}

func TestWebSocketOperationUsesTypedInputAndExistingConnectionCapabilities(t *testing.T) {
	server := New()
	server.MustMount(WebSocketOperation("/ws/{room}", PathString("room"), func(_ context.Context, room string, connection *WebSocketConn) error {
		var message map[string]string
		if err := connection.ReadJSON(&message); err != nil {
			return err
		}
		message["room"] = room
		return connection.WriteJSON(message)
	}))

	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	webSocketURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws/general"
	connection, _, err := gwebsocket.DefaultDialer.Dial(webSocketURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer connection.Close()
	if err := connection.WriteJSON(map[string]string{"message": "hello"}); err != nil {
		t.Fatalf("write websocket: %v", err)
	}
	var response map[string]string
	if err := connection.ReadJSON(&response); err != nil {
		t.Fatalf("read websocket: %v", err)
	}
	if response["message"] != "hello" || response["room"] != "general" {
		t.Fatalf("response = %#v", response)
	}
}

func TestOperationFormBodyHonorsServerBodyLimit(t *testing.T) {
	server := New(WithMaxBodyBytes(4))
	server.MustMount(PostText("/forms/{id}", FormBody(), func(_ context.Context, values url.Values) (string, error) {
		return values.Get("name"), nil
	}))

	request := httptest.NewRequest(http.MethodPost, "/forms/42", strings.NewReader("name=Ada"))
	request.Header.Set("Content-Type", MIMEPOSTForm)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}

func TestOperationHTTPRequestInputPreservesMatchedParams(t *testing.T) {
	server := New()
	server.MustMount(GetText("/users/{id}", HTTPRequest(), func(_ context.Context, request *http.Request) (string, error) {
		return server.MatchedParams(request).Path("id"), nil
	}))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "42" {
		t.Fatalf("response = %d %q, want 200 %q", recorder.Code, recorder.Body.String(), "42")
	}
}

func TestOperationConvenienceInputsOnlyCompileStateIndependentTerminals(t *testing.T) {
	tests := []struct {
		name      string
		operation *Operation
	}{
		{
			name: "form body",
			operation: PostText("/forms/{id}", FormBody(), func(_ context.Context, _ url.Values) (string, error) {
				return "ok", nil
			}),
		},
		{
			name: "json form body",
			operation: PostJSON("/json-forms/{id}", FormBody(), func(_ context.Context, _ url.Values) (map[string]string, error) {
				return map[string]string{"status": "ok"}, nil
			}),
		},
		{
			name: "http request",
			operation: GetText("/users/{id}", HTTPRequest(), func(_ context.Context, _ *http.Request) (string, error) {
				return "ok", nil
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := New()
			server.MustMount(test.operation)
			server.finalizeRoutes()
			state := server.compiled.Load()
			if state == nil || len(state.mux.routes) != 1 {
				t.Fatalf("compiled routes = %#v, want one route", state)
			}
			if !state.mux.routes[0].needsState {
				t.Fatal("request-state-dependent convenience input compiled as stateless")
			}
		})
	}
}

func TestOperationStreamErrorAfterCommitDoesNotAppendErrorResponse(t *testing.T) {
	base := Handle(
		Get("/stream"),
		NoInput(),
		StreamOutput(MIMEPlain),
		func(context.Context, EmptyInput) (func(io.Writer) error, error) {
			return func(writer io.Writer) error {
				_, _ = io.WriteString(writer, "partial")
				return errors.New("stream failed")
			}, nil
		},
	)
	tests := []struct {
		name      string
		operation *Operation
	}{
		{name: "direct", operation: base},
		{name: "middleware", operation: base.WithMiddleware(func(c *Ctx) { c.Next() })},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := New()
			server.MustMount(test.operation)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil))
			if recorder.Code != http.StatusOK || recorder.Body.String() != "partial" {
				t.Fatalf("response = %d %q, want 200 %q", recorder.Code, recorder.Body.String(), "partial")
			}
		})
	}
}

func TestOperationCustomOutputCarriesSchemaAndDocumentedResponses(t *testing.T) {
	output := WithDocumentedResponses(
		map[int]DocumentedResponse{
			http.StatusConflict: {
				Description: "Name already exists",
				Content: map[string]any{MIMEJSON: map[string]any{
					"type":       "object",
					"properties": map[string]any{"message": map[string]any{"type": "string"}},
				}},
			},
		},
		WithOutputSchema(
			map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}},
			OutputFunc(http.StatusCreated, MIMEJSON, func(writer io.Writer, value map[string]bool) error {
				return json.NewEncoder(writer).Encode(value)
			}),
		),
	)
	operation := Handle(Post("/custom"), NoInput(), output, func(_ context.Context, _ EmptyInput) (map[string]bool, error) {
		return map[string]bool{"ok": true}, nil
	})
	server := New(WithOpenAPI("operations", "1.0.0"))
	server.MustMount(operation)
	document, err := server.OpenAPI()
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	text := string(document)
	for _, fragment := range []string{`"201"`, `"409"`, `"Name already exists"`, `"ok":{"type":"boolean"}`} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("OpenAPI missing %s: %s", fragment, text)
		}
	}
}

func TestOperationMountRejectsInvalidInputComposition(t *testing.T) {
	t.Run("multiple bodies", func(t *testing.T) {
		input := CombineInputs(JSONBody[operationCreateUser](), JSONBody[operationCreateUser]())
		operation := PostJSON("/invalid", input, func(_ context.Context, _ InputPair[operationCreateUser, operationCreateUser]) (operationUser, error) {
			return operationUser{}, nil
		})
		err := New().Mount(operation)
		if !errors.Is(err, ErrMultipleOperationBodies) {
			t.Fatalf("Mount error = %v", err)
		}
	})

	t.Run("duplicate parameter", func(t *testing.T) {
		input := CombineInputs(QueryString("id"), QueryInt("id"))
		operation := GetJSON("/invalid", input, func(_ context.Context, _ InputPair[string, int]) (operationUser, error) {
			return operationUser{}, nil
		})
		err := New().Mount(operation)
		if !errors.Is(err, ErrDuplicateInputParameter) {
			t.Fatalf("Mount error = %v", err)
		}
	})

	t.Run("nil input constructor", func(t *testing.T) {
		operation := GetJSON("/invalid", InputFunc[string](nil), func(_ context.Context, _ string) (operationUser, error) {
			return operationUser{}, nil
		})
		err := New().Mount(operation)
		if !errors.Is(err, ErrInputConstructorNil) {
			t.Fatalf("Mount error = %v", err)
		}
	})

	t.Run("nil input mapper", func(t *testing.T) {
		input := MapInputs(PathString("id"), QueryString("q"), (func(string, string) operationLookup)(nil))
		operation := GetJSON("/invalid/{id}", input, func(_ context.Context, _ operationLookup) (operationUser, error) {
			return operationUser{}, nil
		})
		err := New().Mount(operation)
		if !errors.Is(err, ErrInputMapperNil) {
			t.Fatalf("Mount error = %v", err)
		}
	})
}

func TestOperationRouteCapabilitiesAreCopyOnWrite(t *testing.T) {
	base := PostJSON("/users", JSONBody[operationCreateUser](), func(_ context.Context, _ operationCreateUser) (operationUser, error) {
		return operationUser{ID: 1}, nil
	})
	limited := base.WithMaxBodyBytes(4)
	if base == limited {
		t.Fatal("WithMaxBodyBytes mutated the original operation")
	}
	server := New()
	server.MustMount(limited)
	request := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"Ada"}`))
	request.Header.Set("Content-Type", MIMEJSON)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("limited status = %d, body = %q", recorder.Code, recorder.Body.String())
	}

	errorOperation := GetJSON("/items/{id}", PathInt64("id"), func(_ context.Context, id int64) (operationUser, error) {
		return operationUser{ID: id}, nil
	}).WithErrorWriter(func(writer http.ResponseWriter, _ *http.Request, _ int, _ error) bool {
		writer.WriteHeader(http.StatusTeapot)
		return true
	})
	errorServer := New()
	errorServer.MustMount(errorOperation)
	errorRecorder := httptest.NewRecorder()
	errorServer.ServeHTTP(errorRecorder, httptest.NewRequest(http.MethodGet, "/items/invalid", nil))
	if errorRecorder.Code != http.StatusTeapot {
		t.Fatalf("custom error status = %d", errorRecorder.Code)
	}
}

func TestOperationHTTPAndStaticTerminals(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(tempDir+"/asset.txt", []byte("asset"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New()
	server.MustMount(
		HTTPFuncOperation(http.MethodGet, "/raw", func(writer http.ResponseWriter, _ *http.Request) error {
			_, err := io.WriteString(writer, "raw")
			return err
		}),
		HandleHTTP(Get("/users/{id}"), PathInt64("id"), func(writer http.ResponseWriter, _ *http.Request, id int64) error {
			_, err := io.WriteString(writer, fmt.Sprintf("user:%d", id))
			return err
		}),
		StaticDirectory("/assets", tempDir),
		StaticFile("/favicon.ico", tempDir+"/asset.txt"),
	)

	tests := []struct {
		method string
		path   string
		want   string
	}{
		{method: http.MethodGet, path: "/raw", want: "raw"},
		{method: http.MethodGet, path: "/users/7", want: "user:7"},
		{method: http.MethodGet, path: "/assets/asset.txt", want: "asset"},
		{method: http.MethodGet, path: "/favicon.ico", want: "asset"},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != test.want {
			t.Fatalf("%s %s = %d %q, want 200 %q", test.method, test.path, recorder.Code, recorder.Body.String(), test.want)
		}
	}
}

func TestOperationValidationAndContractErrors(t *testing.T) {
	server := New(WithValidator(operationValidatorFunc(func(context.Context, interface{}) error {
		return errors.New("validation failed")
	})))
	server.MustMount(GetJSON("/users/{id}", PathInt64("id"), func(_ context.Context, id int64) (operationUser, error) {
		return operationUser{ID: id}, nil
	}))

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/1", nil))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation status = %d, want 422", recorder.Code)
	}

	withoutValidation := GetJSON("/users/{id}", PathInt64("id"), func(_ context.Context, id int64) (operationUser, error) {
		return operationUser{ID: id}, nil
	}).WithoutServerValidation()
	validationFreeServer := New(WithValidator(operationValidatorFunc(func(context.Context, interface{}) error {
		return errors.New("validation failed")
	})))
	validationFreeServer.MustMount(withoutValidation)
	recorder = httptest.NewRecorder()
	validationFreeServer.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("validation-free status = %d, body = %q", recorder.Code, recorder.Body.String())
	}

	invalidOutput := Handle(Get("/invalid-output"), NoInput(), WithStatus(700, TextOutput()), func(_ context.Context, _ EmptyInput) (string, error) {
		return "never", nil
	})
	if err := New().Mount(invalidOutput); !errors.Is(err, ErrOperationStatusInvalid) {
		t.Fatalf("invalid output mount error = %v", err)
	}

	metadataInput := InputFuncWithMetadata(func(RequestView) (string, error) { return "", nil }, InputMetadata{
		Parameters: []InputParameter{{Name: "missing", Location: ParameterLocationPath}},
	})
	metadataOperation := Handle(Get("/users/{id}"), metadataInput, TextOutput(), func(_ context.Context, _ string) (string, error) {
		return "ok", nil
	})
	if err := New().Mount(metadataOperation); !errors.Is(err, ErrOperationPathParameterMissing) {
		t.Fatalf("metadata mount error = %v", err)
	}
}

func TestOperationProblemDetailsMatchesOpenAPI(t *testing.T) {
	operation := GetJSON("/users/{id}", PathInt64("id"), func(_ context.Context, id int64) (operationUser, error) {
		return operationUser{ID: id}, nil
	}).WithProblemDetails()
	server := New(WithOpenAPI("operations", "1.0.0"))
	server.MustMount(operation)

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/invalid", nil))
	if recorder.Code != http.StatusBadRequest || recorder.Header().Get("Content-Type") != MIMEProblemJSON {
		t.Fatalf("problem response = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), `"application/problem+json"`) || !strings.Contains(string(document), `"instance"`) {
		t.Fatalf("OpenAPI problem schema missing: %s", document)
	}
	if !strings.Contains(string(document), `"operationId":"get_users_by_id"`) {
		t.Fatalf("OpenAPI inferred operationId missing: %s", document)
	}
}

func TestOperationConstraintsDriveRuntimeAndOpenAPI(t *testing.T) {
	input := MapInputs(
		PathInt64("id", Minimum(1), Maximum(10)),
		QueryString("state", AllowedValues("open", "closed")),
		func(id int64, state string) operationLookup {
			return operationLookup{ID: id, Locale: state}
		},
	)
	server := New(WithOpenAPI("operations", "1.0.0"))
	server.MustMount(GetJSON("/items/{id}", input, func(_ context.Context, lookup operationLookup) (map[string]any, error) {
		return map[string]any{"id": lookup.ID, "state": lookup.Locale}, nil
	}))

	for _, test := range []struct {
		path   string
		status int
	}{
		{path: "/items/0?state=open", status: http.StatusBadRequest},
		{path: "/items/1?state=invalid", status: http.StatusBadRequest},
		{path: "/items/10?state=closed", status: http.StatusOK},
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.status {
			t.Fatalf("%s status = %d, want %d, body = %q", test.path, recorder.Code, test.status, recorder.Body.String())
		}
	}

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"minimum":1`, `"maximum":10`, `"enum":["open","closed"]`} {
		if !strings.Contains(string(document), fragment) {
			t.Fatalf("OpenAPI missing %s: %s", fragment, document)
		}
	}
}

func TestOperationMultipartHonorsRouteLimitAndDocumentsBinaryBody(t *testing.T) {
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	fileWriter, err := multipartWriter.CreateFormFile("file", "payload.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(fileWriter, strings.Repeat("x", 128)); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}

	operation := Handle(
		Post("/upload"),
		MultipartFile("file", 1<<20),
		JSONOutput[map[string]string](),
		func(_ context.Context, file *FileHeader) (map[string]string, error) {
			return map[string]string{"filename": file.Filename}, nil
		},
	).WithMaxBodyBytes(64)
	server := New(WithOpenAPI("operations", "1.0.0"), WithMaxBodyBytes(1<<20))
	server.MustMount(operation)

	request := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("upload status = %d, body = %q", recorder.Code, recorder.Body.String())
	}

	document, err := server.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"multipart/form-data"`, `"format":"binary"`, `"required":["file"]`} {
		if !strings.Contains(string(document), fragment) {
			t.Fatalf("OpenAPI missing %s: %s", fragment, document)
		}
	}
}
