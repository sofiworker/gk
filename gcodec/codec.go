package gcodec

import (
	"io"
)

// StreamEncoder defines the streaming encoding interface.
type StreamEncoder interface {
	Encode(w io.Writer, v interface{}) error
}

// StreamDecoder defines the streaming decoding interface.
type StreamDecoder interface {
	Decode(r io.Reader, v interface{}) error
}

// BytesEncoder defines the byte-slice encoding interface.
type BytesEncoder interface {
	EncodeBytes(v interface{}) ([]byte, error)
}

// BytesDecoder defines the byte-slice decoding interface.
type BytesDecoder interface {
	DecodeBytes(data []byte, v interface{}) error
}

// StreamCodec combines streaming encoding and decoding.
type StreamCodec interface {
	StreamEncoder
	StreamDecoder
}

// BytesCodec combines byte-slice encoding and decoding.
type BytesCodec interface {
	BytesEncoder
	BytesDecoder
}

// Codec is the master interface that groups all encoding and decoding capabilities,
// both streaming and byte-slice based.
type Codec interface {
	StreamCodec
	BytesCodec
}
