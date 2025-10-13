package gclient

type (
	// RequestMiddleware type is for request middleware, called before a request is sent
	RequestMiddleware func(client *Client, req *Request) error

	// ResponseMiddleware type is for response middleware, called after a response has been received
	ResponseMiddleware func(client *Client, resp *Response) error
)
