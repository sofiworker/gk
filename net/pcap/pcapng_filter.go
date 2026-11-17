package pcap

import (
	"errors"
	"fmt"
	"io"

	"golang.org/x/net/bpf"
)

func filterCopyNG(reader *ngReader, w io.Writer, vm *bpf.VM) (count int, retErr error) {
	if closer, ok := w.(io.Closer); ok {
		defer func() { retErr = errors.Join(retErr, closer.Close()) }()
	}
	writer := &ngWriter{w: w}
	for {
		block, err := reader.NextBlock()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		switch block := block.(type) {
		case *SectionHeaderBlock:
			writer.order = reader.order
			writer.major, writer.minor = block.MajorVersion, block.MinorVersion
			writer.sectionLength = -1
			writer.sectionOpts = block.Options
			if err := writer.writeSectionHeader(); err != nil {
				return count, err
			}
		case *InterfaceDescriptionBlock:
			if err := writer.writeInterfaceBlock(block); err != nil {
				return count, err
			}
		case *EnhancedPacketBlock:
			if _, _, _, ok := reader.InterfaceInfo(block.InterfaceID); !ok {
				return count, fmt.Errorf("%w: %d", ErrUnknownInterface, block.InterfaceID)
			}
			keep, err := vm.Run(block.PacketData)
			if err != nil {
				return count, err
			}
			if keep == 0 {
				continue
			}
			if err := writer.writeEnhancedPacket(block.InterfaceID, block.TimestampHigh,
				block.TimestampLow, block.CapturedLen, block.OriginalLen,
				block.PacketData, block.Options); err != nil {
				return count, err
			}
			count++
		case *UnknownBlock:
			var header [8]byte
			writer.order.PutUint32(header[:4], uint32(block.Type))
			writer.order.PutUint32(header[4:], uint32(8+len(block.Body)))
			if _, err := w.Write(header[:]); err != nil {
				return count, err
			}
			if _, err := w.Write(block.Body); err != nil {
				return count, err
			}
		}
	}
}
