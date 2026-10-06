package ghttp

import "net/http"

// WS declares a GET WebSocket route. Its arguments follow the usual
// path, handler, options convention. The handler calls Upgrade on its chosen
// WebSocket library using resp and req.Raw, and owns the connection lifecycle.
//
// ghttp supplies routing, middleware and route metadata; it does not negotiate
// the handshake, wrap connections or implement the WebSocket protocol.
// WithInput and WithOutput are not applicable to WS routes.
func WS(path string, handler RawHandlerFunc, opts ...Option) Route {
	r := Raw(http.MethodGet, path, handler, opts...)
	r.meta.kind = RouteWebSocket
	return r
}
