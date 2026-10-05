package ghttpotel

import (
	"net/http"

	"github.com/sofiworker/gk/gotel"
)

// HeaderCarrier 是基于 http.Header 的 gotel.TextMapCarrier。
// HeaderCarrier is a gotel.TextMapCarrier backed by http.Header.
type HeaderCarrier http.Header

var _ gotel.TextMapCarrier = HeaderCarrier(nil)

// Get 返回键对应的首个值。
// Get returns the first value for key.
func (c HeaderCarrier) Get(key string) string { return http.Header(c).Get(key) }

// Set 设置键值。
// Set sets key to value.
func (c HeaderCarrier) Set(key, value string) { http.Header(c).Set(key, value) }

// Keys 返回所有键。
// Keys returns all keys.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
