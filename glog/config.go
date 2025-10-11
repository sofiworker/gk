package glog

import (
	"io"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

// buildWriters 根据配置构建writers
func buildWriters(config *Config) ([]io.Writer, error) {
	var writers []io.Writer

	for _, outputPath := range config.OutputPaths {
		var writer io.Writer
		switch outputPath {
		case StdoutOutput:
			writer = os.Stdout
		case StderrOutput:
			writer = os.Stderr
		case "":
			writer = os.Stdout
		default:
			if config.RotationConfig != nil {
				writer = &lumberjack.Logger{
					Filename:   string(outputPath),
					MaxSize:    config.RotationConfig.MaxSize,
					MaxAge:     config.RotationConfig.MaxAge,
					MaxBackups: config.RotationConfig.MaxBackups,
					LocalTime:  config.RotationConfig.LocalTime,
					Compress:   config.RotationConfig.Compress,
				}
			} else {
				file, err := os.OpenFile(string(outputPath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					return nil, err
				}
				writer = file
			}
		}
		writers = append(writers, writer)
	}

	if len(writers) == 0 {
		writers = []io.Writer{os.Stdout}
	}

	return writers, nil
}
