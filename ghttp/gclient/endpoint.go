package gclient

import "net/http"

type Endpoint struct {
	client *Client
	Method string
	URL    string
	steps  []RequestStep
}

func NewEndpoint(client *Client, method, rawURL string, steps ...RequestStep) *Endpoint {
	return &Endpoint{
		client: client,
		Method: method,
		URL:    rawURL,
		steps:  append([]RequestStep(nil), steps...),
	}
}

func (c *Client) NewEndpoint(method, rawURL string, steps ...RequestStep) *Endpoint {
	return NewEndpoint(c, method, rawURL, steps...)
}

func (e *Endpoint) Clone() *Endpoint {
	if e == nil {
		return nil
	}
	cp := *e
	cp.steps = append([]RequestStep(nil), e.steps...)
	return &cp
}

func (e *Endpoint) SetMethod(method string) *Endpoint {
	e.Method = method
	return e
}

func (e *Endpoint) SetURL(rawURL string) *Endpoint {
	e.URL = rawURL
	return e
}

func (e *Endpoint) Use(steps ...RequestStep) *Endpoint {
	e.steps = append(e.steps, steps...)
	return e
}

func (e *Endpoint) Steps() []RequestStep {
	return append([]RequestStep(nil), e.steps...)
}

func (e *Endpoint) Request(extraSteps ...RequestStep) (*Request, error) {
	if e == nil || e.client == nil {
		return nil, nil
	}
	req := e.client.R()
	if e.Method != "" {
		req.SetMethod(e.Method)
	}
	if e.URL != "" {
		req.SetURL(e.URL)
	}
	if err := req.Apply(e.steps...); err != nil {
		return nil, err
	}
	if err := req.Apply(extraSteps...); err != nil {
		return nil, err
	}
	return req, nil
}

func (e *Endpoint) MustRequest(extraSteps ...RequestStep) *Request {
	req, err := e.Request(extraSteps...)
	if err != nil {
		panic(err)
	}
	return req
}

func (e *Endpoint) Execute(extraSteps ...RequestStep) (*Response, error) {
	req, err := e.Request(extraSteps...)
	if err != nil {
		return nil, err
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	return req.Execute(method, req.URL)
}

func (e *Endpoint) Stream(extraSteps ...RequestStep) (*http.Response, error) {
	req, err := e.Request(extraSteps...)
	if err != nil {
		return nil, err
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	return req.Stream(method, req.URL)
}

func (e *Endpoint) SSE(extraSteps ...RequestStep) (*SSERequest, error) {
	req, err := e.Request(extraSteps...)
	if err != nil {
		return nil, err
	}
	return req.NewSSERequest(), nil
}

func (e *Endpoint) WebSocket(extraSteps ...RequestStep) (*WebSocketRequest, error) {
	req, err := e.Request(extraSteps...)
	if err != nil {
		return nil, err
	}
	return req.NewWebSocketRequest(), nil
}
