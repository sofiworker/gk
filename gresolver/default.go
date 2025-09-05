package gresolver

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

type DefaultResolver struct {
	config *DnsConfig
}

// NewDefaultResolver 创建一个纯Go实现的DNS解析器
func NewDefaultResolver(opts ...Option) *DefaultResolver {
	config := &DnsConfig{
		Nameservers: DefaultNS,
		Ndots:       1,
		Timeout:     5,
		Attempts:    2,
	}
	for _, opt := range opts {
		opt(config)
	}
	config.Validate()
	return &DefaultResolver{
		config: config,
	}
}

func (r *DefaultResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	host = ToDNSQueryFormat(host)
	name, err := dnsmessage.NewName(host)
	if err != nil {
		return nil, &DNSError{Op: "lookup", Name: host, Err: ErrInvalidDomainName}
	}

	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:               uint16(time.Now().UnixNano()),
			Response:         false,
			OpCode:           0,
			RecursionDesired: true,
		},
		Questions: []dnsmessage.Question{
			{
				Name:  name,
				Type:  dnsmessage.TypeA,
				Class: dnsmessage.ClassINET,
			},
		},
	}

	resp, err := r.exchange(ctx, &msg)
	if err != nil {
		return nil, err
	}

	var addrs []net.IPAddr
	for _, answer := range resp.Answers {
		if answer.Header.Type == dnsmessage.TypeA {
			resource := answer.Body.(*dnsmessage.AResource)
			addrs = append(addrs, net.IPAddr{IP: resource.A[:]})
		}
	}

	if len(addrs) == 0 {
		return nil, &DNSError{Op: "lookup", Name: host, Err: ErrNoIPFound}
	}
	return addrs, nil
}

func (r *DefaultResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	addrs, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	hosts := make([]string, len(addrs))
	for i, addr := range addrs {
		hosts[i] = addr.IP.String()
	}
	return hosts, nil
}

func (r *DefaultResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	name, err := dnsmessage.NewName(host)
	if err != nil {
		return "", fmt.Errorf("invalid domain name: %v", err)
	}

	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:               uint16(time.Now().UnixNano()),
			Response:         false,
			RecursionDesired: true,
		},
		Questions: []dnsmessage.Question{
			{
				Name:  name,
				Type:  dnsmessage.TypeCNAME,
				Class: dnsmessage.ClassINET,
			},
		},
	}

	resp, err := r.exchange(ctx, &msg)
	if err != nil {
		return "", err
	}

	for _, answer := range resp.Answers {
		if answer.Header.Type == dnsmessage.TypeCNAME {
			resource := answer.Body.(*dnsmessage.CNAMEResource)
			return resource.CNAME.String(), nil
		}
	}

	return host, nil
}

func (r *DefaultResolver) exchange(ctx context.Context, msg *dnsmessage.Message) (*dnsmessage.Message, error) {
	packed, err := msg.Pack()
	if err != nil {
		return nil, &DNSError{Op: "pack", Err: err}
	}

	var lastErr error
	queryServer := func(ns string) error {
		_, _, err := net.SplitHostPort(ns)
		if err != nil {
			ns = net.JoinHostPort(ns, strconv.Itoa(dnsPort))
		}
		conn, err := net.DialTimeout("udp", ns, r.config.Timeout)
		if err != nil {
			return &DNSError{Op: "dial", Server: ns, Err: err}
		}
		defer conn.Close()

		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		} else {
			_ = conn.SetDeadline(time.Now().Add(r.config.Timeout))
		}

		if _, err := conn.Write(packed); err != nil {
			return &DNSError{Op: "write", Server: ns, Err: err}
		}

		resp := make([]byte, udpMaxSize)
		n, err := conn.Read(resp)
		if err != nil {
			return &DNSError{Op: "read", Server: ns, Err: err}
		}

		var response dnsmessage.Message
		if err := response.Unpack(resp[:n]); err != nil {
			return &DNSError{Op: "unpack", Server: ns, Err: err}
		}

		if response.Header.ID != msg.Header.ID {
			return &DNSError{Op: "verify", Server: ns, Err: ErrIDMismatch}
		}

		*msg = response
		return nil
	}

	for i := 0; i < r.config.Attempts; i++ {
		for _, ns := range r.config.Nameservers {
			if err := queryServer(ns); err != nil {
				lastErr = err
				continue
			}
			return msg, nil
		}
	}

	if lastErr != nil {
		return nil, &DNSError{Op: "resolve", Err: fmt.Errorf("all servers failed: %v", lastErr)}
	}
	return nil, &DNSError{Op: "resolve", Err: ErrNoResponse}
}

func (r *DefaultResolver) Scheme() string {
	return "default"
}
