package gserver

import (
	"errors"
	"strings"
	"sync"

	"github.com/sofiworker/gk/gcodec"
)

var (
	ErrAlreadyRegistered = errors.New("codec already registered")
	ErrInvalidCodec      = errors.New("codec is nil")
)

type CodecFactory struct {
	codecs sync.Map
}

func newCodecFactory() *CodecFactory {
	cf := &CodecFactory{}

	// Register default codecs for common content types
	cf.Register("application/json", gcodec.NewJSONCodec())
	cf.Register("application/xml", gcodec.NewXMLCodec())
	cf.Register("text/xml", gcodec.NewXMLCodec())
	cf.Register("application/x-yaml", gcodec.NewYAMLCodec())
	cf.Register("application/yaml", gcodec.NewYAMLCodec())
	cf.Register("text/yaml", gcodec.NewYAMLCodec())

	return cf
}

func (c *CodecFactory) Get(name string) gcodec.Codec {
	key := normalizeContentType(name)
	if v, ok := c.codecs.Load(key); ok {
		return v.(gcodec.Codec)
	}
	return nil
}

func (c *CodecFactory) Register(name string, codec gcodec.Codec) error {
	if codec == nil {
		return ErrInvalidCodec
	}
	key := normalizeContentType(name)
	if _, ok := c.codecs.Load(key); ok {
		return ErrAlreadyRegistered
	}
	c.codecs.Store(key, codec)
	return nil
}

func normalizeContentType(ct string) string {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if idx := strings.IndexAny(ct, ";,"); idx >= 0 {
		ct = ct[:idx]
	}
	return ct
}
