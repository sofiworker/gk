package glog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gopkg.in/natefinch/lumberjack.v2"
)

func TestBuildWritersDefaultStdout(t *testing.T) {
	t.Parallel()

	config := &Config{
		EnableStdout: false,
		FilePaths:    nil,
	}

	writers, err := buildWriters(config)
	if err != nil {
		t.Fatalf("buildWriters failed: %v", err)
	}
	if len(writers) != 1 {
		t.Fatalf("expected 1 writer, got %d", len(writers))
	}
	if writers[0] != os.Stdout {
		t.Fatalf("expected os.Stdout writer")
	}
}

func TestBuildWritersRotation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "rotation.log")

	config := &Config{
		EnableStdout:   false,
		FilePaths:      []string{logPath},
		RotationConfig: &RotationConfig{MaxSize: 1, MaxAge: 1, MaxBackups: 1, Compress: true, LocalTime: true},
	}

	writers, err := buildWriters(config)
	if err != nil {
		t.Fatalf("buildWriters failed: %v", err)
	}
	if len(writers) != 1 {
		t.Fatalf("expected 1 writer, got %d", len(writers))
	}
	writer, ok := writers[0].(*lumberjack.Logger)
	if !ok {
		t.Fatalf("expected lumberjack.Logger writer")
	}
	if writer.Filename != logPath {
		t.Fatalf("expected log path %q, got %q", logPath, writer.Filename)
	}
}

func TestBuildWritersDefaultRotation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "default-rotation.log")

	config := &Config{
		EnableStdout:   false,
		FilePaths:      []string{logPath},
		RotationConfig: nil,
	}

	writers, err := buildWriters(config)
	if err != nil {
		t.Fatalf("buildWriters failed: %v", err)
	}
	if len(writers) != 1 {
		t.Fatalf("expected 1 writer, got %d", len(writers))
	}
	writer, ok := writers[0].(*lumberjack.Logger)
	if !ok {
		t.Fatalf("expected lumberjack.Logger writer")
	}
	if writer.MaxSize != 100 || writer.MaxAge != 30 || writer.MaxBackups != 7 || !writer.Compress || !writer.LocalTime {
		t.Fatalf("unexpected default rotation config: %+v", writer)
	}
}

