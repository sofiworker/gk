package ghttp

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	gwebsocket "github.com/gorilla/websocket"
)

// RedirectOperation 创建固定目标的重定向 Operation。
// RedirectOperation creates a redirect Operation with a fixed target.
func RedirectOperation(method, path string, status int, location string) *Operation {
	return HandleNoInput(Endpoint(method, path), RedirectOutput(status), func(context.Context) (RedirectResponse, error) {
		return RedirectResponse{Location: location}, nil
	})
}

// RedirectFuncOperation 创建由类型化输入计算目标的重定向 Operation。
// RedirectFuncOperation creates a redirect Operation whose target is computed from typed input.
func RedirectFuncOperation[I any](builder *EndpointBuilder, input Input[I], status int, redirect func(I) (string, error)) *Operation {
	if redirect == nil {
		if builder == nil {
			return &Operation{setupErr: ErrEndpointBuilderNil}
		}
		return &Operation{method: builder.method, path: builder.path, setupErr: ErrOperationHandlerNil}
	}
	return Handle(builder, input, RedirectOutput(status), func(_ context.Context, value I) (RedirectResponse, error) {
		location, err := redirect(value)
		return RedirectResponse{Location: location}, err
	})
}

// HTMLViewOperation 使用 Server 配置的 Renderer 创建 HTML Operation。
// HTMLViewOperation creates an HTML Operation using the Server-configured Renderer.
func HTMLViewOperation(method, path string, status int, name string, data any) *Operation {
	operation := &Operation{
		method: method, path: path, terminal: routeTerminalHTML, status: status,
		produces: []string{"text/html"},
	}
	if status < http.StatusContinue || status > 599 {
		operation.setupErr = fmt.Errorf("%w: %d", ErrOperationStatusInvalid, status)
		return operation
	}
	operation.openAPI.responseSchema = map[string]any{"type": "string"}
	operation.build = func(server *Server, mounted *Operation) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if err := renderHTML(writer, server, status, name, data); err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
			}
		})
	}
	return operation
}

// SSEOperation 创建带类型化输入的 Server-Sent Events Operation。
// SSEOperation creates a Server-Sent Events Operation with typed input.
func SSEOperation[I any](path string, input Input[I], handler func(context.Context, I, *SSEWriter) error) *Operation {
	if handler == nil {
		return &Operation{method: http.MethodGet, path: path, terminal: routeTerminalSSE, setupErr: ErrOperationHandlerNil}
	}
	operation := Handle(Get(path), input, SSEOutput(), func(ctx context.Context, value I) (func(*SSEWriter) error, error) {
		return func(stream *SSEWriter) error {
			return handler(ctx, value, stream)
		}, nil
	})
	operation.terminal = routeTerminalSSE
	return operation
}

// RawOperation 将原生 http.Handler 包装为一等 Operation。
// RawOperation wraps a native http.Handler as a first-class Operation.
func RawOperation(method, path string, handler http.Handler) *Operation {
	operation := &Operation{
		method:           method,
		path:             path,
		terminal:         routeTerminalRaw,
		stateIndependent: false,
	}
	if isNilHTTPHandler(handler) {
		operation.setupErr = ErrOperationHandlerNil
		return operation
	}
	operation.build = func(_ *Server, _ *Operation) http.Handler {
		return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, _ pathParamList) {
			handler.ServeHTTP(writer, request)
		})
	}
	return operation
}

// HTTPFuncOperation 将返回 error 的原生 HTTP 函数包装为一等 Operation。
// HTTPFuncOperation wraps a native HTTP function returning error as a first-class Operation.
func HTTPFuncOperation(method, path string, handler func(http.ResponseWriter, *http.Request) error) *Operation {
	operation := &Operation{
		method:           method,
		path:             path,
		terminal:         routeTerminalHTTPFunc,
		stateIndependent: false,
	}
	if handler == nil {
		operation.setupErr = ErrOperationHandlerNil
		return operation
	}
	operation.build = func(server *Server, mounted *Operation) http.Handler {
		return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, _ pathParamList) {
			if err := handler(writer, request); err != nil && !isErrHandled(err) {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
			}
		})
	}
	return operation
}

// HandleHTTP 将输入契约与自行写响应的 HTTP handler 编译为 Operation。
// HandleHTTP compiles an input contract and a response-writing HTTP handler into an Operation.
func HandleHTTP[I any](builder *EndpointBuilder, input Input[I], handler func(http.ResponseWriter, *http.Request, I) error) *Operation {
	if builder == nil {
		return &Operation{setupErr: ErrEndpointBuilderNil}
	}
	operation := &Operation{
		method:   builder.method,
		path:     builder.path,
		terminal: routeTerminalHTTPFunc,
	}
	if input == nil {
		operation.setupErr = ErrOperationInputNil
		return operation
	}
	if handler == nil {
		operation.setupErr = ErrOperationHandlerNil
		return operation
	}
	inputMetadata, inputErr := metadataOfInput(input)
	operation.openAPI = compileInputMetadata(inputMetadata)
	operation.setupErr = inputErr
	if inputMetadata.RequestBody != nil {
		for contentType := range inputMetadata.RequestBody.Content {
			operation.consumes = append(operation.consumes, normalizeContentType(contentType))
		}
	}
	operation.build = func(server *Server, mounted *Operation) http.Handler {
		return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, params pathParamList) {
			operationRequest := newOperationRequest(writer, request, params, server, mounted)
			value, err := input.build(&operationRequest)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
				return
			}
			if !validateOperationInput(writer, request, server, mounted, value) {
				return
			}
			if err := handler(writer, request, value); err != nil && !isErrHandled(err) {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusInternalServerError, err)
			}
		})
	}
	return operation
}

