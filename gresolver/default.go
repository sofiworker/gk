package gresolver

import (
	"context"
	"net"
)

type DefaultResolver struct {
	remote []string
}

func NewDefaultResolver(remote ...string) *DefaultResolver {
	return &DefaultResolver{remote: remote}
}

//func (r *DefaultResolver) Resolve(name string) ([]net.Addr, error) {
//	return r.ResolveContext(context.Background(), name)
//}
//
//func (r *DefaultResolver) ResolveContext(ctx context.Context, name string) ([]net.Addr, error) {
//	if name == "" {
//		return nil, NotFoundMethodError
//	}
//	return nil, nil
//}

func (r *DefaultResolver) GoResolve(ctx context.Context, network, address string) (net.Conn, error) {
	return nil, nil
}
