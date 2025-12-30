package glog_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/glog"
)

// --- Helpers ---

// parseJSONLog is a helper to parse a single JSON log line.
func parseJSONLog(t *testing.T, logLine string) map[string]interface{} {
	t.Helper()
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(logLine), &data); err != nil {
		t.Fatalf("Failed to parse JSON log line: %q, error: %v", logLine, err)
	}
	return data
}

func parseMessageField(data map[string]interface{}) (string, bool) {
	if value, ok := data["msg"]; ok {
		return fmt.Sprint(value), true
	}
	if value, ok := data["message"]; ok {
		return fmt.Sprint(value), true
	}
	return "", false
}

func readLogContent(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("Failed to read log file: %v", err)
	}
	return string(content)
}

func countNonEmptyLines(content string) int {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// createTempLogFile creates a temporary directory and a log file path for testing.
// It automatically registers a cleanup function to remove the directory after the test.
func createTempLogFile(t *testing.T) (string, string) {
	t.Helper()
	tempDir, err := ioutil.TempDir("", "glog-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	logFilePath := filepath.Join(tempDir, "test.log")
	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})
	return logFilePath, tempDir
}

func parseLastJSONLog(t *testing.T, content string) map[string]interface{} {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		return parseJSONLog(t, line)
	}
	t.Fatalf("No log lines found")
	return nil
}

// --- Tests ---

// TestDefaultLogger verifies the "out-of-the-box" behavior.
func TestDefaultLogger(t *testing.T) {
	// This test relies on the default init() behavior.
	// We can't easily isolate it, so we'll just observe its output.
	// For more controlled tests, we use Configure.
	// A simple call to ensure it doesn't panic.
	glog.Info("Default logger initialized")
	if glog.Default() == nil {
		t.Fatal("Default logger should not be nil")
	}
}

// TestConfigure tests the comprehensive configuration capabilities.
func TestConfigure(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)

	// Start with a clean slate for each sub-test
	baseConfig := []glog.Option{
		glog.WithOutputPaths(logFilePath),
		glog.WithEncoding(glog.JSONEncoding),
		glog.WithLevel(glog.InfoLevel),
		glog.WithDevelopment(false),
		glog.WithDisableCaller(false),
		glog.WithDisableStacktrace(false),
		glog.WithInitialFields(nil),
	}

	if err := glog.Configure(baseConfig...); err != nil {
		t.Fatalf("Failed to set up base config: %v", err)
	}

	t.Run("ChangeLevelAndEncoding", func(t *testing.T) {
		err := glog.Configure(
			glog.WithOutputPaths(logFilePath),
			glog.WithLevel(glog.DebugLevel),
			glog.WithEncoding(glog.ConsoleEncoding),
		)
		if err != nil {
			t.Fatalf("Failed to configure: %v", err)
		}

		glog.Debugf("Debug message: %s", "ok")
		glog.Sync()
		time.Sleep(100 * time.Millisecond)

		content, _ := ioutil.ReadFile(logFilePath)
		if !strings.Contains(string(content), "DEBUG") || !strings.Contains(string(content), "Debug message: ok") {
			t.Errorf("Expected debug console log, got: %s", string(content))
		}
	})

	t.Run("WithInitialFields", func(t *testing.T) {
		err := glog.Configure(
			glog.WithOutputPaths(logFilePath),
			glog.WithEncoding(glog.JSONEncoding),
			glog.WithInitialFields(map[string]interface{}{"service": "test-app"}),
		)
		if err != nil {
			t.Fatalf("Failed to configure: %v", err)
		}

		glog.Info("Log with initial fields")
		glog.Sync()
		time.Sleep(100 * time.Millisecond)

		content, _ := ioutil.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		if logData["service"] != "test-app" {
			t.Errorf("Initial field 'service' not present: %v", logData)
		}
	})

	t.Run("DisableCaller", func(t *testing.T) {
		err := glog.Configure(
			glog.WithOutputPaths(logFilePath),
			glog.WithEncoding(glog.JSONEncoding),
			glog.WithDisableCaller(true),
		)
		if err != nil {
			t.Fatalf("Failed to configure: %v", err)
		}

		glog.Info("Info without caller")
		glog.Sync()
		time.Sleep(100 * time.Millisecond)

		content, _ := ioutil.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		if _, ok := logData["caller"]; ok {
			t.Errorf("Caller field should not be present")
		}
	})

	t.Run("CustomEncoderKeys", func(t *testing.T) {
		err := glog.Configure(
			glog.WithOutputPaths(logFilePath),
			glog.WithEncoding(glog.JSONEncoding),
			glog.WithMessageKey("message"),
			glog.WithLevelKey("severity"),
		)
		if err != nil {
			t.Fatalf("Failed to configure: %v", err)
		}

		glog.Warn("Custom key test")
		glog.Sync()
		time.Sleep(100 * time.Millisecond)

		content, _ := ioutil.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		if _, ok := logData["message"]; !ok {
			t.Errorf("Expected message key 'message', but it was not found")
		}
		if _, ok := logData["severity"]; !ok {
			t.Errorf("Expected level key 'severity', but it was not found")
		}
	})
}

