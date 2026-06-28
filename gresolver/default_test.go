package gresolver

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestNewDefaultResolver(t *testing.T) {
	r := NewDefaultResolver(nil)
	if r == nil || r.Scheme() != "default" {
		t.Errorf("NewDefaultResolver failed")
	}
}

func TestLookupIPAddr(t *testing.T) {
	server := newTestDNSServer(t)
	server.answerA("example.com.", [4]byte{127, 0, 0, 1})

	r := NewDefaultResolver(
		WithNameservers([]string{server.address()}),
		WithTimeout(200*time.Millisecond),
		WithAttempts(1),
	)

	ips, err := r.LookupIPAddr(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("LookupIPAddr error: %v", err)
	}
	if len(ips) == 0 || ips[0].IP.String() != "127.0.0.1" {
		t.Fatalf("LookupIPAddr result error: %v", ips)
	}
}

func TestLookupHost(t *testing.T) {
	server := newTestDNSServer(t)
	server.answerA("example.com.", [4]byte{127, 0, 0, 1})

	r := NewDefaultResolver(
		WithNameservers([]string{server.address()}),
		WithTimeout(200*time.Millisecond),
		WithAttempts(1),
	)

	hosts, err := r.LookupHost(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("LookupHost error: %v", err)
	}
	if len(hosts) == 0 || hosts[0] != "127.0.0.1" {
		t.Fatalf("LookupHost result error: %v", hosts)
	}
}

func TestLookupCNAME(t *testing.T) {
	server := newTestDNSServer(t)
	server.answerCNAME("example.com.", "alias.example.com.")

	r := NewDefaultResolver(
		WithNameservers([]string{server.address()}),
		WithTimeout(200*time.Millisecond),
		WithAttempts(1),
	)

	cname, err := r.LookupCNAME(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("LookupCNAME error: %v", err)
	}
	if cname != "alias.example.com." {
		t.Fatalf("LookupCNAME result error: %v", cname)
	}
}

type testDNSServer struct {
	conn     net.PacketConn
	recordsA map[string][4]byte
	cnames   map[string]string
}

func newTestDNSServer(t *testing.T) *testDNSServer {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket failed: %v", err)
	}

	server := &testDNSServer{
		conn:     conn,
		recordsA: make(map[string][4]byte),
		cnames:   make(map[string]string),
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	go server.serve()
	return server
}

func (s *testDNSServer) address() string {
	return s.conn.LocalAddr().String()
}

func (s *testDNSServer) answerA(name string, ip [4]byte) {
	s.recordsA[name] = ip
}

func (s *testDNSServer) answerCNAME(name, target string) {
	s.cnames[name] = target
}

func (s *testDNSServer) serve() {
	buf := make([]byte, 2048)
	for {
		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			return
		}

		var req dnsmessage.Message
		if err := req.Unpack(buf[:n]); err != nil || len(req.Questions) == 0 {
			continue
		}

		resp := dnsmessage.Message{
			Header: dnsmessage.Header{
				ID:                 req.Header.ID,
				Response:           true,
				RecursionAvailable: true,
			},
			Questions: req.Questions,
		}

		question := req.Questions[0]
		name := question.Name.String()
		switch question.Type {
		case dnsmessage.TypeA:
			if ip, ok := s.recordsA[name]; ok {
				resp.Answers = append(resp.Answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{
						Name:  question.Name,
						Type:  dnsmessage.TypeA,
						Class: dnsmessage.ClassINET,
						TTL:   60,
					},
					Body: &dnsmessage.AResource{A: ip},
				})
			}
		case dnsmessage.TypeCNAME:
			if target, ok := s.cnames[name]; ok {
				cname, err := dnsmessage.NewName(target)
				if err == nil {
					resp.Answers = append(resp.Answers, dnsmessage.Resource{
						Header: dnsmessage.ResourceHeader{
							Name:  question.Name,
							Type:  dnsmessage.TypeCNAME,
							Class: dnsmessage.ClassINET,
							TTL:   60,
						},
						Body: &dnsmessage.CNAMEResource{CNAME: cname},
					})
				}
			}
		}

		data, err := resp.Pack()
		if err != nil {
			continue
		}
		_, _ = s.conn.WriteTo(data, addr)
	}
}
