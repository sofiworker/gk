package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"time"

	"golang.org/x/net/bpf"
)

// CaptureOne 快速捕获单接口并输出到 pcapng 文件。
func CaptureOne(ctx context.Context, iface string, outPath string, opts ...func(*Config)) error {
	cfg := Config{
		Interfaces:  []string{iface},
		OutputPath:  outPath,
		Format:      FormatPCAPNG,
		Promiscuous: true,
		Timeout:     500 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		return err
	}
	return c.Run(ctx)
}

// WithSnapLen 设置 snaplen。
func WithSnapLen(snap int) func(*Config) {
	return func(c *Config) { c.SnapLen = snap }
}

// WithFilterRaw 直接设置原始 BPF。
func WithFilterRaw(raw []byte) func(*Config) {
	return func(c *Config) {
		c.Filter.Raw = append(c.Filter.Raw, bytesToRaw(raw)...)
	}
}

// WithWriter 指定自定义 writer。
func WithWriter(w io.Writer) func(*Config) {
	return func(c *Config) {
		c.Writer = w
		c.OutputPath = ""
	}
}

func bytesToRaw(b []byte) []bpf.RawInstruction {
	reader := bytes.NewReader(b)
	var ins []bpf.RawInstruction
	for {
		var r bpf.RawInstruction
		if err := binary.Read(reader, binary.BigEndian, &r); err != nil {
			break
		}
		ins = append(ins, r)
	}
	return ins
}
