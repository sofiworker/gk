package pcap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/iotest"
	"time"

	"golang.org/x/net/bpf"
)

func TestFormatsRoundTrip(t *testing.T) {
	for _, format := range []Format{FormatPCAP, FormatPCAPNG} {
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			for _, resolution := range []time.Duration{time.Microsecond, time.Nanosecond} {
				t.Run(string(format)+"/"+order.String()+"/"+resolution.String(), func(t *testing.T) {
					var output bytes.Buffer
					w, err := NewWriter(&output, WithFormat(format), WithByteOrder(order),
						WithTimestampResolution(resolution), WithSnapLen(64), WithLinkType(101), WithBuffer(4096))
					if err != nil {
						t.Fatal(err)
					}
					if w.Format() != format {
						t.Fatalf("writer format = %s", w.Format())
					}
					id, err := w.AddInterface(101, 64)
					if err != nil {
						t.Fatal(err)
					}
					ts := time.Unix(1700000000, 123456789).UTC().Truncate(resolution)
					pkt := &Packet{InterfaceID: id, Data: []byte{1, 2, 3, 4}, CapturedLen: 3, OriginalLen: 20, Timestamp: ts}
					if format == FormatPCAPNG {
						pkt.Options = []Option{{Code: 1, Value: []byte("comment")}}
					}
					if err := w.WritePacket(pkt); err != nil {
						t.Fatal(err)
					}
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}
					if pkt.CapturedLen != 3 || pkt.OriginalLen != 20 || len(pkt.Data) != 4 {
						t.Fatal("packet mutated")
					}
					r, err := NewReader(iotest.OneByteReader(bytes.NewReader(output.Bytes())))
					if err != nil {
						t.Fatal(err)
					}
					if r.Format() != format {
						t.Fatalf("reader format = %s", r.Format())
					}
					got, err := r.ReadPacket()
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got.Data, pkt.Data[:3]) || !got.Timestamp.Equal(ts) || got.CapturedLen != 3 ||
						got.OriginalLength() != 20 || got.CaptureLength() != 3 || got.LinkType != 101 || got.InterfaceID != id {
						t.Fatalf("packet = %+v", got)
					}
					link, snap, res, ok := r.InterfaceInfo(id)
					if !ok || link != 101 || snap != 64 || res != resolution {
						t.Fatalf("interface = %d %d %v %v", link, snap, res, ok)
					}
					if _, _, _, ok := r.InterfaceInfo(99); ok {
						t.Fatal("unknown interface found")
					}
					if format == FormatPCAP {
						if r.Header() != w.Header() || r.CurrentSection() != nil {
							t.Fatal("classic metadata mismatch")
						}
						if _, err := r.NextBlock(); !errors.Is(err, ErrUnsupportedFormat) {
							t.Fatalf("NextBlock = %v", err)
						}
					} else {
						if r.Header() != (FileHeader{}) || w.Header() != (FileHeader{}) || r.CurrentSection() == nil {
							t.Fatal("pcapng metadata mismatch")
						}
						if !reflect.DeepEqual(got.Options, pkt.Options) {
							t.Fatalf("options = %+v", got.Options)
						}
					}
					if _, err := r.ReadPacket(); !errors.Is(err, io.EOF) {
						t.Fatalf("EOF = %v", err)
					}
				})
			}
		}
	}
}

func TestReaderInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, io.EOF},
		{"short signature", []byte{1}, io.ErrUnexpectedEOF},
		{"unknown format", []byte{1, 2, 3, 4}, ErrInvalidMagicNumber},
		{"short classic header", []byte{0xd4, 0xc3, 0xb2, 0xa1}, io.ErrUnexpectedEOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewReader(bytes.NewReader(tt.data))
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
	r, err := NewReader(bytes.NewReader([]byte{0x0a, 0x0d, 0x0d, 0x0a}))
	if err != nil {
		t.Fatal(err)
	}
	if r.CurrentSection() != nil {
		t.Fatal("section parsed too early")
	}
	if _, err := r.ReadPacket(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated pcapng = %v", err)
	}
}

func TestWriterInvalidOptions(t *testing.T) {
	for name, option := range map[string]WriterOption{
		"format": WithFormat("unknown"), "snaplen": WithSnapLen(0), "buffer": WithBuffer(0),
		"byte order": WithByteOrder(nil), "resolution": WithTimestampResolution(time.Second),
		"version": WithVersion(0, 0), "section version": WithSectionVersion(0, 0),
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if _, err := NewWriter(&output, option); err == nil {
				t.Fatal("invalid option accepted")
			}
			if output.Len() != 0 {
				t.Fatal("invalid config wrote output")
			}
		})
	}
	if _, err := NewWriter(io.Discard, WithFormat("unknown")); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatal(err)
	}
	if _, err := NewWriter(io.Discard, WithFormat(FormatPCAPNG), WithLinkType(1<<16)); !errors.Is(err, ErrInvalidInterface) {
		t.Fatal(err)
	}
}

