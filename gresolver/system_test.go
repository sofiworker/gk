package gresolver

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNewSystemResolver(t *testing.T) {
	r := NewSystemResolver()
	if r == nil {
		t.Fatal("NewSystemResolver() 返回了 nil")
	}
	if got := r.Scheme(); got != "default" {
		t.Errorf("Scheme() = %v, want %v", got, "default")
	}
}

func TestSystemResolver_LookupMethods(t *testing.T) {
	resolver := &SystemResolver{
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				dialer := &net.Dialer{
					Timeout: time.Second,
				}
				return dialer.DialContext(ctx, "udp", "8.8.8.8:53")
			},
		},
	}

	ctx := context.Background()
	testDomain := "google.com"

	t.Run("测试 LookupIPAddr", func(t *testing.T) {
		ips, err := resolver.LookupIPAddr(ctx, testDomain)
		if err != nil {
			t.Errorf("LookupIPAddr() error = %v", err)
			return
		}
		if len(ips) == 0 {
			t.Error("LookupIPAddr() 返回了空的IP列表")
		}
	})

	t.Run("测试 LookupHost", func(t *testing.T) {
		hosts, err := resolver.LookupHost(ctx, testDomain)
		if err != nil {
			t.Errorf("LookupHost() error = %v", err)
			return
		}
		if len(hosts) == 0 {
			t.Error("LookupHost() 返回了空的主机列表")
		}
	})

	t.Run("测试 LookupCNAME", func(t *testing.T) {
		cname, err := resolver.LookupCNAME(ctx, testDomain)
		if err != nil {
			t.Errorf("LookupCNAME() error = %v", err)
			return
		}
		if cname == "" {
			t.Error("LookupCNAME() 返回了空的CNAME")
		}
	})
}
