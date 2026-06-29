package ghttp

import "io"

// Codec handles HTTP content encoding/decoding.
// Each Codec is associated with one or more Content-Types.
type Codec interface {
	ContentTypes() []string
	Marshal(w io.Writer, v interface{}) error
	Unmarshal(r io.Reader, v interface{}) error
}