func TestWriterInvalidPacketsAndInterfaces(t *testing.T) {
	for _, format := range []Format{FormatPCAP, FormatPCAPNG} {
		t.Run(string(format), func(t *testing.T) {
			w, err := NewWriter(io.Discard, WithFormat(format))
			if err != nil {
				t.Fatal(err)
			}
			for _, pkt := range []*Packet{nil, {Data: []byte{1}, CapturedLen: 2}} {
				if err := w.WritePacket(pkt); !errors.Is(err, ErrInvalidPacket) {
					t.Fatalf("invalid packet = %v", err)
				}
			}
			if err := w.WritePacket(&Packet{InterfaceID: 99}); !errors.Is(err, ErrUnknownInterface) {
				t.Fatalf("unknown interface = %v", err)
			}
			if format == FormatPCAP {
				for _, args := range [][2]uint32{{2, 65535}, {1, 64}} {
					if _, err := w.AddInterface(args[0], args[1]); !errors.Is(err, ErrInvalidInterface) {
						t.Fatal(err)
					}
				}
				if _, err := w.AddInterface(1, 65535, WithInterfaceOption(1, nil)); !errors.Is(err, ErrInvalidInterface) {
					t.Fatal(err)
				}
				if _, err := w.AddInterface(1, 65535); err != nil {
					t.Fatal(err)
				}
				if _, err := w.AddInterface(1, 65535); !errors.Is(err, ErrInvalidInterface) {
					t.Fatal(err)
				}
			} else if _, err := w.AddInterface(1<<16, 65535); !errors.Is(err, ErrInvalidInterface) {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type captureOutput struct {
	bytes.Buffer
	closes   int
	closeErr error
}

func (w *captureOutput) Close() error { w.closes++; return w.closeErr }

func TestWriterDefaultInterfaceAndClose(t *testing.T) {
	for _, format := range []Format{FormatPCAP, FormatPCAPNG} {
		t.Run(string(format), func(t *testing.T) {
			output := &captureOutput{}
			w, err := NewWriter(output, WithFormat(format), WithBuffer(4096), WithLinkType(101))
			if err != nil {
				t.Fatal(err)
			}
			if err := w.WritePacketData([]byte{1, 2}, time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if output.closes != 1 {
				t.Fatalf("close count = %d", output.closes)
			}
			r, err := NewReader(bytes.NewReader(output.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			pkt, err := r.ReadPacket()
			if err != nil || pkt.LinkType != 101 || !bytes.Equal(pkt.Data, []byte{1, 2}) {
				t.Fatalf("packet = %+v, %v", pkt, err)
			}
		})
	}
}

func TestNewFileWriterFormats(t *testing.T) {
	for _, format := range []Format{FormatPCAP, FormatPCAPNG} {
		t.Run(string(format), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "capture")
			w, closeWriter, err := NewFileWriter(path, WithFormat(format), WithBuffer(4096))
			if err != nil {
				t.Fatal(err)
			}
			if err := w.WritePacketData([]byte{1}, time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
			if err := closeWriter(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := NewReader(bytes.NewReader(data))
			if err != nil || r.Format() != format {
				t.Fatalf("reader = %+v, %v", r, err)
			}
			if _, err := r.ReadPacket(); err != nil {
				t.Fatal(err)
			}
		})
	}
	dir := t.TempDir()
	if _, _, err := NewFileWriter(dir); err == nil {
		t.Fatal("directory accepted")
	}
	path := filepath.Join(dir, "invalid")
	if _, _, err := NewFileWriter(path, WithFormat("bad")); err == nil {
		t.Fatal("bad format accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("failed constructor leaked file: %v", err)
	}
}

func TestFilterCopyPreservesFormats(t *testing.T) {
	for _, format := range []Format{FormatPCAP, FormatPCAPNG} {
		t.Run(string(format), func(t *testing.T) {
			var input bytes.Buffer
			w, err := NewWriter(&input, WithFormat(format), WithByteOrder(binary.BigEndian),
				WithDefaultTimestampResolution(time.Nanosecond), WithSnapLen(64),
				WithTimeZone(-3600), WithSigFigs(7), WithVersion(2, 3))
			if err != nil {
				t.Fatal(err)
			}
			ts := time.Unix(1700000000, 123456789).UTC()
			for _, data := range [][]byte{{0x41, 1}, {0x42, 2}} {
				if err := w.WritePacket(&Packet{Data: data, OriginalLen: 20, Timestamp: ts}); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			output := &captureOutput{}
			prog := []bpf.Instruction{bpf.LoadAbsolute{Off: 0, Size: 1}, bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x41, SkipFalse: 1}, bpf.RetConstant{Val: 0xffff}, bpf.RetConstant{Val: 0}}
			n, err := FilterCopy(bytes.NewReader(input.Bytes()), output, prog)
			if err != nil || n != 1 || output.closes != 1 {
				t.Fatalf("copy = %d, %v, closes %d", n, err, output.closes)
			}
			r, err := NewReader(bytes.NewReader(output.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			if r.Format() != format {
				t.Fatal(r.Format())
			}
			if format == FormatPCAP && r.Header() != w.Header() {
				t.Fatalf("header = %+v, want %+v", r.Header(), w.Header())
			}
			pkt, err := r.ReadPacket()
			if err != nil || pkt.OriginalLen != 20 || !pkt.Timestamp.Equal(ts) || !bytes.Equal(pkt.Data, []byte{0x41, 1}) {
				t.Fatalf("packet = %+v, %v", pkt, err)
			}
			if _, err := r.ReadPacket(); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			closeErr := errors.New("close failure")
			failing := &captureOutput{closeErr: closeErr}
			if _, err := FilterCopy(bytes.NewReader(input.Bytes()), failing, prog); !errors.Is(err, closeErr) {
				t.Fatalf("close failure = %v", err)
			}
		})
	}
}

func TestPCAPNGFilterSectionsAndMetadata(t *testing.T) {
	var input bytes.Buffer
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		w, err := NewWriter(&input, WithFormat(FormatPCAPNG), WithByteOrder(order),
			WithSectionVersion(1, 2), WithSectionLength(999), WithSectionOption(Option{Code: 1, Value: []byte("section")}))
		if err != nil {
			t.Fatal(err)
		}
		for _, link := range []uint32{1, 101} {
			id, err := w.AddInterface(link, 64, WithInterfaceTimestampResolution(time.Nanosecond), WithInterfaceOption(2, []byte("eth")))
			if err != nil {
				t.Fatal(err)
			}
			if err := w.WritePacket(&Packet{InterfaceID: id, Data: []byte{0x41}, OriginalLen: 10, Timestamp: time.Unix(2, 123), Options: []Option{{Code: 1, Value: []byte("packet")}}}); err != nil {
				t.Fatal(err)
			}
		}
		// Unknown blocks include their trailing block length in Body.
		var unknown [16]byte
		order.PutUint32(unknown[:4], 0x00000bad)
		order.PutUint32(unknown[4:8], 16)
		copy(unknown[8:12], []byte{1, 2, 3, 4})
		order.PutUint32(unknown[12:], 16)
		input.Write(unknown[:])
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	n, err := FilterCopy(bytes.NewReader(input.Bytes()), &output, []bpf.Instruction{bpf.RetConstant{Val: 65535}})
	if err != nil || n != 4 {
		t.Fatalf("copy = %d, %v", n, err)
	}
	r, err := NewReader(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	sections, interfaces, packets, unknowns := 0, 0, 0, 0
	for {
		block, err := r.NextBlock()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch block := block.(type) {
		case *SectionHeaderBlock:
			sections++
			if block.MinorVersion != 2 || block.SectionLength != -1 || len(block.Options) != 1 || string(block.Options[0].Value) != "section" {
				t.Fatalf("section = %+v", block)
			}
			if _, _, _, ok := r.InterfaceInfo(0); ok {
				t.Fatal("interfaces not reset between sections")
			}
		case *InterfaceDescriptionBlock:
			interfaces++
			_, _, res, ok := r.InterfaceInfo(block.ID)
			if !ok || res != time.Nanosecond || block.SnapLen != 64 || len(block.Options) != 2 {
				t.Fatalf("interface = %+v", block)
			}
		case *EnhancedPacketBlock:
			packets++
			if block.OriginalLen != 10 || block.TimestampLow != 2000000123 || len(block.Options) != 1 || string(block.Options[0].Value) != "packet" {
				t.Fatalf("packet block = %+v", block)
			}
		case *UnknownBlock:
			unknowns++
			if block.Type != 0xbad || !bytes.Equal(block.Body[:4], []byte{1, 2, 3, 4}) {
				t.Fatalf("unknown = %+v", block)
			}
		}
	}
	if sections != 2 || interfaces != 4 || packets != 4 || unknowns != 2 {
		t.Fatalf("blocks = %d %d %d %d", sections, interfaces, packets, unknowns)
	}
}

func TestPacketLengthFallbacks(t *testing.T) {
	for _, tt := range []struct {
		pkt  Packet
		want int
	}{
		{Packet{Data: []byte{1}}, 1},
		{Packet{Data: []byte{1}, Header: PacketHeader{OrigLen: 2}}, 2},
		{Packet{Data: []byte{1}, OriginalLen: 3, Header: PacketHeader{OrigLen: 2}}, 3},
	} {
		if got := tt.pkt.OriginalLength(); got != tt.want {
			t.Fatalf("length = %d, want %d", got, tt.want)
		}
	}
}
