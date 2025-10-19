package pcapng

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

type Reader struct {
	r             io.Reader
	order         binary.ByteOrder
	section       *SectionHeaderBlock
	interfaces    map[uint32]*interfaceInfo
	nextInterface uint32
	defaultRes    time.Duration
}

type interfaceInfo struct {
	block *InterfaceDescriptionBlock
	tsRes time.Duration
}

type Packet struct {
	InterfaceID uint32
	Data        []byte
	Timestamp   time.Time
	CapturedLen uint32
	OriginalLen uint32
	Options     []Option
}

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:          r,
		interfaces: make(map[uint32]*interfaceInfo),
		defaultRes: time.Microsecond,
	}
}

func (r *Reader) CurrentSection() *SectionHeaderBlock {
	return r.section
}

func (r *Reader) NextBlock() (Block, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r.r, hdr[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}

	var blockType BlockType
	if r.order != nil {
		blockType = BlockType(r.order.Uint32(hdr[0:4]))
	} else {
		blockType = BlockType(binary.LittleEndian.Uint32(hdr[0:4]))
	}
	if r.order == nil && blockType != SectionHeaderBlockType {
		return nil, ErrInvalidSection
	}

	switch blockType {
	case SectionHeaderBlockType:
		var bom [4]byte
		if _, err := io.ReadFull(r.r, bom[:]); err != nil {
			return nil, err
		}
		byteOrderMagic := binary.BigEndian.Uint32(bom[:])

		var order binary.ByteOrder
		switch byteOrderMagic {
		case ByteOrderMagicLittle:
			order = binary.LittleEndian
		case ByteOrderMagicBig:
			order = binary.BigEndian
		default:
			return nil, ErrInvalidSection
		}

		totalLength := order.Uint32(hdr[4:8])
		body, err := r.readBody(totalLength, bom[:])
		if err != nil {
			return nil, err
		}

		block, err := parseSectionHeaderBlock(totalLength, order, body)
		if err != nil {
			return nil, err
		}

		r.order = order
		r.section = block
		r.interfaces = make(map[uint32]*interfaceInfo)
		r.nextInterface = 0

		return block, nil

	case InterfaceDescriptionBlockType:
		totalLength := r.order.Uint32(hdr[4:8])
		body, err := r.readBody(totalLength, nil)
		if err != nil {
			return nil, err
		}

		block, err := parseInterfaceDescriptionBlock(totalLength, r.order, body)
		if err != nil {
			return nil, err
		}

		block.ID = r.nextInterface
		r.nextInterface++

		tsRes := extractTimestampResolution(block.Options)
		if tsRes == 0 {
			tsRes = r.defaultRes
		}
		r.interfaces[block.ID] = &interfaceInfo{
			block: block,
			tsRes: tsRes,
		}
		return block, nil

	case EnhancedPacketBlockType:
		totalLength := r.order.Uint32(hdr[4:8])
		body, err := r.readBody(totalLength, nil)
		if err != nil {
			return nil, err
		}

		block, err := parseEnhancedPacketBlock(totalLength, r.order, body)
		if err != nil {
			return nil, err
		}
		return block, nil

	default:
		totalLength := r.order.Uint32(hdr[4:8])
		if _, err := r.readBody(totalLength, nil); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("pcapng: unsupported block type %d", blockType)
	}
}

func (r *Reader) ReadPacket() (*Packet, error) {
	for {
		block, err := r.NextBlock()
		if err != nil {
			return nil, err
		}

		epb, ok := block.(*EnhancedPacketBlock)
		if !ok {
			continue
		}

		info := r.interfaces[epb.InterfaceID]
		tsRes := r.defaultRes
		if info != nil && info.tsRes != 0 {
			tsRes = info.tsRes
		}

		packet := &Packet{
			InterfaceID: epb.InterfaceID,
			Data:        append([]byte(nil), epb.PacketData...),
			Timestamp:   epb.Timestamp(tsRes),
			CapturedLen: epb.CapturedLen,
			OriginalLen: epb.OriginalLen,
			Options:     append([]Option(nil), epb.Options...),
		}
		return packet, nil
	}
}

func (r *Reader) readBody(totalLength uint32, prefix []byte) ([]byte, error) {
	if totalLength < 12 {
		return nil, ErrInvalidBlockLength
	}
	bodyLen := int(totalLength) - 8
	if bodyLen < len(prefix) {
		return nil, ErrInvalidBlockLength
	}
	body := make([]byte, bodyLen)
	copy(body, prefix)
	if _, err := io.ReadFull(r.r, body[len(prefix):]); err != nil {
		return nil, err
	}
	return body, nil
}

func parseSectionHeaderBlock(totalLength uint32, order binary.ByteOrder, body []byte) (*SectionHeaderBlock, error) {
	if len(body) < 20 {
		return nil, ErrInvalidBlockLength
	}

	trailer := order.Uint32(body[len(body)-4:])
	if trailer != totalLength {
		return nil, ErrInvalidBlockLength
	}

	payload := body[:len(body)-4]

	byteOrderMagic := binary.BigEndian.Uint32(payload[0:4])
	major := order.Uint16(payload[4:6])
	minor := order.Uint16(payload[6:8])
	if len(payload) < 16 {
		return nil, ErrInvalidSection
	}
	sectionLength := int64(order.Uint64(payload[8:16]))

	optionsData := payload[16:]
	options, err := parseOptions(optionsData, order)
	if err != nil {
		return nil, err
	}

	return &SectionHeaderBlock{
		BlockHeader: BlockHeader{
			Type:        SectionHeaderBlockType,
			TotalLength: totalLength,
		},
		ByteOrderMagic: byteOrderMagic,
		MajorVersion:   major,
		MinorVersion:   minor,
		SectionLength:  sectionLength,
		Options:        options,
	}, nil
}

