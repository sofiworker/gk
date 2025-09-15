package gresolver

import (
	"context"
	"fmt"
	"net"
	"sync"
)

var (
	resolverFactory        *ResolverFactory
	ErrInvalidResolverName = fmt.Errorf("invalid resolver name")
	ErrAlreadyRegistered   = fmt.Errorf("resolver already registered")
)

type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)

	LookupHost(ctx context.Context, host string) ([]string, error)

	LookupCNAME(ctx context.Context, host string) (string, error)

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

	systemResolver := NewSystemResolver()
	_ = resolverFactory.Register(systemResolver.Scheme(), systemResolver)

	pureGoResolver := NewPureGoResolver()
	_ = resolverFactory.Register(pureGoResolver.Scheme(), pureGoResolver)

	defaultResolver := NewDefaultResolver()
	_ = resolverFactory.Register(defaultResolver.Scheme(), defaultResolver)
}

func (f *ResolverFactory) Register(name string, resolver Resolver) error {
	if name == "" {
		return ErrInvalidResolverName
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.resolvers[name]; ok {
		return ErrAlreadyRegistered
	}
	f.resolvers[name] = resolver
	return nil
}

func (f *ResolverFactory) GetResolverByName(name string) (Resolver, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	resolver, ok := f.resolvers[name]
	if !ok {
		return nil, ErrInvalidResolverName
	}
	return resolver, nil
}

func IsDNSStyleDomain(host string) bool {
	if host == "" {
		return false
	}

	dotIndex := len(host) - 1
	hasDot := false

	for i := 0; i < len(host); i++ {
		if host[i] == '.' {
			hasDot = true
			dotIndex = i
			break
		}
	}

	if !hasDot {
		return false
	}

	if dotIndex == 0 || dotIndex == len(host)-1 {
		return false
	}

	return true
}

func ToDNSStyleDomain(host string) string {
	if IsDNSStyleDomain(host) {
		return host
	}

	if host == "" {
		return ""
	}
	return host + ".local"
}

func ToDNSQueryFormat(host string) string {
	if host == "" {
		return "."
	}

	if host[len(host)-1] == '.' {
		return host
	}

	if !IsDNSStyleDomain(host) {
		host = ToDNSStyleDomain(host)
	}

	return host + "."
}
