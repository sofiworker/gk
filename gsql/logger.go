package gsql

import "log"

type Logger interface {
	Debugf(format string, v ...interface{})
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

// stdLogger 是默认的日志实现，避免 gsql 核心依赖具体日志库（如 glog）。
type stdLogger struct{}

func (stdLogger) Debugf(format string, v ...interface{}) { log.Printf(format, v...) }
func (stdLogger) Infof(format string, v ...interface{})  { log.Printf(format, v...) }
func (stdLogger) Warnf(format string, v ...interface{})  { log.Printf(format, v...) }
func (stdLogger) Errorf(format string, v ...interface{}) { log.Printf(format, v...) }

var defaultSQLLogger Logger = stdLogger{}
