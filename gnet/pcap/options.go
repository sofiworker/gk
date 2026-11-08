package pcap

import (
	"encoding/binary"
	"fmt"
	"time"
)

type WriterOption func(*writerConfig) error

type writerConfig struct {
	format                     Format
	byteOrder                  binary.ByteOrder
	resolution                 time.Duration
	versionMaj, versionMin     uint16
	thisZone                   int32
	sigFigs, snapLen, network  uint32
	bufferSize                 int
	sectionMajor, sectionMinor uint16
	sectionLength              int64
	sectionOpts                []Option
}

// WithFormat selects the capture file format. The default is classic PCAP.
func WithFormat(format Format) WriterOption {
	return func(cfg *writerConfig) error {
		switch format {
		case FormatPCAP, FormatPCAPNG:
			cfg.format = format
			return nil
		default:
			return fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
		}
	}
}

func WithSnapLen(snapLen uint32) WriterOption {
	return func(cfg *writerConfig) error {
		if snapLen == 0 {
			return fmt.Errorf("pcap: snap length must be positive")
		}
		cfg.snapLen = snapLen
		return nil
	}
}

func WithLinkType(linkType uint32) WriterOption {
	return func(cfg *writerConfig) error {
		cfg.network = linkType
		return nil
	}
}

// WithBuffer 启用带缓冲写入以减少系统调用。
func WithBuffer(size int) WriterOption {
	return func(cfg *writerConfig) error {
		if size <= 0 {
			return fmt.Errorf("pcap: buffer size must be positive")
		}
		cfg.bufferSize = size
		return nil
	}
}

func WithByteOrder(order binary.ByteOrder) WriterOption {
	return func(cfg *writerConfig) error {
		if order != binary.BigEndian && order != binary.LittleEndian {
			return fmt.Errorf("pcap: unsupported byte order")
		}
		cfg.byteOrder = order
		return nil
	}
}

func WithTimestampResolution(resolution time.Duration) WriterOption {
	return func(cfg *writerConfig) error {
		switch resolution {
		case time.Microsecond, time.Nanosecond:
			cfg.resolution = resolution
			return nil
		default:
			return fmt.Errorf("pcap: unsupported timestamp resolution %s", resolution)
		}
	}
}

func WithVersion(major, minor uint16) WriterOption {
	return func(cfg *writerConfig) error {
		if major == 0 {
			return fmt.Errorf("pcap: version major must be positive")
		}
		cfg.versionMaj = major
		cfg.versionMin = minor
		return nil
	}
}

func WithTimeZone(zone int32) WriterOption {
	return func(cfg *writerConfig) error {
		cfg.thisZone = zone
		return nil
	}
}

func WithSigFigs(sig uint32) WriterOption {
	return func(cfg *writerConfig) error {
		cfg.sigFigs = sig
		return nil
	}
}

func WithSectionVersion(major, minor uint16) WriterOption {
	return func(cfg *writerConfig) error {
		if major == 0 {
			return fmt.Errorf("pcapng: section version major must be positive")
		}
		cfg.sectionMajor = major
		cfg.sectionMinor = minor
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

// WithDefaultTimestampResolution is an alias for WithTimestampResolution.
func WithDefaultTimestampResolution(res time.Duration) WriterOption {
	return WithTimestampResolution(res)
}
