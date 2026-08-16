package capture

import (
	"sync"
	"time"

	"github.com/sofiworker/gk/gnet/pcap"
	"github.com/sofiworker/gk/gnet/pcapng"
)

type pcapWriter struct {
	writer *pcap.Writer
	closer interface{ Close() error }
}

func (w *pcapWriter) WritePacket(src source, data []byte, ts time.Time) error {
	return w.writer.WritePacketData(data, ts)
}

func (w *pcapWriter) Close() error {
	// Writer.Close 内部 flush 缓冲并关闭底层文件。
	if err := w.writer.Close(); err != nil {
		return err
	}
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}

type pcapngWriter struct {
	writer  *pcapng.Writer
	closer  interface{ Close() error }
	snapLen uint32
	mu      sync.Mutex // 多网卡多 goroutine 并发写互斥
}

func (w *pcapngWriter) WritePacket(src source, data []byte, ts time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.WritePacket(src.writerID, data, ts)
}

func (w *pcapngWriter) Close() error {
	// Writer.Close 内部 flush 缓冲并关闭底层文件。
	if err := w.writer.Close(); err != nil {
		return err
	}
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}
