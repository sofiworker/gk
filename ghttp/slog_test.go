package ghttp

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogLoggerAdapter(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, nil))
	logger := NewSlogLogger(base)
	logger.InfoContext(context.Background(), "hello", "key", "value")

	if !strings.Contains(buf.String(), "hello") || !strings.Contains(buf.String(), "value") {
		t.Fatalf("log output = %q", buf.String())
	}
}

func TestNewSlogLoggerNil(t *testing.T) {
	if NewSlogLogger(nil) != nil {
		t.Fatal("NewSlogLogger(nil) should return nil")
	}
}
