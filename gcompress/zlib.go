package gcompress

import (
	"bytes"
	"compress/zlib"
	"io"
)

// ZlibCompress 使用 zlib 压缩数据。
func ZlibCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ZlibDecompress 解压 zlib 数据。
func ZlibDecompress(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func ZlibCompressString(s string) ([]byte, error) {
	return ZlibCompress([]byte(s))
}

func ZlibDecompressToString(data []byte) (string, error) {
	out, err := ZlibDecompress(data)
	return string(out), err
}
