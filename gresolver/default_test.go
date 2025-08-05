package gresolver

import (
	"context"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"testing"
)

// mock DefaultResolver.exchange 方法
type mockDefaultResolver struct {
	*DefaultResolver
}

func (r *mockDefaultResolver) exchange(ctx context.Context, msg *dnsmessage.Message) (*dnsmessage.Message, error) {
	// 根据请求类型返回模拟数据
	if len(msg.Questions) > 0 {
		switch msg.Questions[0].Type {
		case dnsmessage.TypeA:
			return &dnsmessage.Message{
				Answers: []dnsmessage.Resource{
					{
						Header: dnsmessage.ResourceHeader{Type: dnsmessage.TypeA},
						Body:   &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}},
					},
				},
			}, nil
		case dnsmessage.TypeCNAME:
			return &dnsmessage.Message{
				Answers: []dnsmessage.Resource{
					{
						Header: dnsmessage.ResourceHeader{Type: dnsmessage.TypeCNAME},
						Body:   &dnsmessage.CNAMEResource{CNAME: dnsmessage.Name{Length: 9, Data: [255]byte{'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't'}}},
					},
				},
			}, nil
		}
	}
	return nil, errors.New("mock error")
}

func TestNewDefaultResolver(t *testing.T) {
	r := NewDefaultResolver(nil)
	if r == nil || r.Scheme() != "default" {
		t.Errorf("NewDefaultResolver failed")
	}
}

func TestLookupIPAddr(t *testing.T) {
	r := &mockDefaultResolver{NewDefaultResolver(nil)}
	ctx := context.Background()
	ips, err := r.LookupIPAddr(ctx, "localhost.")
	if err != nil {
		t.Fatalf("LookupIPAddr error: %v", err)
	}
	if len(ips) == 0 || ips[0].IP.String() != "127.0.0.1" {
		t.Errorf("LookupIPAddr result error: %v", ips)
	}
}

func TestLookupHost(t *testing.T) {
	r := &mockDefaultResolver{NewDefaultResolver(nil)}
	ctx := context.Background()
	hosts, err := r.LookupHost(ctx, "localhost")
	if err != nil {
		t.Fatalf("LookupHost error: %v", err)
	}
	if len(hosts) == 0 || hosts[0] != "127.0.0.1" {
		t.Errorf("LookupHost result error: %v", hosts)
	}
}

func TestLookupCNAME(t *testing.T) {
	r := &mockDefaultResolver{NewDefaultResolver(nil)}
	ctx := context.Background()
	cname, err := r.LookupCNAME(ctx, "localhost")
	if err != nil {
		t.Fatalf("LookupCNAME error: %v", err)
	}
	if cname != "localhost" {
		t.Errorf("LookupCNAME result error: %v", cname)
	}
}
