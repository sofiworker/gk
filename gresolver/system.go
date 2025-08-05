package gresolver

import (
	"context"
	"net"
)

// SystemResolver 使用系统DNS设置的解析器
type SystemResolver struct {
	*net.Resolver
}

// NewSystemResolver 创建一个系统DNS解析器
func NewSystemResolver() *SystemResolver {
	return &SystemResolver{
		Resolver: net.DefaultResolver,
	}
}

func (r *SystemResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.Resolver.LookupIPAddr(ctx, host)
}

func (r *SystemResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return r.Resolver.LookupHost(ctx, host)
}

func (r *SystemResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	return r.Resolver.LookupCNAME(ctx, host)
}

func (r *SystemResolver) Scheme() string {
	return "default"
}
