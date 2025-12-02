package capture

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/sofiworker/gk/gnet/pcap"
	"github.com/sofiworker/gk/gnet/pcapng"
	"github.com/sofiworker/gk/gnet/rawcap"
	"golang.org/x/net/bpf"
)

type Format string

const (
	FormatPCAP   Format = "pcap"
	FormatPCAPNG Format = "pcapng"
)

// Config 捕获配置。
type Config struct {
	Interfaces  []string      // 要捕获的网卡列表，pcap 格式仅支持单网卡。
	SnapLen     int           // 截获长度
	BufferSize  int           // 套接字缓冲区
	Promiscuous bool          // 混杂模式
	Timeout     time.Duration // 读取超时
	Format      Format        // 输出格式：pcap 或 pcapng，默认 pcapng
	OutputPath  string        // 输出文件路径，不填则写入 Writer（为空则丢弃）
	Writer      io.Writer     // 自定义输出，优先级高于 OutputPath
	Filter      Filter        // BPF 过滤器配置

	LinkType uint32 // pcap 网络类型/pcapng 接口 link type，默认 1(ETHERNET)

	// Linux 性能选项
	TPacketV3 bool // 启用 PACKET_RX_RING
	BlockSize int  // TPacket block 大小
	NumBlocks int  // TPacket block 数量
	FrameSize int  // TPacket frame 大小
}

// Filter 支持预编译 BPF 过滤指令。
type Filter struct {
	Instructions []bpf.Instruction
	Raw          []bpf.RawInstruction
}

type Capture struct {
	cfg      Config
	sources  []source
	writer   packetWriter
	closeFns []func() error
}

type source struct {
	name     string
	index    int
	handle   rawcap.Handle
	writerID uint32
}

type packetWriter interface {
	WritePacket(src source, data []byte, ts time.Time) error
	Close() error
}

var (
	openLiveFn  = rawcap.OpenLive
	ifaceByName = net.InterfaceByName
)

// New 创建捕获器，未启动读取，需调用 Run。
func New(cfg Config) (*Capture, error) {
	cfg = normalizeConfig(cfg)

	if len(cfg.Interfaces) == 0 {
		return nil, fmt.Errorf("capture: at least one interface required")
	}
	if cfg.Format == FormatPCAP && len(cfg.Interfaces) > 1 {
		return nil, fmt.Errorf("capture: pcap format supports only one interface")
	}

	var (
		sources  []source
		closeFns []func() error
	)

	for _, name := range cfg.Interfaces {
		iface, err := ifaceByName(name)
		if err != nil {
			closeAll(closeFns)
			return nil, fmt.Errorf("capture: lookup interface %s: %w", name, err)
		}

		handle, err := openLiveFn(name, rawcap.Config{
			SnapLen:     cfg.SnapLen,
			Promiscuous: cfg.Promiscuous,
			Timeout:     cfg.Timeout,
			BufferSize:  cfg.BufferSize,
			TPacketV3:   cfg.TPacketV3,
			BlockSize:   cfg.BlockSize,
			NumBlocks:   cfg.NumBlocks,
			FrameSize:   cfg.FrameSize,
		})
		if err != nil {
			closeAll(closeFns)
			return nil, fmt.Errorf("capture: open %s: %w", name, err)
		}
		closeFns = append(closeFns, handle.Close)

		if err := attachFilterIfAny(handle, cfg.Filter); err != nil {
			closeAll(closeFns)
			return nil, fmt.Errorf("capture: attach filter on %s: %w", name, err)
		}

		sources = append(sources, source{
			name:   name,
			index:  iface.Index,
			handle: handle,
		})
	}

	writer, err := buildWriter(cfg, sources)
	if err != nil {
		closeAll(closeFns)
		return nil, err
	}
	if writer != nil {
		closeFns = append(closeFns, writer.Close)
	}

	return &Capture{
		cfg:      cfg,
		sources:  sources,
		writer:   writer,
		closeFns: closeFns,
	}, nil
}

// Run 启动捕获，直到 ctx 取消或发生错误。
func (c *Capture) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(c.sources))
	var wg sync.WaitGroup

	for _, src := range c.sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.captureLoop(ctx, src); err != nil {
				select {
				case errCh <- err:
				default:
				}
				cancel()
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			_ = c.Close()
			return err
		}
	}

	return c.Close()
}

func (c *Capture) captureLoop(ctx context.Context, src source) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		pkt, err := src.handle.ReadPacket()
		if err != nil {
			if err == rawcap.ErrHandleClosed {
				return nil
			}
			return fmt.Errorf("capture: read %s: %w", src.name, err)
		}
		if pkt == nil || len(pkt.Data) == 0 {
			continue
		}

		ts := time.Now().UTC()
		if pkt.Info != nil && !pkt.Info.Timestamp.IsZero() {
			ts = pkt.Info.Timestamp
		}

		if c.writer != nil {
			if err := c.writer.WritePacket(src, pkt.Data, ts); err != nil {
				return fmt.Errorf("capture: write %s: %w", src.name, err)
			}
		}
	}
}

// Close 关闭所有句柄与输出。
func (c *Capture) Close() error {
	closeAll(c.closeFns)
	c.closeFns = nil
	return nil
}

func normalizeConfig(cfg Config) Config {
	if cfg.SnapLen <= 0 {
		cfg.SnapLen = rawcap.DefaultSnapLen
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = rawcap.DefaultBufferSize
	}
	if cfg.Format == "" {
		cfg.Format = FormatPCAPNG
	}
	if cfg.LinkType == 0 {
		cfg.LinkType = 1 // LINKTYPE_ETHERNET
	}
	return cfg
}

func closeAll(fns []func() error) {
	for _, fn := range fns {
		_ = fn()
	}
}

func buildWriter(cfg Config, sources []source) (packetWriter, error) {
	if cfg.Writer == nil && cfg.OutputPath == "" {
		return nil, nil
	}

	var w io.Writer
	var closer io.Closer

	if cfg.Writer != nil {
		w = cfg.Writer
		if c, ok := w.(io.Closer); ok {
			closer = c
		}
	} else {
		file, err := os.Create(cfg.OutputPath)
		if err != nil {
			return nil, fmt.Errorf("capture: create output: %w", err)
		}
		w = file
		closer = file
	}

	switch cfg.Format {
	case FormatPCAP:
		if len(sources) != 1 {
			if closer != nil {
				_ = closer.Close()
			}
			return nil, fmt.Errorf("capture: pcap requires single interface")
		}
		writer, err := pcap.NewWriter(w, pcap.WithSnapLen(uint32(cfg.SnapLen)), pcap.WithLinkType(cfg.LinkType))
		if err != nil {
			if closer != nil {
				_ = closer.Close()
			}
			return nil, err
		}
		return &pcapWriter{writer: writer, closer: closer}, nil
	case FormatPCAPNG:
		wr, err := pcapng.NewWriter(w, pcapng.WithDefaultTimestampResolution(time.Microsecond))
		if err != nil {
			if closer != nil {
				_ = closer.Close()
			}
			return nil, err
		}
		pw := &pcapngWriter{writer: wr, closer: closer, snapLen: uint32(cfg.SnapLen)}
		for i := range sources {
			ifaceID, err := wr.AddInterface(uint16(cfg.LinkType), pw.snapLen)
			if err != nil {
				_ = pw.Close()
				return nil, err
			}
			sources[i].writerID = ifaceID
		}
		return pw, nil
	default:
		if closer != nil {
			_ = closer.Close()
		}
		return nil, fmt.Errorf("capture: unsupported format %s", cfg.Format)
	}
}
