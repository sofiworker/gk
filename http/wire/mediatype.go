package wire

import (
	"slices"
	"strings"
)

// 规范化的 media-type 常量。两端共用同一份字面量，避免 415 判定与解码分派各写一份而漂移。
// Canonical media-type constants. Both sides share one set of literals so the 415
// decision and decoder dispatch cannot drift apart.
const (
	ContentTypeJSON      = "application/json"
	ContentTypeXML       = "application/xml"
	ContentTypeForm      = "application/x-www-form-urlencoded"
	ContentTypeMultipart = "multipart/form-data"
	ContentTypeText      = "text/plain"
	ContentTypeSSE       = "text/event-stream"
)

// MediaType 取 Content-Type 头的 media-type 部分（到第一个 ';' 为止，去空白并转小写）。
// 不做完整 RFC 解析——415 判定只需比对主类型，参数（charset/boundary）无关。
// MediaType extracts the media-type part of a Content-Type header (up to the first
// ';', trimmed and lowercased). It does no full RFC parse — a 415 decision only needs
// the base type; parameters (charset/boundary) are irrelevant.
func MediaType(contentType string) string {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	return strings.ToLower(strings.TrimSpace(contentType))
}

// ContentTypeIn 报告收到的 Content-Type 是否属于 want 集合（按 media-type 比对）。
// got 为空时放行（交给解码器处理空体），want 为空集时不校验。
//
// want 应已归一化为小写 media-type（例如在注册期），此处只归一化 got 一次；集合很小，
// 线性比对比 map 更快且零分配。
// ContentTypeIn reports whether the received Content-Type belongs to the want set
// (compared by media-type). An empty got passes (deferring empty bodies to the
// decoder), and an empty want set means no check.
//
// want is expected to be normalized lowercase media-types (e.g. at registration), so
// only got is normalized here; the set is tiny, so a linear compare beats a map and
// allocates nothing.
func ContentTypeIn(got string, want []string) bool {
	if got == "" || len(want) == 0 {
		return true
	}
	return slices.Contains(want, MediaType(got))
}
