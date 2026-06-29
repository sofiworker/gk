package ghttp

import "net/http"

// Context is the request context interface exposed to handlers.
type Context interface {
	ResponseWriter() http.ResponseWriter
	Request() *http.Request
}
