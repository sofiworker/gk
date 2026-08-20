package ghttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"reflect"
	"strings"
)

var (
	ErrRouteProducesUnsupported = errors.New("route produces content type is unsupported")

	allHTTPMethods = []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
	}
)

type routeTarget interface {
	routePath(path string) string
	routeGroup() *Group
	consumesContentTypes() []string
	producesContentTypes() []string
	owner() *Server
}

type responseCodec struct {
	contentType string
	codec       Codec
}

type responseHeader struct {
	name        string
	value       string
	description string
}

// writeRouteError 依次分发到 Operation、Server 与内置错误 writer。
// writeRouteError dispatches through Operation, Server and built-in error writers.
func writeRouteError(writer http.ResponseWriter, request *http.Request, server *Server, operationWriter ErrorWriter, produces []string, defaultCode int, err error) {
	if server == nil {
		writeErrorWithCodec(writer, request, nil, defaultCode, err, produces, nil)
		return
	}
	if server.dispatchError(writer, request, defaultCode, err) {
		return
	}
	status := statusCodeFromError(defaultCode, err)
	if operationWriter != nil && operationWriter(writer, request, status, err) {
		return
	}
	if server.config.errorWriter != nil && server.config.errorWriter(writer, request, status, err) {
		return
	}
	writeErrorWithCodec(writer, request, server, defaultCode, err, produces, nil)
}

func requestWithMaxBodyBytes(writer http.ResponseWriter, request *http.Request, maxBodyBytes int64) *http.Request {
	if maxBodyBytes <= 0 || request == nil || request.Body == nil || request.Body == http.NoBody {
		return request
	}
	limited := request.WithContext(request.Context())
	limited.Body = http.MaxBytesReader(writer, request.Body, maxBodyBytes)
	return limited
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

func validateRequestContentType(request *http.Request, hasBody bool, consumes []string) error {
	if len(consumes) == 0 || !hasBody {
		return nil
	}
	contentType := request.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		return Err(http.StatusUnsupportedMediaType, "missing Content-Type", WithCause(ErrUnsupportedMediaType))
	}
	mediaType := fastNormalizeContentType(contentType)
	for _, allowed := range consumes {
		if mediaTypeMatches(allowed, mediaType) {
			return nil
		}
	}
	return Err(http.StatusUnsupportedMediaType, fmt.Sprintf("unsupported media type %q", contentType), WithCause(ErrUnsupportedMediaType))
}

// fastNormalizeContentType 对最常见的媒体类型形态做 O(1) 精确比较,避免每请求
// mime.ParseMediaType 的完整解析;其它形态回退完整归一化。
// fastNormalizeContentType matches the most common media-type spellings with
// O(1) comparisons, avoiding a full mime.ParseMediaType per request; other
// shapes fall back to full normalization.
func fastNormalizeContentType(contentType string) string {
	switch contentType {
	case "application/json", "application/json; charset=utf-8":
		return "application/json"
	case "application/x-www-form-urlencoded", "application/x-www-form-urlencoded; charset=utf-8":
		return "application/x-www-form-urlencoded"
	case "multipart/form-data", "multipart/form-data; charset=utf-8":
		return "multipart/form-data"
	}
	return normalizeContentType(contentType)
}

func normalizeContentTypes(contentTypes []string) []string {
	normalized := make([]string, 0, len(contentTypes))
	for _, contentType := range contentTypes {
		contentType = normalizeContentType(contentType)
		if contentType != "" {
			normalized = append(normalized, contentType)
		}
	}
	return normalized
}

func normalizeContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		return strings.ToLower(mediaType)
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
}

func mediaTypeMatches(allowed, actual string) bool {
	if allowed == "*/*" {
		return true
	}
	if strings.HasSuffix(allowed, "/*") {
		return strings.HasPrefix(actual, strings.TrimSuffix(allowed, "*"))
	}
	return allowed == actual
}

func isHTTPMethodToken(method string) bool {
	if method == "" {
		return false
	}
	for index := 0; index < len(method); index++ {
		character := method[index]
		if character <= 32 || character >= 127 {
			return false
		}
		switch character {
		case '(', ')', '<', '>', '@', ',', ';', ':', '\\', '"', '/', '[', ']', '?', '=', '{', '}', ' ', '\t':
			return false
		}
	}
	return true
}

