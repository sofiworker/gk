package client

import (
	"errors"
	"strconv"

	"github.com/sofiworker/gk/gerr"
)

// 本文件把 client 的错误接入基础契约层的 gerr：领域错误的 Kind 语义在仓库里只定义一次，
// client 不再自创一套分类。
// This file wires the client's error into the base-contract layer's gerr: the domain
// Kind vocabulary is defined once in this repository, and the client does not invent a
// second classification.

// Kind 返回本次失败对应的 gerr.Kind。传输层失败按超时/取消/不可用细分；收到响应时按
// 状态码映射。
// Kind returns the gerr.Kind for this failure. Transport failures split into
// timeout/canceled/unavailable; a received response maps from its status code.
func (e *Error) Kind() gerr.Kind {
	if e.StatusCode == 0 {
		switch {
		case gerr.IsTimeout(e.Err):
			return gerr.KindTimeout
		case gerr.IsCanceled(e.Err):
			return gerr.KindCanceled
		case errors.Is(e.Err, ErrBodyNotReplayable), errors.Is(e.Err, ErrNoBaseURL):
			// 这两类属于调用方的配置/用法错误，而非对端不可用。
			// These two are the caller's configuration/usage mistakes, not peer
			// unavailability.
			return gerr.KindInvalid
		default:
			return gerr.KindUnavailable
		}
	}
	switch {
	case e.StatusCode == 400, e.StatusCode == 422:
		return gerr.KindInvalid
	case e.StatusCode == 401, e.StatusCode == 403:
		return gerr.KindPermission
	case e.StatusCode == 404:
		return gerr.KindNotFound
	case e.StatusCode == 409:
		return gerr.KindConflict
	case e.StatusCode == 408, e.StatusCode == 504:
		return gerr.KindTimeout
	case e.StatusCode == 429, e.StatusCode == 502, e.StatusCode == 503:
		return gerr.KindUnavailable
	case e.StatusCode >= 500:
		return gerr.KindInternal
	case e.StatusCode >= 400:
		return gerr.KindInvalid
	default:
		// 非 2xx 但也不是 4xx/5xx（例如被接受的 3xx 之外的怪状态）：保守归为内部错误。
		// Non-2xx but neither 4xx nor 5xx (an odd status outside accepted 3xx): fall back
		// to internal.
		return gerr.KindInternal
	}
}

// Gerr 把本错误转换成 *gerr.Error，保留状态码为 Code、方法为 Op，并把底层错误挂到
// Cause 上（errors.Is/As 链路因此贯通到 gerr 世界）。
// Gerr converts this error into a *gerr.Error, keeping the status as Code and the method
// as Op, and attaching the cause so errors.Is/As chains reach into the gerr world.
func (e *Error) Gerr() *gerr.Error {
	opts := []gerr.Option{gerr.WithKind(e.Kind()), gerr.WithOp(e.Op)}
	if e.StatusCode != 0 {
		opts = append(opts, gerr.WithCode(strconv.Itoa(e.StatusCode)))
	}
	if e.Err != nil {
		opts = append(opts, gerr.WithCause(e.Err))
	}
	return gerr.New(e.Error(), opts...)
}

// AsGerr 把 client 侧的错误转换为 gerr 世界的 error；不是 *Error 时原样返回。
// AsGerr converts a client-side error into the gerr world; a non-*Error value passes
// through unchanged.
func AsGerr(err error) error {
	if err == nil {
		return nil
	}
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Gerr()
	}
	return err
}
