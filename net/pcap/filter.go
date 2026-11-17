package pcap

import (
	"errors"
	"io"

	"golang.org/x/net/bpf"
)

// FilterCopy detects the input format, filters packets by BPF, and writes
// the same format to w. PCAPNG sections, interface descriptions, packet options,
// and unknown blocks are preserved. It closes w if w implements io.Closer.
func FilterCopy(r io.Reader, w io.Writer, prog []bpf.Instruction) (count int, retErr error) {
	reader, err := NewReader(r)
	if err != nil {
		return 0, err
	}
	vm, err := bpf.NewVM(prog)
	if err != nil {
		return 0, err
	}
	if reader.Format() == FormatPCAPNG {
		return filterCopyNG(reader.ng, w, vm)
	}
	hdr := reader.Header()
	writer, err := NewWriter(w,
		WithSnapLen(hdr.SnapLen),
		WithByteOrder(hdr.ByteOrder()),
		WithVersion(hdr.VersionMajor, hdr.VersionMinor),
		WithTimeZone(hdr.ThisZone),
		WithSigFigs(hdr.SigFigs),
		WithLinkType(hdr.Network),
		WithTimestampResolution(hdr.TimestampResolution()))
	if err != nil {
		return 0, err
	}
	defer func() { retErr = errors.Join(retErr, writer.Close()) }()

	for {
		pkt, err := reader.ReadPacket()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return count, nil
			}
			return count, err
		}
		keep, err := vm.Run(pkt.Data)
		if err != nil {
			return count, err
		}
		if keep != 0 {
			if err := writer.WritePacket(pkt); err != nil {
				return count, err
			}
			count++
		}
	}
}