// TestLoggingMethods verifies the correctness of different logging styles.
func TestLoggingMethods(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)
	err := glog.Configure(
		glog.WithOutputPaths(logFilePath),
		glog.WithLevel(glog.DebugLevel),
		glog.WithEncoding(glog.JSONEncoding),
		glog.WithDisableCaller(true),
	)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	t.Run("StructuredLog", func(t *testing.T) {
		glog.Info("User logged in", "user_id", 123, "ip", "192.168.1.1")
		glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := ioutil.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		msg, ok := parseMessageField(logData)
		if !ok || msg != "User logged in" || logData["user_id"] != float64(123) {
			t.Errorf("Structured log failed: %v", logData)
		}
	})

	t.Run("FormattedLog", func(t *testing.T) {
		glog.Warnf("Failed to connect to %s, attempt %d", "db", 3)
		glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := ioutil.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		msg, ok := parseMessageField(logData)
		if !ok || msg != "Failed to connect to db, attempt 3" {
			t.Errorf("Formatted log failed: %v", logData)
		}
	})

	t.Run("WithLogger", func(t *testing.T) {
		subLogger := glog.With("request_id", "abc-123")
		subLogger.Info("Request started", "method", "GET")
		glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := ioutil.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		if logData["request_id"] != "abc-123" || logData["method"] != "GET" {
			t.Errorf("WithLogger failed: %v", logData)
		}
	})
}

// TestErrorHandling verifies that invalid user input is handled gracefully.
func TestErrorHandling(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)
	err := glog.Configure(
		glog.WithOutputPaths(logFilePath),
		glog.WithLevel(glog.DebugLevel),
		glog.WithEncoding(glog.JSONEncoding),
	)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	t.Run("InvalidKeyValuePairs", func(t *testing.T) {
		glog.Warn("Invalid args", "key1", "value1", "key2") // Odd number
		glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := ioutil.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))

		if errVal, ok := logData["error"]; !ok || !strings.Contains(fmt.Sprint(errVal), "invalid number of arguments") {
			t.Errorf("Expected error field with ErrInvalidKeyValuePairs, got: %v", logData)
		}
	})

	t.Run("KeyNotString", func(t *testing.T) {
		glog.Error("Invalid key type", 123, "value") // Key is not a string
		glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := ioutil.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))

		if errVal, ok := logData["error"]; !ok || !strings.Contains(fmt.Sprint(errVal), "log field key must be a string") {
			t.Errorf("Expected error field with ErrKeyNotString, got: %v", logData)
		}
	})
}

// TestSetLevel verifies dynamic level changes.
func TestSetLevel(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)
	err := glog.Configure(
		glog.WithOutputPaths(logFilePath),
		glog.WithLevel(glog.InfoLevel),
	)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	glog.Debug("This debug message should not appear")
	glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content, _ := ioutil.ReadFile(logFilePath)
	if string(content) != "" {
		t.Fatalf("Log file should be empty, but got: %s", content)
	}

	glog.SetLevel(glog.DebugLevel)
	glog.Debug("This debug message should appear now")
	glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content, _ = ioutil.ReadFile(logFilePath)
	if !strings.Contains(string(content), "This debug message should appear now") {
		t.Errorf("Debug message not found after level change: %s", content)
	}
}

func TestSetLevelFiltering(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)
	err := glog.Configure(
		glog.WithOutputPaths(logFilePath),
		glog.WithLevel(glog.InfoLevel),
		glog.WithEncoding(glog.JSONEncoding),
		glog.WithDisableCaller(true),
		glog.WithDisableStacktrace(true),
	)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	glog.Debug("debug should be dropped")
	glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content := readLogContent(t, logFilePath)
	if strings.TrimSpace(content) != "" {
		t.Fatalf("expected no logs at info level, got: %s", content)
	}

	glog.SetLevel(glog.DebugLevel)
	glog.Debug("debug should be kept")
	glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content = readLogContent(t, logFilePath)
	logData := parseLastJSONLog(t, content)
	msg, ok := parseMessageField(logData)
	if !ok || msg != "debug should be kept" {
		t.Fatalf("unexpected log message: %v", logData)
	}

	glog.SetLevel(glog.ErrorLevel)
	glog.Info("info should be dropped")
	glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content = readLogContent(t, logFilePath)
	if countNonEmptyLines(content) != 1 {
		t.Fatalf("expected no new log lines after error level, got: %s", content)
	}
}

// TestConcurrency ensures thread safety during concurrent logging and reconfiguration.
func TestConcurrency(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)
	err := glog.Configure(
		glog.WithOutputPaths(logFilePath),
		glog.WithLevel(glog.DebugLevel),
		glog.WithEncoding(glog.JSONEncoding),
	)
	if err != nil {
		t.Fatalf("Failed to configure: %v", err)
	}

	var wg sync.WaitGroup
	numGoroutines := 50
	numLogsPerGoroutine := 50
	errCh := make(chan error, 10)

	// Logging goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numLogsPerGoroutine; j++ {
				glog.Warnf("Goroutine %d, log %d", id, j)
			}
		}(i)
	}

	// Reconfiguring goroutines
	for i := 0; i < 10; i++ {
		level := glog.Level(i % 2)
		wg.Add(1)
		go func(lvl glog.Level) {
			defer wg.Done()
			if err := glog.Configure(glog.WithInitialFields(map[string]interface{}{"rand": time.Now().UnixNano()})); err != nil {
				errCh <- err
				return
			}
			_ = lvl
		}(level)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("Failed to configure: %v", err)
	}
	glog.Sync()
	time.Sleep(200 * time.Millisecond)

	content, err := ioutil.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}
	logLines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// The main goal is to ensure no race conditions occurred (test won't fail with -race flag)
	// and the output contains valid JSON entries under concurrent reconfiguration.
	if len(logLines) == 0 {
		t.Errorf("Expected some log lines, but file is empty")
	}

	validJSONLines := 0
	for _, line := range logLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(line), &payload); err == nil {
			validJSONLines++
		}
	}
	if validJSONLines == 0 {
		t.Errorf("Expected at least one valid JSON log line, got none")
	}
}
