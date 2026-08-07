//go:build go1.27

package ghttp

import "context"

// Get sends a typed GET request. Req is unused for the response type only;
// the type parameter keeps the API symmetric with the other verbs.
func (c *Client) Get[Req, Resp any](ctx context.Context, path string) (*Resp, error) {
	return GET[Req, Resp](ctx, c, path)
}

// Post sends a typed POST request.
func (c *Client) Post[Req, Resp any](ctx context.Context, path string, req *Req) (*Resp, error) {
	return POST[Req, Resp](ctx, c, path, req)
}

// Put sends a typed PUT request.
func (c *Client) Put[Req, Resp any](ctx context.Context, path string, req *Req) (*Resp, error) {
	return PUT[Req, Resp](ctx, c, path, req)
}

// Delete sends a typed DELETE request.
func (c *Client) Delete[Req, Resp any](ctx context.Context, path string) (*Resp, error) {
	return DELETE[Req, Resp](ctx, c, path)
}
