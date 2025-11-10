package glog

import (
	"context"
	"sync"
)

var (
	global GLogger
	mu     sync.RWMutex
)

func init() {
	logger, err := newZapLogger(DefaultConfig())
	if err != nil {
		panic("glog: failed to initialize global logger: " + err.Error())
	}
	global = logger
}

// Configure 使用函数式选项来原子性地重新配置全局日志记录器。
func Configure(opts ...Option) error {
	mu.Lock()
	defer mu.Unlock()

	// 从当前的全局 logger 实例中获取配置
	currentConfig := global.Config()
	if currentConfig == nil {
		// 如果当前 logger 没有配置（例如，它是一个测试用的 mock），则从默认值开始
		currentConfig = DefaultConfig()
	}

	// --- 创建配置的深层副本 ---
	newCfg := *currentConfig
	if currentConfig.RotationConfig != nil {
		rotationCopy := *currentConfig.RotationConfig
		newCfg.RotationConfig = &rotationCopy
	}
	if currentConfig.InitialFields != nil {
		fieldsCopy := make(map[string]interface{})
		for k, v := range currentConfig.InitialFields {
			fieldsCopy[k] = v
		}
		newCfg.InitialFields = fieldsCopy
	}
	// --- 副本创建结束 ---

	// 一次性应用所有选项
	for _, opt := range opts {
		opt(&newCfg)
	}

	newLogger, err := New(&newCfg)
	if err != nil {
		return err
	}

	global = newLogger // 原子性地替换全局实例
	return nil
}

// Default 返回立即可用的默认全局日志记录器。
func Default() GLogger {
	mu.RLock()
	defer mu.RUnlock()
	return global
}

// New 根据提供的配置创建一个新的 GLogger 实例。
func New(c *Config) (GLogger, error) {
	return newZapLogger(c)
}

// SetLevel 动态地改变全局日志记录器的级别（高性能）。
func SetLevel(level Level) {
	Default().SetLevel(level)
}

func With(args ...interface{}) GLogger            { return Default().With(args...) }
func Debug(msg string, args ...interface{})       { Default().Debug(msg, args...) }
func Info(msg string, args ...interface{})        { Default().Info(msg, args...) }
func Warn(msg string, args ...interface{})        { Default().Warn(msg, args...) }
func Error(msg string, args ...interface{})       { Default().Error(msg, args...) }
func Fatal(msg string, args ...interface{})       { Default().Fatal(msg, args...) }
func Debugf(template string, args ...interface{}) { Default().Debugf(template, args...) }
func Infof(template string, args ...interface{})  { Default().Infof(template, args...) }
func Warnf(template string, args ...interface{})  { Default().Warnf(template, args...) }
func Errorf(template string, args ...interface{}) { Default().Errorf(template, args...) }
func DebugContext(ctx context.Context, msg string, args ...interface{}) {
	Default().DebugContext(ctx, msg, args...)
}
func InfoContext(ctx context.Context, msg string, args ...interface{}) {
	Default().InfoContext(ctx, msg, args...)
}
func WarnContext(ctx context.Context, msg string, args ...interface{}) {
	Default().WarnContext(ctx, msg, args...)
}
func ErrorContext(ctx context.Context, msg string, args ...interface{}) {
	Default().ErrorContext(ctx, msg, args...)
}
func Sync() error { return Default().Sync() }
