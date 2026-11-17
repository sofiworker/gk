package glog

import "errors"

var (
	// ErrInvalidKeyValuePairs 表示为结构化日志提供了奇数个参数，
	// 而期望的是偶数个（键值对）。
	ErrInvalidKeyValuePairs = errors.New("invalid number of arguments for structured log, key-value pairs expected")

	// ErrKeyNotString 表示为结构化日志提供的键不是字符串类型。
	ErrKeyNotString = errors.New("log field key must be a string")
)
