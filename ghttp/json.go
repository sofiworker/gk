package ghttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/sofiworker/gk/ghttp/wire"
)

// WriteJSON 将数据以 JSON 格式写入响应。
// WriteJSON writes data to the response in JSON format.
//
// 自动设置 Content-Type 为 application/json; charset=utf-8。
// Automatically sets Content-Type to application/json; charset=utf-8.
func WriteJSON(resp *Response, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return writeBody(resp, status, "application/json; charset=utf-8", body)
}

// ReadJSON 从请求体读取并解析 JSON 数据。
// ReadJSON reads and parses JSON data from the request body.
//
// 采用 server 端严格策略：Content-Type 必须为 application/json（缺省时放行），请求体必须
// 恰好包含一个 JSON 值，拒绝空体与未知字段。错误统一为 HTTPError：415 表示媒体类型不符，
// 413 表示超出 http.MaxBytesReader 限额，400 表示格式非法；底层错误保存在 Cause 中，
// 可用 errors.Is(err, wire.ErrInvalidFormat) 判断。
// It applies the server's strict policy: Content-Type must be application/json (a
// missing header passes), the body must hold exactly one JSON value, and empty bodies
// and unknown fields are rejected. Errors are always HTTPError: 415 for a media-type
// mismatch, 413 when over an http.MaxBytesReader limit, 400 for invalid format; the
// underlying error is kept in Cause so errors.Is(err, wire.ErrInvalidFormat) works.
func ReadJSON(req *Request, v any) error {
	contentType := req.Raw.Header.Get("Content-Type")
	if !wire.ContentTypeIn(contentType, jsonMediaTypes) {
		return unsupportedMediaType(wire.ContentTypeJSON, contentType)
	}
	err := wire.DecodeJSON(req.Raw.Body, v, req.Raw.ContentLength,
		wire.RequireBody(), wire.DisallowUnknownFields())
	if err != nil {
		// A JSON decoder may stop at a syntax error before asking the lazy
		// decompressor for the byte that proves the decoded body is oversized.
		// Drain that decoder when present so the size limit keeps its 413 class.
		_, drainErr := io.Copy(io.Discard, req.Raw.Body)
		var mbe *http.MaxBytesError
		if errors.As(drainErr, &mbe) {
			err = drainErr
		} else if dec, ok := req.Raw.Body.(*decBody); ok && dec.err != nil {
			err = dec.err
		}
	}
	return bodyDecodeError(err)
}

// jsonMediaTypes 是 ReadJSON 接受的 media-type 集合。
// jsonMediaTypes is the media-type set accepted by ReadJSON.
var jsonMediaTypes = []string{wire.ContentTypeJSON}

// unsupportedMediaType 返回 415 错误。
// unsupportedMediaType returns a 415 error.
func unsupportedMediaType(want, got string) error {
	return HTTPError{
		Status:  http.StatusUnsupportedMediaType,
		Message: fmt.Sprintf("expected %s, got %s", want, wire.LogToken(wire.MediaType(got))),
	}
}

// bodyDecodeError 把 wire 解码错误映射为 HTTPError：超限为 413，其余为 400。
// bodyDecodeError maps a wire decode error to HTTPError: 413 for overrun, 400 otherwise.
func bodyDecodeError(err error) error {
	if err == nil {
		return nil
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return HTTPError{Status: http.StatusRequestEntityTooLarge, Message: "request body too large", Cause: err}
	}
	return HTTPError{Status: http.StatusBadRequest, Message: "invalid request body", Cause: err}
}

// DecodeJSON 从请求体解码 JSON 数据（泛型版本）。
// DecodeJSON decodes JSON data from the request body (generic version).
//
// 自动验证 Content-Type，拒绝未知字段。
// Automatically validates Content-Type and disallows unknown fields.
func DecodeJSON[T any](req *Request) (T, error) {
	var zero T
	if err := ReadJSON(req, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

// EncodeJSON 将数据以 JSON 格式编码到响应（泛型版本）。
// EncodeJSON encodes data to the response in JSON format (generic version).
//
// 默认使用 200 状态码。
// Uses 200 status code by default.
func EncodeJSON[T any](resp *Response, data T) error {
	return WriteJSON(resp, http.StatusOK, data)
}

// EncodeJSONWithStatus 将数据以 JSON 格式编码到响应，并指定状态码。
// EncodeJSONWithStatus encodes data to the response in JSON format with a specified status code.
func EncodeJSONWithStatus[T any](resp *Response, status int, data T) error {
	return WriteJSON(resp, status, data)
}
