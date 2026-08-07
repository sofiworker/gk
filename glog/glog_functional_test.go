package glog_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sofiworker/gk/glog"
)

func TestContextLoggingTraceFields(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)
	err := glog.Configure(
		glog.WithOutputPaths(logFilePath),
		glog.WithEncoding(glog.JSONEncoding),
		glog.WithDisableCaller(true),
		glog.WithTraceExtractor(func(context.Context) (string, string) {
			return "0102030405060708090a0b0c0d0e0f10", "0102030405060708"
		}),
	)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	glog.InfoContext(context.Background(), "with trace")
	time.Sleep(100 * time.Millisecond)

	content, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(content))), &data); err != nil {
		t.Fatalf("Failed to parse JSON log: %v", err)
	}
	if data["trace_id"] != "0102030405060708090a0b0c0d0e0f10" {
		t.Fatalf("expected trace_id, got %v", data["trace_id"])
	}
	if data["span_id"] != "0102030405060708" {
		t.Fatalf("expected span_id, got %v", data["span_id"])
	}
}
