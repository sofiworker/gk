package capture

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/sofiworker/gk/gnet/pcap"
)

type writerOutput struct {
	bytes.Buffer
	closes int
}

func (w *writerOutput) Close() error { w.closes++; return nil }

func TestBuildWriterFormats(t *testing.T) {
	for _, format := range []Format{FormatPCAP, FormatPCAPNG} {
		t.Run(string(format), func(t *testing.T) {
			output := &writerOutput{}
			sources := []source{{name: "eth0"}}
			if format == FormatPCAPNG {
				sources = append(sources, source{name: "eth1"})
			}
			w, err := buildWriter(normalizeConfig(Config{Writer: output, Format: format}), sources)
			if err != nil {
				t.Fatal(err)
			}
			for _, src := range sources {
				if err := w.WritePacket(src, []byte{byte(src.writerID)}, time.Unix(1, 0)); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if output.closes != 1 {
				t.Fatalf("output closed %d times", output.closes)
			}
			r, err := pcap.NewReader(bytes.NewReader(output.Bytes()))
			if err != nil || r.Format() != format {
				t.Fatalf("reader = %+v, %v", r, err)
			}
			for _, src := range sources {
				pkt, err := r.ReadPacket()
				if err != nil || pkt.InterfaceID != src.writerID || !bytes.Equal(pkt.Data, []byte{byte(src.writerID)}) {
					t.Fatalf("packet = %+v, %v", pkt, err)
				}
			}
			if _, err := r.ReadPacket(); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildWriterFailures(t *testing.T) {
	for _, tt := range []struct {
		format  Format
		sources []source
		link    uint32
	}{
		{"unknown", []source{{}}, 1},
		{FormatPCAP, []source{{}, {}}, 1},
		{FormatPCAPNG, []source{{}}, 1 << 16},
	} {
		output := &writerOutput{}
		if _, err := buildWriter(normalizeConfig(Config{Writer: output, Format: tt.format, LinkType: tt.link}), tt.sources); err == nil {
			t.Fatal("invalid config accepted")
		}
		if output.closes != 1 {
			t.Fatalf("output closed %d times", output.closes)
		}
	}
	if w, err := buildWriter(Config{}, nil); err != nil || w != nil {
		t.Fatalf("empty output = %+v, %v", w, err)
	}
}
