package gresolver

import (
	"context"
	"net"
)

type PureGoResolver struct {
	*net.Resolver
}

func NewPureGoResolver() *PureGoResolver {
	return &PureGoResolver{
		Resolver: &net.Resolver{
			PreferGo: true,
		},
	}
}

func (p *PureGoResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return p.Resolver.LookupIPAddr(ctx, host)
}

func (p *PureGoResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return p.Resolver.LookupHost(ctx, host)
}

func (p *PureGoResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	return p.Resolver.LookupCNAME(ctx, host)
}

func (p *PureGoResolver) Scheme() string {
	return "pure-go"
}