func TestBuildEncoderJSONCustomKeysAndTimeFormat(t *testing.T) {
	t.Parallel()

	config := &Config{
		Encoding: JSONEncoding,
		EncoderConfig: &EncoderConfig{
			MessageKey:    "message",
			LevelKey:      "level",
			TimeKey:       "time",
			CallerKey:     "caller",
			StacktraceKey: "stack",
		},
		TimeFormat: "2006-01-02 15:04:05",
	}

	encoder := buildEncoder(config)
	entryTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	entry := zapcore.Entry{
		Time:    entryTime,
		Level:   zapcore.InfoLevel,
		Message: "hello",
	}

	buf, err := encoder.EncodeEntry(entry, nil)
	if err != nil {
		t.Fatalf("EncodeEntry failed: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if data["message"] != "hello" {
		t.Fatalf("expected message key, got %v", data["message"])
	}
	if data["level"] != "info" {
		t.Fatalf("expected level key, got %v", data["level"])
	}
	if data["time"] != entryTime.Format(config.TimeFormat) {
		t.Fatalf("expected time format %q, got %v", config.TimeFormat, data["time"])
	}
}

func TestBuildEncoderConsole(t *testing.T) {
	t.Parallel()

	config := &Config{
		Encoding: ConsoleEncoding,
		EncoderConfig: &EncoderConfig{
			MessageKey: "msg",
			LevelKey:   "lvl",
			TimeKey:    "ts",
		},
	}

	encoder := buildEncoder(config)
	entry := zapcore.Entry{
		Time:    time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		Level:   zapcore.InfoLevel,
		Message: "hello-console",
	}

	buf, err := encoder.EncodeEntry(entry, nil)
	if err != nil {
		t.Fatalf("EncodeEntry failed: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "{\"msg\"") {
		t.Fatalf("expected console format, got JSON output: %s", out)
	}
	if !strings.Contains(out, "hello-console") {
		t.Fatalf("expected console output to contain message, got %s", out)
	}
}

func TestNewZapLoggerConfigCopy(t *testing.T) {
	t.Parallel()

	config := &Config{
		Level:         InfoLevel,
		Encoding:      JSONEncoding,
		EnableStdout:  true,
		TimeFormat:    "2006-01-02",
		InitialFields: map[string]interface{}{"service": "origin"},
		RotationConfig: &RotationConfig{
			MaxSize:    10,
			MaxAge:     2,
			MaxBackups: 3,
			Compress:   true,
			LocalTime:  true,
		},
		EncoderConfig: &EncoderConfig{
			MessageKey:    "msg",
			LevelKey:      "lvl",
			TimeKey:       "ts",
			CallerKey:     "caller",
			StacktraceKey: "stack",
		},
	}

	logger, err := newZapLogger(config)
	if err != nil {
		t.Fatalf("newZapLogger failed: %v", err)
	}

	config.Level = DebugLevel
	config.TimeFormat = "2006-01-02 15:04:05"
	config.InitialFields["service"] = "mutated"
	config.RotationConfig.MaxSize = 99

	snapshot := logger.Config()
	if snapshot.Level != InfoLevel {
		t.Fatalf("expected level to remain InfoLevel, got %v", snapshot.Level)
	}
	if snapshot.TimeFormat != "2006-01-02" {
		t.Fatalf("expected time format to remain original, got %v", snapshot.TimeFormat)
	}
	if snapshot.InitialFields["service"] != "origin" {
		t.Fatalf("expected initial fields to be copied, got %v", snapshot.InitialFields["service"])
	}
	if snapshot.RotationConfig.MaxSize != 10 {
		t.Fatalf("expected rotation config to be copied, got %v", snapshot.RotationConfig.MaxSize)
	}
}

func TestBuildOptionsInitialFields(t *testing.T) {
	t.Parallel()

	config := &Config{
		InitialFields: map[string]interface{}{
			"service": "unit-test",
		},
	}

	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core, buildOptions(config)...)
	logger.Info("hello")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["service"] != "unit-test" {
		t.Fatalf("expected initial field service, got %v", fields["service"])
	}
}

func TestBuildOptionsCallerAndStacktrace(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(zapcore.DebugLevel)
	config := &Config{
		Development:       true,
		DisableCaller:     false,
		DisableStacktrace: false,
	}
	logger := zap.New(core, buildOptions(config)...)
	logger.Warn("warn with stack")

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if !entries[0].Caller.Defined {
		t.Fatalf("expected caller to be defined")
	}
	if entries[0].Stack == "" {
		t.Fatalf("expected stacktrace to be present on warn in development mode")
	}

	logs.TakeAll()
	config.DisableStacktrace = true
	logger = zap.New(core, buildOptions(config)...)
	logger.Error("error without stack")

	entries = logs.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if entries[0].Stack != "" {
		t.Fatalf("expected no stacktrace when disabled")
	}
}

func TestTraceContextFields(t *testing.T) {
	t.Parallel()

	logger := &zapLogger{
		l:           zap.NewNop(),
		atomicLevel: zap.NewAtomicLevel(),
		config:      DefaultConfig(),
	}

	emptyFields := logger.traceContextFields(context.Background())
	if len(emptyFields) != 0 {
		t.Fatalf("expected no fields for empty context")
	}

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("failed to build trace ID: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("failed to build span ID: %v", err)
	}
	validSpanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	ctxValid := trace.ContextWithSpanContext(context.Background(), validSpanCtx)
	validFields := logger.traceContextFields(ctxValid)
	if len(validFields) != 0 {
		t.Fatalf("expected no fields for non-recording span, got %d", len(validFields))
	}
}

func TestArgsToZapFieldsErrors(t *testing.T) {
	t.Parallel()

	logger := &zapLogger{
		l:           zap.NewNop(),
		atomicLevel: zap.NewAtomicLevel(),
		config:      DefaultConfig(),
	}

	if _, err := logger.argsToZapFields("only-key"); !errors.Is(err, ErrInvalidKeyValuePairs) {
		t.Fatalf("expected ErrInvalidKeyValuePairs, got %v", err)
	}
	if _, err := logger.argsToZapFields(123, "value"); !errors.Is(err, ErrKeyNotString) {
		t.Fatalf("expected ErrKeyNotString, got %v", err)
	}
}

func TestArgsToZapFieldsSuccess(t *testing.T) {
	t.Parallel()

	logger := &zapLogger{
		l:           zap.NewNop(),
		atomicLevel: zap.NewAtomicLevel(),
		config:      DefaultConfig(),
	}

	fields, err := logger.argsToZapFields("id", 123, "name", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[0].Key != "id" || fields[1].Key != "name" {
		t.Fatalf("unexpected field keys: %v, %v", fields[0].Key, fields[1].Key)
	}
}
