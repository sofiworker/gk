package capture

import (
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
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}

type pcapngWriter struct {
	writer  *pcapng.Writer
	closer  interface{ Close() error }
	snapLen uint32
}

func (w *pcapngWriter) WritePacket(src source, data []byte, ts time.Time) error {
	return w.writer.WritePacket(src.writerID, data, ts)
}

func (w *pcapngWriter) Close() error {
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}
