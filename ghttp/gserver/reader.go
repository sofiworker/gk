package gserver

import "bytes"

type bodyReader struct{ *bytes.Reader }

func (br *bodyReader) Close() error { bodyReaderPool.Put(br); return nil }
