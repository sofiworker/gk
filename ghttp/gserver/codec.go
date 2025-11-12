package gserver

import (
	"errors"
	"sync"

	"github.com/sofiworker/gk/gcodec"
)

var (
	ErrAlreadyRegistered = errors.New("codec already registered")
)

type CodecFactory struct {
	codecs sync.Map
}

func newCodecFactory() *CodecFactory {
	return &CodecFactory{}
}

func (c *CodecFactory) Get(name string) gcodec.Codec {
	if v, ok := c.codecs.Load(name); ok {
		return v.(gcodec.Codec)
	}
	return nil
}

func (c *CodecFactory) Register(name string, codec gcodec.Codec) error {
	if _, ok := c.codecs.Load(name); ok {
		return ErrAlreadyRegistered
	}
	c.codecs.Store(name, codec)
	return nil
}
