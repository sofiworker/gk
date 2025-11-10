package gserver

import "sync"

type CodecFactory struct {
	codecs sync.Map
}

func newCodecFactory() *CodecFactory {
	return &CodecFactory{}
}
