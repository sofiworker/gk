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

// WithFilterInstructions 设置预编译的 BPF 指令（可读性更佳）。
func WithFilterInstructions(instructions []bpf.Instruction) func(*Config) {
	return func(c *Config) {
		c.Filter.Instructions = append(c.Filter.Instructions, instructions...)
	}
}

// WithExpr 用 tcpdump 表达式设置过滤器（如 "tcp port 80"），
// New 时编译为 BPF 指令。仅支持 EN10MB 链路类型。
//
// WithExpr sets the filter from a tcpdump expression (e.g. "tcp port 80"),
// compiled into BPF instructions by New. Only EN10MB link types are
// supported.
func WithExpr(expr string) func(*Config) {
	return func(c *Config) { c.expr = expr }
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
