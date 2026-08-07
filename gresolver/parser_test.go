package gresolver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseResolveFile(t *testing.T) {
	content := `
# Comment
nameserver 8.8.8.8
nameserver 8.8.4.4
search example.com
domain example.net
options ndots:2 timeout:2s attempts:3
`
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "resolv.conf")
	err := os.WriteFile(file, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	conf, err := ParseResolveFile(file)
	if err != nil {
		t.Fatalf("ParseResolveFile failed: %v", err)
	}

	if len(conf.Nameservers) != 2 || conf.Nameservers[0] != "8.8.8.8:53" || conf.Nameservers[1] != "8.8.4.4:53" {
		t.Errorf("nameservers = %v, want [8.8.8.8:53 8.8.4.4:53]", conf.Nameservers)
	}
	if len(conf.Search) != 1 || conf.Search[0] != "example.com" {
		t.Errorf("search = %v, want [example.com]", conf.Search)
	}
	if conf.Domain != "example.net" {
		t.Errorf("domain = %q, want example.net", conf.Domain)
	}

	// 验证 options 解析；verify options parsing.
	if conf.Ndots != 2 {
		t.Errorf("expected ndots 2, got %d", conf.Ndots)
	}
	if conf.Timeout != 2*time.Second {
		t.Errorf("expected timeout 2s, got %v", conf.Timeout)
	}
	if conf.Attempts != 3 {
		t.Errorf("expected attempts 3, got %d", conf.Attempts)
	}

	// 校验默认值；validate defaults.
	conf.Validate()
	if len(conf.Nameservers) == 0 {
		t.Fatal("Validate should assign default nameservers")
	}
}

func TestWithFunctions(t *testing.T) {
	c := &DnsConfig{}

	WithNameservers([]string{"1.1.1.1"})(c)
	if c.Nameservers[0] != "1.1.1.1" {
		t.Error("WithNameservers failed")
	}

	WithSearch([]string{"local"})(c)
	if c.Search[0] != "local" {
		t.Error("WithSearch failed")
	}

	WithDomain("local")(c)
	if c.Domain != "local" {
		t.Error("WithDomain failed")
	}

	WithOptions([]string{"opt"})(c)
	if c.Options[0] != "opt" {
		t.Error("WithOptions failed")
	}

	WithNdots(5)(c)
	if c.Ndots != 5 {
		t.Error("WithNdots failed")
	}

	WithTimeout(time.Minute)(c)
	if c.Timeout != time.Minute {
		t.Error("WithTimeout failed")
	}

	WithAttempts(5)(c)
	if c.Attempts != 5 {
		t.Error("WithAttempts failed")
	}
}
