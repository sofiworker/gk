package gresolver

import (
	"context"
	"net"
	"testing"
)

type mockResolver struct{}

func (m *mockResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
}
func (m *mockResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return []string{"localhost"}, nil
}
func (m *mockResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	return "localhost", nil
}
func (m *mockResolver) Scheme() string {
	return "mock"
}

func TestRegisterAndGetResolver(t *testing.T) {
	r := &mockResolver{}
	resolverFactory.Register(r.Scheme(), r)

	resolverFactory.mu.RLock()
	defer resolverFactory.mu.RUnlock()
	got, ok := resolverFactory.resolvers["mock"]
	if !ok {
		t.Fatalf("resolver not registered")
	}
	if got.Scheme() != "mock" {
		t.Errorf("expected scheme 'mock', got %s", got.Scheme())
	}
}

func TestMockResolverMethods(t *testing.T) {
	r := &mockResolver{}
	ctx := context.Background()

	ips, err := r.LookupIPAddr(ctx, "localhost")
	if err != nil || len(ips) == 0 || ips[0].IP.String() != "127.0.0.1" {
		t.Errorf("LookupIPAddr failed: %v, %v", ips, err)
	}

	hosts, err := r.LookupHost(ctx, "localhost")
	if err != nil || len(hosts) == 0 || hosts[0] != "localhost" {
		t.Errorf("LookupHost failed: %v, %v", hosts, err)
	}

	cname, err := r.LookupCNAME(ctx, "localhost")
	if err != nil || cname != "localhost" {
		t.Errorf("LookupCNAME failed: %v, %v", cname, err)
	}
}
