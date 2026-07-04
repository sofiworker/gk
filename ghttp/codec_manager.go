package ghttp

import (
	"mime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// maxNegotiateCacheEntries bounds the Accept-negotiation cache. Accept values
// are client-controlled, so the cache must not grow without limit; once full,
// unseen values are still negotiated correctly, just without being cached.
const maxNegotiateCacheEntries = 256

// CodecManager manages Codecs by Content-Type.
type CodecManager struct {
	mu        sync.RWMutex
	codecs    map[string]Codec
	defaultCT string

	negCache     sync.Map // Accept header value -> Codec
	negCacheSize atomic.Int32
}

// NewCodecManager creates a CodecManager with default codecs registered.
func NewCodecManager() *CodecManager {
	m := &CodecManager{
		codecs:    make(map[string]Codec, 8),
		defaultCT: "application/json",
	}
	m.mustRegister(&JSONCodec{})
	m.mustRegister(&XMLCodec{})
	m.mustRegister(&PlainCodec{})
	m.mustRegister(&FormCodec{})
	return m
}

// Register registers a Codec for all its ContentTypes.
func (m *CodecManager) Register(codec Codec) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ct := range codec.ContentTypes() {
		m.codecs[ct] = codec
	}
	m.invalidateNegotiateCache()
	return nil
}

func (m *CodecManager) mustRegister(codec Codec) {
	for _, ct := range codec.ContentTypes() {
		m.codecs[ct] = codec
	}
}

// invalidateNegotiateCache drops all cached negotiation results; must be
// called whenever the codec set changes.
func (m *CodecManager) invalidateNegotiateCache() {
	m.negCache.Range(func(key, _ interface{}) bool {
		m.negCache.Delete(key)
		return true
	})
	m.negCacheSize.Store(0)
}

// Resolve finds a Codec by Content-Type.
func (m *CodecManager) Resolve(contentType string) (Codec, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}

	codec, ok := m.codecs[mediaType]
	return codec, ok
}

// Negotiate selects the best Codec based on the Accept header. Results are
// cached per Accept value: real-world traffic carries very few distinct
// values, so repeated requests skip the parse/sort entirely.
func (m *CodecManager) Negotiate(accept string) Codec {
	if accept == "" || accept == "*/*" {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.codecs[m.defaultCT]
	}

	if cached, ok := m.negCache.Load(accept); ok {
		return cached.(Codec)
	}

	codec := m.negotiate(accept)
	if codec != nil && m.negCacheSize.Load() < maxNegotiateCacheEntries {
		if _, loaded := m.negCache.LoadOrStore(accept, codec); !loaded {
			m.negCacheSize.Add(1)
		}
	}
	return codec
}

func (m *CodecManager) negotiate(accept string) Codec {
	types := strings.Split(accept, ",")
	type acceptItem struct {
		ct      string
		quality float64
	}
	var items []acceptItem

	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		mediaType, params, _ := mime.ParseMediaType(t)
		q := 1.0
		if params != nil {
			if qs, ok := params["q"]; ok {
				if f, err := strconv.ParseFloat(qs, 64); err == nil {
					q = f
				}
			}
		}
		items = append(items, acceptItem{ct: mediaType, quality: q})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].quality > items[j].quality
	})

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, item := range items {
		if codec, ok := m.codecs[item.ct]; ok {
			return codec
		}
	}

	return m.codecs[m.defaultCT]
}
