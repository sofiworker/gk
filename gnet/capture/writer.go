package capture

import (
	"sync"
	"time"

	"github.com/sofiworker/gk/gnet/pcap"
)

type captureWriter struct {
	writer *pcap.Writer
	mu     sync.Mutex
}

func (w *captureWriter) WritePacket(src source, data []byte, ts time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.WritePacket(&pcap.Packet{InterfaceID: src.writerID, Data: data, Timestamp: ts})
}

func (w *captureWriter) Close() error { return w.writer.Close() }
