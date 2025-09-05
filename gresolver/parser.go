package gresolver

import (
	"bufio"
	"bytes"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	dnsPort    = 53
	udpMaxSize = 512
)

var DefaultNS = []string{"127.0.0.1:53", "[::1]:53"}

type DnsConfig struct {
	Nameservers []string
	Search      []string
	Domain      string
	Options     []string
	Ndots       int
	Timeout     time.Duration
	Attempts    int
}

func ParseResolveFile(file string) (*DnsConfig, error) {
	bs, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	conf := &DnsConfig{
		Ndots:    1,
		Timeout:  time.Second * 5,
		Attempts: 2,
	}
	scanner := bufio.NewScanner(bytes.NewReader(bs))
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		line = strings.ToLower(line)
		if strings.HasPrefix(line, "#") || len(line) == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 0 {
			continue
		}

		directive := fields[0]
		args := fields[1:]
		switch directive {
		case "nameserver":
		case "domain":
		case "search":
		case "options":
			for _, opt := range args {
				switch {
				case strings.HasPrefix(opt, "ndots:"):
					if len(opt) > 6 {
						ndotsStr := opt[6:]
						if ndots, err := strconv.Atoi(ndotsStr); err == nil {
							conf.Ndots = ndots
						}
					}
				case strings.HasPrefix(opt, "timeout:"):
					if len(opt) > 8 {
						timeoutStr := opt[8:]
						if timeout, err := time.ParseDuration(timeoutStr); err == nil {
							conf.Timeout = timeout
						}
					}
				case strings.HasPrefix(opt, "attempts:"):
					if len(opt) > 9 {
						attemptsStr := opt[9:]
						if attempts, err := strconv.Atoi(attemptsStr); err == nil {
							conf.Attempts = attempts
						}
					}
				default:
					if strings.HasPrefix(opt, "edns0") {
						// EDNS0 options are not handled in this parser
						continue
					} else {
						conf.Options = append(conf.Options, opt)
					}
				}
			}
		}
	}
	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	return conf, nil
}

func (c *DnsConfig) Validate() {
	if len(c.Nameservers) == 0 {
		c.Nameservers = DefaultNS
	}
	if c.Timeout <= 0 {
		c.Timeout = 5 * time.Second
	}
	if c.Attempts <= 0 {
		c.Attempts = 2
	}
	if c.Ndots <= 0 {
		c.Ndots = 1
	}
}

type Option func(*DnsConfig)

func WithResolveFile(file string) Option {
	return func(c *DnsConfig) {
		conf, err := ParseResolveFile(file)
		if err != nil {
			panic(err)
		}
		conf.Validate()
		c.Nameservers = conf.Nameservers
		c.Search = conf.Search
		c.Domain = conf.Domain
		c.Options = conf.Options
		c.Ndots = conf.Ndots
		c.Timeout = conf.Timeout
		c.Attempts = conf.Attempts
	}
}

func WithNameservers(nameservers []string) Option {
	return func(c *DnsConfig) {
		c.Nameservers = nameservers
	}
}

func WithSearch(search []string) Option {
	return func(c *DnsConfig) {
		c.Search = search
	}
}

func WithDomain(domain string) Option {
	return func(c *DnsConfig) {
		c.Domain = domain
	}
}

func WithOptions(options []string) Option {
	return func(c *DnsConfig) {
		c.Options = options
	}
}

func WithNdots(ndots int) Option {
	return func(c *DnsConfig) {
		c.Ndots = ndots
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *DnsConfig) {
		c.Timeout = timeout
	}
}

func WithAttempts(attempts int) Option {
	return func(c *DnsConfig) {
		c.Attempts = attempts
	}
}