func writeError(writer http.ResponseWriter, request *http.Request, server *Server, defaultCode int, err error) {
	if server != nil && server.dispatchError(writer, request, defaultCode, err) {
		return
	}
	if server != nil && server.config.errorWriter != nil && server.config.errorWriter(writer, request, statusCodeFromError(defaultCode, err), err) {
		return
	}
	writeErrorWithCodec(writer, request, server, defaultCode, err, nil, nil)
}

func writeErrorWithCodec(writer http.ResponseWriter, request *http.Request, server *Server, defaultCode int, err error, produces []string, codecs []responseCodec) {
	if responseErrorWriteBlocked(request) {
		return
	}
	if server == nil {
		http.Error(writer, err.Error(), statusCodeFromError(defaultCode, err))
		return
	}
	status := statusCodeFromError(defaultCode, err)
	if server.config.problemDetails {
		writeProblemDetails(writer, request, server, status, err)
		return
	}
	body := HTTPError{Code: status, Message: http.StatusText(status), Err: err}
	if httpError := AsError(err); httpError != nil {
		body = *httpError
	} else if server.config.exposeErrorDetails {
		body.Message = err.Error()
	}
	if strings.TrimSpace(body.Message) == "" {
		body.Message = http.StatusText(status)
	}
	contentType, codec, _ := selectResponseCodec(server, request.Header.Get("Accept"), produces, codecs)
	if codec == nil {
		contentType = MIMEJSON
		if server.resolvedJSONCodec != nil {
			codec = server.resolvedJSONCodec
		} else {
			codec, _ = server.codecMgr.Resolve(MIMEJSON)
		}
	}
	if server.envelope != nil {
		server.envelope(writer, request, status, nil, err, contentType, codec)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	writer.WriteHeader(status)
	_ = codec.Marshal(writer, &body)
}

func writeProblemDetails(writer http.ResponseWriter, request *http.Request, server *Server, status int, err error) {
	detail := http.StatusText(status)
	if httpError := AsError(err); httpError != nil {
		if httpError.Message != "" {
			detail = httpError.Message
		}
	} else if server.config.exposeErrorDetails {
		detail = err.Error()
	}
	body := map[string]any{
		"type":     "about:blank",
		"title":    http.StatusText(status),
		"status":   status,
		"detail":   detail,
		"instance": request.URL.Path,
	}
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func statusCodeFromError(defaultCode int, err error) int {
	if httpError := AsError(err); httpError != nil {
		return httpError.Code
	}
	return defaultCode
}

func responseHasBody(status int) bool {
	return status >= http.StatusOK && status != http.StatusNoContent && status != http.StatusNotModified
}

func selectResponseCodec(server *Server, accept string, produces []string, codecs []responseCodec) (string, Codec, bool) {
	if len(codecs) == 0 {
		if len(produces) == 0 && server != nil && server.resolvedCodecs != nil {
			// 默认 produces 走注册期预解析结果,跳过每请求 Resolve。
			// default produces reuse the registration-time resolution,
			// skipping per-request Resolve.
			codecs = server.resolvedCodecs
		} else {
			if len(produces) == 0 && server != nil {
				produces = server.produces
			}
			codecs = resolveResponseCodecs(server, produces)
		}
	}
	if len(codecs) == 0 {
		return "", nil, false
	}
	if len(codecs) == 1 && (accept == "" || accept == "*/*") {
		return codecs[0].contentType, codecs[0].codec, true
	}
	candidates := make([]string, len(codecs))
	for index, candidate := range codecs {
		candidates[index] = candidate.contentType
	}
	contentType, codec, ok := server.codecMgr.Select(accept, candidates)
	if !ok {
		return "", nil, false
	}
	return contentType, codec, true
}

func resolveResponseCodecs(server *Server, produces []string) []responseCodec {
	if server == nil {
		return nil
	}
	codecs := make([]responseCodec, 0, len(produces))
	for _, contentType := range produces {
		codec, ok := server.codecMgr.Resolve(contentType)
		if ok {
			codecs = append(codecs, responseCodec{contentType: contentType, codec: codec})
		}
	}
	return codecs
}

func isNilHTTPHandler(handler http.Handler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	return (value.Kind() == reflect.Func || value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface) && value.IsNil()
}

func renderHTML(writer http.ResponseWriter, server *Server, status int, name string, data any) error {
	if server.renderer == nil {
		return ErrRendererNotConfigured
	}
	var body bytes.Buffer
	if err := server.renderer.Render(name, data, &body); err != nil {
		return err
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(status)
	_, err := writer.Write(body.Bytes())
	return err
}
