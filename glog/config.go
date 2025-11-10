package glog

import (
	"io"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

// RotationConfig 定义了日志轮转的配置。
type RotationConfig struct {
	MaxSize    int // MB
	MaxAge     int // days
	MaxBackups int
	LocalTime  bool
	Compress   bool
}

// EncoderConfig 定义了结构化日志中各个字段的键名。
type EncoderConfig struct {
	MessageKey    string `json:"message_key"`
	LevelKey      string `json:"level_key"`
	TimeKey       string `json:"time_key"`
	CallerKey     string `json:"caller_key"`
	StacktraceKey string `json:"stacktrace_key"`
}

// Config 是一个通用的日志配置结构体。
type Config struct {
	Level             Level
	Encoding          Encoding
	InitialFields     map[string]interface{}
	EnableStdout      bool
	FilePaths         []string
	EncoderConfig     *EncoderConfig
	RotationConfig    *RotationConfig
	DisableCaller     bool
	DisableStacktrace bool
	Development       bool
	TimeFormat        string
}

// DefaultConfig 返回一个被完全初始化的、适合生产环境的默认日志配置。
// 默认启用文件输出和日志轮转。
func DefaultConfig() *Config {
	return &Config{
		Level:             InfoLevel,
		Encoding:          ConsoleEncoding,
		EnableStdout:      true,
		FilePaths:         nil,
		Development:       false,
		DisableCaller:     false,
		DisableStacktrace: false,
		InitialFields:     make(map[string]interface{}),
		TimeFormat:        "2006-01-02 15:04:05.000",
		RotationConfig: &RotationConfig{
			MaxSize:    100,
			MaxAge:     30,
			MaxBackups: 7,
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
}

// buildWriters 根据配置构建 io.Writer。
func buildWriters(config *Config) ([]io.Writer, error) {
	writers := make([]io.Writer, 0)

	if config.EnableStdout {
		writers = append(writers, os.Stdout)
	}

	// 如果没有文件输出且没有标准输出，默认使用 os.Stdout
	if len(config.FilePaths) == 0 && !config.EnableStdout {
		writers = append(writers, os.Stdout)
	}

	// 如果启用了文件日志但没有配置轮转，则提供一个默认的轮转配置
	rotationConfig := config.RotationConfig
	if len(config.FilePaths) > 0 && rotationConfig == nil {
		rotationConfig = &RotationConfig{
			MaxSize:    100, // 100 MB
			MaxAge:     30,  // 30 days
			MaxBackups: 7,
			Compress:   true,
			LocalTime:  true,
		}
	}

	for _, path := range config.FilePaths {
		var writer io.Writer
		if rotationConfig != nil {
			writer = &lumberjack.Logger{
				Filename:   path,
				MaxSize:    rotationConfig.MaxSize,
				MaxAge:     rotationConfig.MaxAge,
				MaxBackups: rotationConfig.MaxBackups,
				LocalTime:  rotationConfig.LocalTime,
				Compress:   rotationConfig.Compress,
			}
		} else {
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				return nil, err
			}
			writer = file
		}
		writers = append(writers, writer)
	}

	return writers, nil
}
