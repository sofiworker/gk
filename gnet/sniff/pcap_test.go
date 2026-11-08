package sniff

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/pcap"
)

func TestPcapSources(t *testing.T) {
	for _, format := range []pcap.Format{pcap.FormatPCAP, pcap.FormatPCAPNG} {
		for _, constructor := range []struct {
			name      string
			newSource func(*pcap.Reader) *PcapSource
		}{
			{"pcap", NewPcapSource}, {"pcapng alias", NewPcapngSource},
		} {
			t.Run(string(format)+"/"+constructor.name, func(t *testing.T) {
				var input bytes.Buffer
				w, err := pcap.NewWriter(&input, pcap.WithFormat(format))
				if err != nil {
					t.Fatal(err)
				}
				id, err := w.AddInterface(1, 65535)
				if err != nil {
					t.Fatal(err)
				}
				if format == pcap.FormatPCAPNG {
					id, err = w.AddInterface(1, 65535)
					if err != nil {
						t.Fatal(err)
					}
				}
				ts := time.Unix(1, 0)
				if err := w.WritePacket(&pcap.Packet{InterfaceID: id, Data: []byte{1}, OriginalLen: 20, Timestamp: ts}); err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				r, err := pcap.NewReader(bytes.NewReader(input.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				src := constructor.newSource(r)
				got, err := src.Next()
				if err != nil || got.InterfaceID != int(id) || got.CaptureLen != 1 || got.OriginalLen != 20 || !got.Timestamp.Equal(ts) {
					t.Fatalf("packet = %+v, %v", got, err)
				}
				if _, err := src.Next(); !errors.Is(err, io.EOF) {
					t.Fatal(err)
				}
			})
		}
	}
}
