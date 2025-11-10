package gcodec

import (
	"io"
	"strings"
	"sync"
)

// HTTPCodec is a manager that uses a map of codecs to handle content negotiation
// for HTTP requests and responses.
// It implements the ghttp.Codec interface.
type HTTPCodec struct {
	codecs sync.Map // map[string]Codec
}

// NewHTTPCodec creates a new HTTPCodec and registers the default codecs.
func NewHTTPCodec() *HTTPCodec {
	hc := &HTTPCodec{}

	// Register default codecs for common content types
	hc.RegisterCodec("application/json", NewJSONCodec())
	hc.RegisterCodec("application/xml", NewXMLCodec())
	hc.RegisterCodec("text/xml", NewXMLCodec())
	hc.RegisterCodec("application/x-yaml", NewYAMLCodec())
	hc.RegisterCodec("application/yaml", NewYAMLCodec())
	hc.RegisterCodec("text/yaml", NewYAMLCodec())
	hc.RegisterCodec("text/plain", NewPlainCodec())

	return hc
}

// RegisterCodec registers a codec for a given content type.
// Content types are normalized to lowercase and without parameters.
func (hc *HTTPCodec) RegisterCodec(contentType string, codec Codec) {
	hc.codecs.Store(normalizeContentType(contentType), codec)
}

// GetCodec returns the codec for a given content type.
// The content type is normalized before lookup.
func (hc *HTTPCodec) GetCodec(contentType string) (Codec, bool) {
	normalized := normalizeContentType(contentType)
	if codec, ok := hc.codecs.Load(normalized); ok {
		return codec.(Codec), true
	}
	return nil, false
}

func (hc *HTTPCodec) Encode(w io.Writer, v interface{}) error {
	return nil
}

func (hc *HTTPCodec) Decode(r io.Reader, v interface{}) error {
	return nil
}

// normalizeContentType standardizes a content type string.
// It converts to lowercase and removes any parameters (e.g., ; charset=utf-8).
func normalizeContentType(ct string) string {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = ct[:idx]
	}
	return ct
}
