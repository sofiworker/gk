package gresolver

import (
	"context"
	"net"
	"testing"
)

func TestNewSystemResolver(t *testing.T) {
	r := NewSystemResolver()
	if r == nil {
		t.Fatal("NewSystemResolver() 返回了 nil")
	}
	if got := r.Scheme(); got != "system" {
		t.Errorf("Scheme() = %v, want %v", got, "system")
	}
}

func TestSystemResolver_LookupMethods(t *testing.T) {
	server := newTestDNSServer(t)
	server.answerA("google.com.", [4]byte{8, 8, 8, 8})
	server.answerCNAME("google.com.", "dns.google.")

	resolver := &SystemResolver{
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				dialer := &net.Dialer{}
				return dialer.DialContext(ctx, "udp", server.address())
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
		if len(ips) == 0 || ips[0].IP.String() != "8.8.8.8" {
			t.Errorf("LookupIPAddr() 返回错误结果: %v", ips)
		}
	})

	t.Run("测试 LookupHost", func(t *testing.T) {
		hosts, err := resolver.LookupHost(ctx, testDomain)
		if err != nil {
			t.Errorf("LookupHost() error = %v", err)
			return
		}
		if len(hosts) == 0 || hosts[0] != "8.8.8.8" {
			t.Errorf("LookupHost() 返回错误结果: %v", hosts)
		}
	})

	t.Run("测试 LookupCNAME", func(t *testing.T) {
		cname, err := resolver.LookupCNAME(ctx, testDomain)
		if err != nil {
			t.Errorf("LookupCNAME() error = %v", err)
			return
		}
		if cname != "dns.google." {
			t.Errorf("LookupCNAME() 返回错误结果: %v", cname)
		}
	})
}
