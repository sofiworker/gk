package gclient

import (
	"context"
	"errors"
	"net/http"
)

func (c *Client) Stream(method, rawURL string) (*http.Response, error) {
	return c.R().Stream(method, rawURL)
}

func (c *Client) GetStream(rawURL string) (*http.Response, error) {
	return c.Stream(http.MethodGet, rawURL)
}

func (r *Request) Stream(method, rawURL string) (*http.Response, error) {
	r.SetMethod(method)
	r.SetURL(rawURL)
	return r.effectiveClient().stream(r)
}

func (r *Request) GetStream(rawURL string) (*http.Response, error) {
	return r.Stream(http.MethodGet, rawURL)
}

func (c *Client) stream(r *Request) (*http.Response, error) {
	if r == nil {
		return nil, errors.New("request is nil")
	}

	fullURL, err := r.prepareURL()
	if err != nil {
		return nil, err
	}
	r.URL = fullURL

	if err := c.applyRequestMiddleware(r); err != nil {
		return nil, err
	}

	builder := newHTTPRequestBuilder(r, c)
	executor := c.effectiveExecutorForRequest(r)
	httpReq, err := builder.Build()
	if err != nil {
		return nil, err
	}
	r.RawRequest = httpReq

	if c.logger != nil && c.config.DumpConfig != nil && c.config.DumpConfig.DumpRequest {
		if dump, dumpErr := dumpHTTPRequest(httpReq); dumpErr == nil {
			c.logger.Debugf("request dump\n%s", dump)
		} else {
			c.logger.Warnf("dump request failed: %v", dumpErr)
		}
	}

	ctx := httpReq.Context()
	if ctx == nil {
		ctx = context.Background()
		httpReq = httpReq.WithContext(ctx)
	}

	tracer := c.tracer
	if r.tracer != nil {
		tracer = r.tracer
	}

	var spanEnd func()
	if tracer != nil {
		traceCtx, end := tracer.StartSpan(ctx)
		httpReq = httpReq.WithContext(traceCtx)
		spanEnd = end
		tracer.SetAttribute("http.method", httpReq.Method)
		tracer.SetAttribute("http.url", httpReq.URL.String())
	}

	var cancel context.CancelFunc
	if r.timeout > 0 {
		ctx, cancel = context.WithTimeout(httpReq.Context(), r.timeout)
		httpReq = httpReq.WithContext(ctx)
	}

	httpResp, execErr := executor.Do(httpReq)
	if cancel != nil {
		cancel()
	}
	if spanEnd != nil {
		spanEnd()
	}
	if execErr != nil {
		return nil, execErr
	}

	return httpResp, nil
}
