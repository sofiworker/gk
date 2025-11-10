package glog

// Level 日志级别
type Level int8

const (
	DebugLevel Level = iota - 1
	InfoLevel
	WarnLevel
	ErrorLevel
	DPanicLevel
	PanicLevel
	FatalLevel
)

type Encoding string

const (
	JSONEncoding    Encoding = "json"
	ConsoleEncoding          = "console"
)

// OutputType 输出类型
type OutputType string

const (
	StdoutOutput OutputType = "stdout"
	StderrOutput OutputType = "stderr"
	FileOutput   OutputType = "file"
)
