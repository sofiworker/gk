package capture

import "testing"

// TestWithExprOption 覆盖 WithExpr 配置注入。
func TestWithExprOption(t *testing.T) {
	cfg := Config{}
	WithExpr("tcp port 80")(&cfg)
	if cfg.expr != "tcp port 80" {
		t.Fatalf("expr = %q", cfg.expr)
	}
}
