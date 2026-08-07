package gcompress

import (
	"bytes"
	"compress/flate"
	"io"
)

// FlateCompress 使用 DEFLATE 压缩数据。
func FlateCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestCompression)
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

// FlateDecompress 解压 DEFLATE 数据。
func FlateDecompress(data []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()
	return io.ReadAll(r)
}

func FlateCompressString(s string) ([]byte, error) {
	return FlateCompress([]byte(s))
}

func FlateDecompressToString(data []byte) (string, error) {
	out, err := FlateDecompress(data)
	return string(out), err
}
