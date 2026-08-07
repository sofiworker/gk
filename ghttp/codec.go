package ghttp

import "io"

// Codec 处理 HTTP 内容编码/解码。
// Codec handles HTTP content encoding/decoding.
// 每个 Codec 关联一个或多个 Content-Type。
// Each Codec is associated with one or more Content-Types.
type Codec interface {
	ContentTypes() []string
	Marshal(w io.Writer, v interface{}) error
	Unmarshal(r io.Reader, v interface{}) error
}
