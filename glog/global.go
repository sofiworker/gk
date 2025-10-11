package glog

import "sync"

// GlobalLogger 全局默认logger实例
var (
	global   Logger
	globalMu sync.RWMutex
)

// GetLogger 获取全局logger实例
func GetLogger() Logger {
	globalMu.RLock()
	if global != nil {
		defer globalMu.RUnlock()
		return global
	}
	globalMu.RUnlock()

	// 双重检查锁模式
	globalMu.Lock()
	defer globalMu.Unlock()
	if global == nil {
		cfg := &Config{
			Level:       InfoLevel,
			Encoding:    ConsoleEncoding,
			CallerSkip:  1,
			OutputPaths: []OutputType{StdoutOutput},
		}
		logger, err := NewLogger(cfg)
		if err != nil {
			panic(err)
		}
		global = logger
	}
	return global
}

// SetLogger 设置全局logger实例
func SetLogger(logger Logger) {
	globalMu.Lock()
	defer globalMu.Unlock()
	global = logger
}
