package pcapng

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

type WriterOption func(*writerConfig) error

type writerConfig struct {
	byteOrder     binary.ByteOrder
	major         uint16
	minor         uint16
	sectionLength int64
	sectionOpts   []Option
	defaultRes    time.Duration
	bufferSize    int
}

type Writer struct {
	w             io.Writer
	buf           *bufio.Writer
	order         binary.ByteOrder
	sectionLength int64
	sectionOpts   []Option
	defaultRes    time.Duration
	interfaces    map[uint32]*interfaceWriterInfo
	nextInterface uint32
	closer        io.Closer
	major         uint16
	minor         uint16
}

type interfaceWriterInfo struct {
	linkType uint16
	snapLen  uint32
	tsRes    time.Duration
}

func NewWriter(w io.Writer, opts ...WriterOption) (*Writer, error) {
	cfg := writerConfig{
		byteOrder:     binary.LittleEndian,
		major:         1,
		minor:         0,
		sectionLength: -1,
		defaultRes:    time.Microsecond,
	}

	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}

	writer := &Writer{
		w:             w,
		order:         cfg.byteOrder,
		sectionLength: cfg.sectionLength,
		sectionOpts:   append([]Option(nil), cfg.sectionOpts...),
		defaultRes:    cfg.defaultRes,
		interfaces:    make(map[uint32]*interfaceWriterInfo),
		major:         cfg.major,
		minor:         cfg.minor,
	}

	if closer, ok := w.(io.Closer); ok {
		writer.closer = closer
	}

	if cfg.bufferSize > 0 {
		writer.buf = bufio.NewWriterSize(w, cfg.bufferSize)
		writer.w = writer.buf
	}

	if err := writer.writeSectionHeader(); err != nil {
		return nil, err
	}

	return writer, nil
}

func (w *Writer) writeSectionHeader() error {
	options := encodeOptions(w.sectionOpts, w.order)
	payloadLen := 4 + 2 + 2 + 8 + len(options)
	totalLength := uint32(8 + payloadLen + 4)

	var buf bytes.Buffer
	putUint32(&buf, w.order, uint32(SectionHeaderBlockType))
	putUint32(&buf, w.order, totalLength)

	magic := ByteOrderMagicLittle
	if w.order == binary.BigEndian {
		magic = ByteOrderMagicBig
	}
	var magicBytes [4]byte
	binary.BigEndian.PutUint32(magicBytes[:], magic)
	buf.Write(magicBytes[:])

	putUint16(&buf, w.order, w.major)
	putUint16(&buf, w.order, w.minor)

	sectionLength := uint64(0xFFFFFFFFFFFFFFFF)
	if w.sectionLength >= 0 {
		sectionLength = uint64(w.sectionLength)
	}
	putUint64(&buf, w.order, sectionLength)

	buf.Write(options)
	putUint32(&buf, w.order, totalLength)

	_, err := w.w.Write(buf.Bytes())
	return err
}

func (w *Writer) AddInterface(linkType uint16, snapLen uint32, opts ...InterfaceOption) (uint32, error) {
	cfg := interfaceConfig{}
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return 0, err
		}
	}

	tsRes := cfg.tsResolution
	if tsRes == 0 {
		tsRes = w.defaultRes
	}
	options := append([]Option(nil), cfg.options...)

	addTSOption := false
	if cfg.hasTSOption {
		addTSOption = false
	} else if cfg.tsResolution != 0 {
		addTSOption = true
	} else if tsRes != time.Microsecond {
		addTSOption = true
	}

	if addTSOption {
		value, err := encodeTimestampResolution(tsRes)
		if err != nil {
			return 0, err
		}
		options = append(options, Option{
			Code:  9,
			Value: []byte{value},
		})
	}

	block := &InterfaceDescriptionBlock{
		BlockHeader: BlockHeader{
			Type: InterfaceDescriptionBlockType,
		},
		LinkType: linkType,
		Reserved: 0,
		SnapLen:  snapLen,
		Options:  options,
	}

	if err := w.writeInterfaceBlock(block); err != nil {
		return 0, err
	}

	id := w.nextInterface
	w.nextInterface++
	w.interfaces[id] = &interfaceWriterInfo{
		linkType: linkType,
		snapLen:  snapLen,
		tsRes:    tsRes,
	}
	return id, nil
}

func (w *Writer) writeInterfaceBlock(block *InterfaceDescriptionBlock) error {
	options := encodeOptions(block.Options, w.order)
	payloadLen := 2 + 2 + 4 + len(options)
	totalLength := uint32(8 + payloadLen + 4)

	var buf bytes.Buffer
	putUint32(&buf, w.order, uint32(InterfaceDescriptionBlockType))
	putUint32(&buf, w.order, totalLength)
	putUint16(&buf, w.order, block.LinkType)
	putUint16(&buf, w.order, block.Reserved)
	putUint32(&buf, w.order, block.SnapLen)
	buf.Write(options)
	putUint32(&buf, w.order, totalLength)

	_, err := w.w.Write(buf.Bytes())
	return err
}

func (w *Writer) WritePacket(interfaceID uint32, data []byte, ts time.Time, opts ...Option) error {
	info, ok := w.interfaces[interfaceID]
	if !ok {
		return fmt.Errorf("pcapng: unknown interface %d", interfaceID)
	}

	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}

	tsRes := info.tsRes
	if tsRes == 0 {
		tsRes = w.defaultRes
	}

	high, low, err := encodeTimestamp(ts, tsRes)
	if err != nil {
		return err
	}

	origLen := uint32(len(data))
	capturedLen := origLen
	if info.snapLen > 0 && capturedLen > info.snapLen {
		capturedLen = info.snapLen
	}

	payload := data[:capturedLen]
	return w.writeEnhancedPacket(interfaceID, high, low, capturedLen, origLen, payload, opts)
}

