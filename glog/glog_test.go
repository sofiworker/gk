package glog_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sofiworker/gk/glog"
)

// --- Helpers ---

// parseJSONLog 解析单行 JSON 日志的辅助函数。
// parseJSONLog parses a single JSON log line.
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

// createTempLogFile 创建测试用的临时目录与日志文件路径，并注册清理函数。
// createTempLogFile creates a temp dir/path and registers cleanup.
func createTempLogFile(t *testing.T) (string, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "glog-test-*")
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

// TestDefaultLogger 验证开箱即用的默认行为。
// TestDefaultLogger verifies the out-of-the-box behavior.
func TestDefaultLogger(t *testing.T) {
	// 依赖默认 init() 行为，无法隔离，只观察输出并确保不 panic。
	// Relies on default init(); just observe output and ensure no panic.
	glog.Info("Default logger initialized")
	if glog.Default() == nil {
		t.Fatal("Default logger should not be nil")
	}
}

// TestConfigure 验证完整配置能力。
// TestConfigure verifies the comprehensive configuration capabilities.
func TestConfigure(t *testing.T) {
	logFilePath, _ := createTempLogFile(t)

	// 每个子测试从干净状态开始；start with a clean slate per sub-test.
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
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)

		content, _ := os.ReadFile(logFilePath)
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
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)

		content, _ := os.ReadFile(logFilePath)
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
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)

		content, _ := os.ReadFile(logFilePath)
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
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)

		content, _ := os.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		if _, ok := logData["message"]; !ok {
			t.Errorf("Expected message key 'message', but it was not found")
		}
		if _, ok := logData["severity"]; !ok {
			t.Errorf("Expected level key 'severity', but it was not found")
		}
	})
}

// TestLoggingMethods 验证不同日志写法的正确性。
// TestLoggingMethods verifies different logging styles.
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
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := os.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		msg, ok := parseMessageField(logData)
		if !ok || msg != "User logged in" || logData["user_id"] != float64(123) {
			t.Errorf("Structured log failed: %v", logData)
		}
	})

	t.Run("FormattedLog", func(t *testing.T) {
		glog.Warnf("Failed to connect to %s, attempt %d", "db", 3)
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := os.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		msg, ok := parseMessageField(logData)
		if !ok || msg != "Failed to connect to db, attempt 3" {
			t.Errorf("Formatted log failed: %v", logData)
		}
	})

	t.Run("WithLogger", func(t *testing.T) {
		subLogger := glog.With("request_id", "abc-123")
		subLogger.Info("Request started", "method", "GET")
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := os.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))
		if logData["request_id"] != "abc-123" || logData["method"] != "GET" {
			t.Errorf("WithLogger failed: %v", logData)
		}
	})
}

// TestErrorHandling 验证非法用户输入被妥善处理。
// TestErrorHandling verifies invalid user input is handled gracefully.
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
		glog.Warn("Invalid args", "key1", "value1", "key2") // 奇数个参数；odd number of args.
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := os.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))

		if errVal, ok := logData["error"]; !ok || !strings.Contains(fmt.Sprint(errVal), "invalid number of arguments") {
			t.Errorf("Expected error field with ErrInvalidKeyValuePairs, got: %v", logData)
		}
	})

	t.Run("KeyNotString", func(t *testing.T) {
		glog.Error("Invalid key type", 123, "value") // key 不是字符串；key is not a string.
		_ = glog.Sync()
		time.Sleep(100 * time.Millisecond)
		content, _ := os.ReadFile(logFilePath)
		logData := parseLastJSONLog(t, string(content))

		if errVal, ok := logData["error"]; !ok || !strings.Contains(fmt.Sprint(errVal), "log field key must be a string") {
			t.Errorf("Expected error field with ErrKeyNotString, got: %v", logData)
		}
	})
}

// TestSetLevel 验证动态级别切换。
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
	_ = glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content, _ := os.ReadFile(logFilePath)
	if string(content) != "" {
		t.Fatalf("Log file should be empty, but got: %s", content)
	}

	glog.SetLevel(glog.DebugLevel)
	glog.Debug("This debug message should appear now")
	_ = glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content, _ = os.ReadFile(logFilePath)
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
	_ = glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content := readLogContent(t, logFilePath)
	if strings.TrimSpace(content) != "" {
		t.Fatalf("expected no logs at info level, got: %s", content)
	}

	glog.SetLevel(glog.DebugLevel)
	glog.Debug("debug should be kept")
	_ = glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content = readLogContent(t, logFilePath)
	logData := parseLastJSONLog(t, content)
	msg, ok := parseMessageField(logData)
	if !ok || msg != "debug should be kept" {
		t.Fatalf("unexpected log message: %v", logData)
	}

	glog.SetLevel(glog.ErrorLevel)
	glog.Info("info should be dropped")
	_ = glog.Sync()
	time.Sleep(100 * time.Millisecond)
	content = readLogContent(t, logFilePath)
	if countNonEmptyLines(content) != 1 {
		t.Fatalf("expected no new log lines after error level, got: %s", content)
	}
}

// TestConcurrency 验证并发日志与重配置的线程安全。
// TestConcurrency ensures thread safety under concurrent logging and reconfiguration.
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

	// 日志 goroutine；logging goroutines.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numLogsPerGoroutine; j++ {
				glog.Warnf("Goroutine %d, log %d", id, j)
			}
		}(i)
	}

	// 重配置 goroutine；reconfiguring goroutines.
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
	_ = glog.Sync()
	time.Sleep(200 * time.Millisecond)

	content, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}
	logLines := strings.Split(strings.TrimSpace(string(content)), "\n")

	// 主要目标是确保无数据竞争（配合 -race）且并发重配置下输出仍是合法 JSON。
	// Goal: no data races and valid JSON output under concurrent reconfiguration.
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
