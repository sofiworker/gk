package gresolver

import "fmt"

var (
	ErrInvalidDomainName = fmt.Errorf("invalid domain name")
	ErrNoIPFound         = fmt.Errorf("no IP addresses found")
	ErrIDMismatch        = fmt.Errorf("DNS message ID mismatch")
	ErrNoResponse        = fmt.Errorf("no DNS servers responded")
)

type DNSError struct {
	Op     string
	Name   string
	Server string
	Err    error
}

func (e *DNSError) Error() string {
	if e.Server != "" {
		return fmt.Sprintf("dns %s %s on server %s: %v", e.Op, e.Name, e.Server, e.Err)
	}
	return fmt.Sprintf("dns %s %s: %v", e.Op, e.Name, e.Err)
}

func (e *DNSError) Unwrap() error {
	return e.Err
}
