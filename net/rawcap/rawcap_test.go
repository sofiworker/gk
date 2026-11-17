package rawcap

import (
	"testing"
)

func TestNormalizeConfig(t *testing.T) {
	cfg := normalizeConfig(Config{})
	if cfg.SnapLen != DefaultSnapLen {
		t.Fatalf("unexpected snaplen: %d", cfg.SnapLen)
	}
	if cfg.BufferSize != DefaultBufferSize {
		t.Fatalf("unexpected buffer size: %d", cfg.BufferSize)
	}
	if cfg.Timeout != 0 {
		t.Fatalf("unexpected timeout: %s", cfg.Timeout)
	}
}

func TestOpenLiveInvalidInterface(t *testing.T) {
	if _, err := OpenLive("invalid0", Config{}); err == nil {
		t.Fatalf("expected error for invalid interface")
	}
}