// StaticDirectory 创建从受限本地目录提供文件的 GET Operation。
// StaticDirectory creates a GET Operation serving files from a confined local directory.
func StaticDirectory(pathPrefix, root string) *Operation {
	fileSystem, err := NewSafeFS(root)
	if err != nil {
		return &Operation{method: http.MethodGet, path: staticOperationPath(pathPrefix), terminal: routeTerminalStatic, setupErr: err}
	}
	return StaticFileSystem(pathPrefix, fileSystem)
}

// StaticFileSystem 创建从 http.FileSystem 提供文件的 GET Operation。
// StaticFileSystem creates a GET Operation serving files from an http.FileSystem.
func StaticFileSystem(pathPrefix string, fileSystem http.FileSystem) *Operation {
	path := staticOperationPath(pathPrefix)
	if isNilHTTPFileSystem(fileSystem) {
		return &Operation{method: http.MethodGet, path: path, terminal: routeTerminalStatic, setupErr: ErrOperationFileSystemNil}
	}
	prefix := strings.TrimRight(joinRoutePaths("/", pathPrefix), "/")
	handler := http.StripPrefix(prefix, http.FileServer(fileSystem))
	operation := RawOperation(http.MethodGet, path, handler)
	operation.terminal = routeTerminalStatic
	return operation
}

// StaticFile 创建提供单个本地文件的 GET Operation。
// StaticFile creates a GET Operation serving one local file.
func StaticFile(path, filePath string) *Operation {
	if strings.TrimSpace(filePath) == "" {
		return &Operation{method: http.MethodGet, path: path, terminal: routeTerminalStatic, setupErr: ErrOperationFilePathRequired}
	}
	operation := RawOperation(http.MethodGet, path, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.ServeFile(writer, request, filePath)
	}))
	operation.terminal = routeTerminalStatic
	return operation
}

func staticOperationPath(pathPrefix string) string {
	return joinRoutePaths(pathPrefix, "{path...}")
}

func isNilHTTPFileSystem(fileSystem http.FileSystem) bool {
	if fileSystem == nil {
		return true
	}
	value := reflect.ValueOf(fileSystem)
	return (value.Kind() == reflect.Func || value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface) && value.IsNil()
}

// WebSocketOperation 创建带显式输入契约的 WebSocket GET Operation。
// WebSocketOperation creates a WebSocket GET Operation with an explicit input contract.
func WebSocketOperation[I any](path string, input Input[I], handler func(context.Context, I, *WebSocketConn) error) *Operation {
	operation := &Operation{
		method:           http.MethodGet,
		path:             path,
		terminal:         routeTerminalWebSocket,
		stateIndependent: false,
		status:           http.StatusSwitchingProtocols,
	}
	if input == nil {
		operation.setupErr = ErrOperationInputNil
		return operation
	}
	if handler == nil {
		operation.setupErr = ErrOperationHandlerNil
		return operation
	}
	inputMetadata, inputErr := metadataOfInput(input)
	operation.openAPI = compileInputMetadata(inputMetadata)
	operation.setupErr = inputErr
	operation.build = func(server *Server, mounted *Operation) http.Handler {
		return pathParamHandlerFunc(func(writer http.ResponseWriter, request *http.Request, params pathParamList) {
			operationRequest := newOperationRequest(writer, request, params, server, mounted)
			value, err := input.build(&operationRequest)
			if err != nil {
				writeRouteError(writer, request, server, mounted.errorWriter, mounted.produces, http.StatusBadRequest, err)
				return
			}
			if !validateOperationInput(writer, request, server, mounted, value) {
				return
			}

			checkOrigin := server.config.webSocketCheckOrigin
			if mounted.wsCheckOrigin != nil {
				checkOrigin = mounted.wsCheckOrigin
			}
			if checkOrigin == nil {
				checkOrigin = webSocketSameOrigin
			}
			upgrader := gwebsocket.Upgrader{
				CheckOrigin:     checkOrigin,
				Subprotocols:    server.config.webSocketSubprotocols,
				ReadBufferSize:  server.config.webSocketReadBufferSize,
				WriteBufferSize: server.config.webSocketWriteBufferSize,
			}
			webSocket, err := upgrader.Upgrade(writer, request, nil)
			if err != nil {
				if server.logger != nil {
					server.logger.WarnContext(request.Context(), "websocket upgrade failed", "error", err, "path", request.URL.Path)
				}
				return
			}

			connection := &WebSocketConn{conn: webSocket}
			defer connection.Close()
			stopKeepAlive := startWebSocketKeepAlive(connection, server.config.webSocketPingPeriod, server.config.webSocketPongWait)
			defer stopKeepAlive()
			if err := handler(request.Context(), value, connection); err != nil && server.logger != nil {
				server.logger.ErrorContext(request.Context(), "websocket handler error", "error", err, "path", request.URL.Path)
			}
		})
	}
	return operation
}
