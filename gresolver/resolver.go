package gresolver

import (
	"context"
	"net"
	"sync"
)

var (
	resolverFactory *ResolverFactory
)

type Resolver interface {
	ResolveContext(ctx context.Context, name string) ([]net.Addr, error)
	Scheme() string
}

type ResolverFactory struct {
	mu        sync.RWMutex
	resolvers map[string]Resolver
}

func init() {
	resolverFactory = &ResolverFactory{
		resolvers: make(map[string]Resolver),
	}
}

func (f *ResolverFactory) Register(name string, resolver Resolver) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolvers[name] = resolver
}
