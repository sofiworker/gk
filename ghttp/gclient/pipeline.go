package gclient

type RequestStep func(*Request) error

type RequestPipeline struct {
	client *Client
	steps  []RequestStep
}

func ComposeRequestSteps(steps ...RequestStep) RequestStep {
	return func(req *Request) error {
		return req.Apply(steps...)
	}
}

func (r *Request) Apply(steps ...RequestStep) error {
	for _, step := range steps {
		if step == nil {
			continue
		}
		if err := step(r); err != nil {
			return err
		}
	}
	return nil
}

func (r *Request) MustApply(steps ...RequestStep) *Request {
	if err := r.Apply(steps...); err != nil {
		panic(err)
	}
	return r
}

func (c *Client) NewPipeline(steps ...RequestStep) *RequestPipeline {
	return &RequestPipeline{
		client: c,
		steps:  append([]RequestStep(nil), steps...),
	}
}

func (p *RequestPipeline) Append(steps ...RequestStep) *RequestPipeline {
	p.steps = append(p.steps, steps...)
	return p
}

func (p *RequestPipeline) Steps() []RequestStep {
	return append([]RequestStep(nil), p.steps...)
}

func (p *RequestPipeline) Request() (*Request, error) {
	if p == nil || p.client == nil {
		return nil, nil
	}
	req := p.client.R()
	if err := req.Apply(p.steps...); err != nil {
		return nil, err
	}
	return req, nil
}

func (p *RequestPipeline) MustRequest() *Request {
	req, err := p.Request()
	if err != nil {
		panic(err)
	}
	return req
}

func (p *RequestPipeline) Execute(method, rawURL string) (*Response, error) {
	req, err := p.Request()
	if err != nil {
		return nil, err
	}
	return req.Execute(method, rawURL)
}
