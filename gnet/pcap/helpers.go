package pcap

import (
	"fmt"
	"os"
)

// NewFileWriter 创建写入指定文件的 Writer，返回关闭函数便于调用方统一回收。
func NewFileWriter(path string, opts ...WriterOption) (*Writer, func() error, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("pcap: create %s: %w", path, err)
	}
	w, err := NewWriter(f, opts...)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return w, f.Close, nil
}