func (w *Writer) writeEnhancedPacket(interfaceID, tsHigh, tsLow, capturedLen, originalLen uint32, payload []byte, opts []Option) error {
	padding := (4 - (capturedLen % 4)) % 4
	options := encodeOptions(opts, w.order)
	payloadLen := 4 + 4 + 4 + 4 + 4 + int(capturedLen) + int(padding) + len(options)
	totalLength := uint32(8 + payloadLen + 4)

	var buf bytes.Buffer
	putUint32(&buf, w.order, uint32(EnhancedPacketBlockType))
	putUint32(&buf, w.order, totalLength)
	putUint32(&buf, w.order, interfaceID)
	putUint32(&buf, w.order, tsHigh)
	putUint32(&buf, w.order, tsLow)
	putUint32(&buf, w.order, capturedLen)
	putUint32(&buf, w.order, originalLen)
	buf.Write(payload)
	if padding > 0 {
		buf.Write(make([]byte, padding))
	}
	buf.Write(options)
	putUint32(&buf, w.order, totalLength)

	_, err := w.w.Write(buf.Bytes())
	return err
}

func (w *Writer) Close() error {
	if w.buf != nil {
		if err := w.buf.Flush(); err != nil {
			return err
		}
	}
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}

func encodeOptions(options []Option, order binary.ByteOrder) []byte {
	var buf bytes.Buffer
	for _, opt := range options {
		putUint16(&buf, order, opt.Code)
		putUint16(&buf, order, uint16(len(opt.Value)))
		buf.Write(opt.Value)
		pad := (4 - (len(opt.Value) % 4)) % 4
		if pad > 0 {
			buf.Write(make([]byte, pad))
		}
	}
	// end of options
	putUint16(&buf, order, 0)
	putUint16(&buf, order, 0)
	return buf.Bytes()
}

func encodeTimestampResolution(d time.Duration) (byte, error) {
	switch d {
	case time.Nanosecond:
		return 9, nil
	case time.Microsecond:
		return 6, nil
	default:
		return 0, fmt.Errorf("pcapng: unsupported timestamp resolution %s", d)
	}
}

func encodeTimestamp(ts time.Time, resolution time.Duration) (uint32, uint32, error) {
	var value uint64
	switch resolution {
	case time.Nanosecond:
		value = uint64(ts.Unix())*1_000_000_000 + uint64(ts.Nanosecond())
	case time.Microsecond:
		value = uint64(ts.Unix())*1_000_000 + uint64(ts.Nanosecond()/1000)
	default:
		return 0, 0, fmt.Errorf("pcapng: unsupported timestamp resolution %s", resolution)
	}
	high := uint32(value >> 32)
	low := uint32(value & 0xffffffff)
	return high, low, nil
}

func putUint16(buf *bytes.Buffer, order binary.ByteOrder, value uint16) {
	var tmp [2]byte
	order.PutUint16(tmp[:], value)
	buf.Write(tmp[:])
}

func putUint32(buf *bytes.Buffer, order binary.ByteOrder, value uint32) {
	var tmp [4]byte
	order.PutUint32(tmp[:], value)
	buf.Write(tmp[:])
}

func putUint64(buf *bytes.Buffer, order binary.ByteOrder, value uint64) {
	var tmp [8]byte
	order.PutUint64(tmp[:], value)
	buf.Write(tmp[:])
}

type InterfaceOption func(*interfaceConfig) error

type interfaceConfig struct {
	options      []Option
	tsResolution time.Duration
	hasTSOption  bool
}

func WithInterfaceOption(code uint16, value []byte) InterfaceOption {
	return func(cfg *interfaceConfig) error {
		cfg.options = append(cfg.options, Option{
			Code:  code,
			Value: append([]byte(nil), value...),
		})
		if code == 9 {
			cfg.hasTSOption = true
			if len(value) > 0 {
				cfg.tsResolution = parseTimestampResolution(value[0])
			}
		}
		return nil
	}
}

func WithInterfaceTimestampResolution(res time.Duration) InterfaceOption {
	return func(cfg *interfaceConfig) error {
		cfg.tsResolution = res
		return nil
	}
}

func WithByteOrder(order binary.ByteOrder) WriterOption {
	return func(cfg *writerConfig) error {
		if order != binary.BigEndian && order != binary.LittleEndian {
			return fmt.Errorf("pcapng: unsupported byte order")
		}
		cfg.byteOrder = order
		return nil
	}
}

func WithSectionVersion(major, minor uint16) WriterOption {
	return func(cfg *writerConfig) error {
		if major == 0 {
			return fmt.Errorf("pcapng: section version major must be positive")
		}
		cfg.major = major
		cfg.minor = minor
		return nil
	}
}

func WithSectionLength(length int64) WriterOption {
	return func(cfg *writerConfig) error {
		cfg.sectionLength = length
		return nil
	}
}

func WithSectionOption(option Option) WriterOption {
	return func(cfg *writerConfig) error {
		cfg.sectionOpts = append(cfg.sectionOpts, option)
		return nil
	}
}

func WithDefaultTimestampResolution(res time.Duration) WriterOption {
	return func(cfg *writerConfig) error {
		switch res {
		case time.Microsecond, time.Nanosecond:
			cfg.defaultRes = res
			return nil
		default:
			return fmt.Errorf("pcapng: unsupported default timestamp resolution %s", res)
		}
	}
}

// WithBuffer 启用带缓冲写入以减少系统调用。
func WithBuffer(size int) WriterOption {
	return func(cfg *writerConfig) error {
		if size <= 0 {
			return fmt.Errorf("pcapng: buffer size must be positive")
		}
		cfg.bufferSize = size
		return nil
	}
}