func parseInterfaceDescriptionBlock(totalLength uint32, order binary.ByteOrder, body []byte) (*InterfaceDescriptionBlock, error) {
	if len(body) < 12 {
		return nil, ErrInvalidBlockLength
	}
	trailer := order.Uint32(body[len(body)-4:])
	if trailer != totalLength {
		return nil, ErrInvalidBlockLength
	}
	payload := body[:len(body)-4]
	if len(payload) < 8 {
		return nil, ErrInvalidBlockLength
	}

	linkType := order.Uint16(payload[0:2])
	reserved := order.Uint16(payload[2:4])
	snapLen := order.Uint32(payload[4:8])
	optionsData := payload[8:]
	options, err := parseOptions(optionsData, order)
	if err != nil {
		return nil, err
	}

	return &InterfaceDescriptionBlock{
		BlockHeader: BlockHeader{
			Type:        InterfaceDescriptionBlockType,
			TotalLength: totalLength,
		},
		LinkType: linkType,
		Reserved: reserved,
		SnapLen:  snapLen,
		Options:  options,
	}, nil
}

func parseEnhancedPacketBlock(totalLength uint32, order binary.ByteOrder, body []byte) (*EnhancedPacketBlock, error) {
	if len(body) < 24 {
		return nil, ErrInvalidBlockLength
	}

	trailer := order.Uint32(body[len(body)-4:])
	if trailer != totalLength {
		return nil, ErrInvalidBlockLength
	}

	payload := body[:len(body)-4]
	if len(payload) < 20 {
		return nil, ErrInvalidBlockLength
	}

	epb := &EnhancedPacketBlock{
		BlockHeader: BlockHeader{
			Type:        EnhancedPacketBlockType,
			TotalLength: totalLength,
		},
		InterfaceID:   order.Uint32(payload[0:4]),
		TimestampHigh: order.Uint32(payload[4:8]),
		TimestampLow:  order.Uint32(payload[8:12]),
		CapturedLen:   order.Uint32(payload[12:16]),
		OriginalLen:   order.Uint32(payload[16:20]),
	}

	offset := 20
	if int(epb.CapturedLen) > len(payload)-offset {
		return nil, ErrInvalidBlockLength
	}

	epb.PacketData = make([]byte, epb.CapturedLen)
	copy(epb.PacketData, payload[offset:offset+int(epb.CapturedLen)])
	offset += int(epb.CapturedLen)

	padding := (4 - (epb.CapturedLen % 4)) % 4
	if offset+int(padding) > len(payload) {
		return nil, ErrInvalidBlockLength
	}
	offset += int(padding)

	optionsData := payload[offset:]
	options, err := parseOptions(optionsData, order)
	if err != nil {
		return nil, err
	}
	epb.Options = options

	return epb, nil
}

func parseOptions(data []byte, order binary.ByteOrder) ([]Option, error) {
	var options []Option
	reader := bytes.NewReader(data)
	for reader.Len() >= 4 {
		var code uint16
		var length uint16
		if err := binary.Read(reader, order, &code); err != nil {
			return nil, err
		}
		if err := binary.Read(reader, order, &length); err != nil {
			return nil, err
		}

		if code == 0 { // end of options
			pad := (4 - (int(length) % 4)) % 4
			if err := discard(reader, int(length)+pad); err != nil {
				return nil, err
			}
			break
		}

		value := make([]byte, length)
		if _, err := io.ReadFull(reader, value); err != nil {
			return nil, err
		}

		options = append(options, Option{
			Code:  code,
			Value: value,
		})

		pad := (4 - (int(length) % 4)) % 4
		if err := discard(reader, pad); err != nil {
			return nil, err
		}
	}
	return options, nil
}

func discard(r *bytes.Reader, n int) error {
	if n == 0 {
		return nil
	}
	if n < 0 || n > r.Len() {
		return ErrInvalidBlockLength
	}
	_, err := r.Seek(int64(n), io.SeekCurrent)
	return err
}

func extractTimestampResolution(options []Option) time.Duration {
	const (
		ifTsResolCode = 9
	)
	for _, opt := range options {
		if opt.Code == ifTsResolCode && len(opt.Value) > 0 {
			return parseTimestampResolution(opt.Value[0])
		}
	}
	return 0
}

func parseTimestampResolution(raw byte) time.Duration {
	if raw&0x80 == 0 {
		power := int(raw)
		switch power {
		case 6:
			return time.Microsecond
		case 9:
			return time.Nanosecond
		default:
			if power >= 0 && power <= 9 {
				divisor := int64(1)
				for i := 0; i < power; i++ {
					divisor *= 10
				}
				return time.Second / time.Duration(divisor)
			}
		}
		return time.Microsecond
	}

	power := int(raw & 0x7f)
	if power >= 0 && power <= 30 {
		divisor := int64(1) << power
		return time.Second / time.Duration(divisor)
	}
	return time.Microsecond
}
