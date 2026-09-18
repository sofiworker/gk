// Package modelhttp 组合模型协议适配器与 HTTP 传输，不定义默认厂商协议。
// Package modelhttp composes model protocol adapters with HTTP transport without defining a default vendor protocol.
package modelhttp

import (
	"context"
	"errors"

	"github.com/sofiworker/gk/gai/model"
	"github.com/sofiworker/gk/gai/transport/httptransport"
)

var ErrConfig = errors.New("invalid model HTTP adapter configuration")

// Protocol 由厂商适配实现持有独立配置，包含路由、凭据及厂商专属选项。
// Protocol implementations hold separate routing, credential and vendor-option configuration.
// Decode 必须处理状态码与响应头，返回通用模型身份、结束原因和用量。
// Decode must handle status and headers and return neutral identity, finish reason and usage.
type Protocol interface {
	Encode(context.Context, model.Request) (httptransport.Request, error)
	Decode(context.Context, httptransport.Response) (model.Response, error)
}
type Transport interface {
	Do(context.Context, httptransport.Request) (httptransport.Response, error)
}
type Client struct {
	protocol  Protocol
	transport Transport
}

func New(protocol Protocol, transport Transport) (*Client, error) {
	if protocol == nil || transport == nil {
		return nil, ErrConfig
	}
	return &Client{protocol: protocol, transport: transport}, nil
}
func (c *Client) Generate(ctx context.Context, r model.Request) (model.Response, error) {
	if err := ctx.Err(); err != nil {
		return model.Response{}, err
	}
	if r.Model.ID == "" {
		return model.Response{}, ErrConfig
	}
	if err := r.Parameters.Validate(); err != nil {
		return model.Response{}, err
	}
	request, err := c.protocol.Encode(ctx, r)
	if err != nil {
		return model.Response{}, err
	}
	response, err := c.transport.Do(ctx, request)
	if err != nil {
		return model.Response{}, err
	}
	return c.protocol.Decode(ctx, response)
}
