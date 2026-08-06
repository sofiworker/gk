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

// Select negotiates the best registered codec from candidates using RFC 9110
// Accept semantics: q=0 excludes a media range, wildcards match, equal
// qualities keep declaration order. ok=false means no candidate is acceptable.
func (m *CodecManager) Select(accept string, candidates []string) (string, Codec, bool) {
	if len(candidates) == 0 {
		return "", nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if strings.TrimSpace(accept) == "" || strings.TrimSpace(accept) == "*/*" {
		codec, ok := m.codecs[candidates[0]]
		return candidates[0], codec, ok
	}
	for _, item := range parseAcceptItems(accept) {
		for _, candidate := range candidates {
			if acceptMediaTypeMatches(item.contentType, candidate) {
				codec, ok := m.codecs[candidate]
				return candidate, codec, ok
			}
		}
	}
	return "", nil, false
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
	items := parseAcceptItems(accept)
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, item := range items {
		if codec, ok := m.codecs[item.contentType]; ok {
			return codec
		}
	}

	return m.codecs[m.defaultCT]
}

type acceptItem struct {
	contentType string
	quality     float64
	index       int
}

var (
	acceptParseCache sync.Map
	acceptParseSize  atomic.Int32
)

const maxAcceptParseCacheEntries = 256

func parseAcceptItems(accept string) []acceptItem {
	accept = strings.TrimSpace(accept)
	if accept == "" {
		return nil
	}
	if cached, ok := acceptParseCache.Load(accept); ok {
		return cached.([]acceptItem)
	}
	items := parseAcceptItemsUncached(accept)
	if acceptParseSize.Load() < maxAcceptParseCacheEntries {
		if _, loaded := acceptParseCache.LoadOrStore(accept, items); !loaded {
			acceptParseSize.Add(1)
		}
	}
	return items
}

func parseAcceptItemsUncached(accept string) []acceptItem {
	parts := strings.Split(accept, ",")
	items := make([]acceptItem, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		mediaType, params, err := mime.ParseMediaType(part)
		if err != nil {
			mediaType = normalizeContentType(part)
		}
		if mediaType == "" {
			continue
		}
		quality := 1.0
		if params != nil {
			if q, ok := params["q"]; ok {
				if parsed, err := strconv.ParseFloat(q, 64); err == nil {
					quality = parsed
				}
			}
		}
		if quality <= 0 {
			continue
		}
		items = append(items, acceptItem{contentType: strings.ToLower(mediaType), quality: quality, index: i})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].quality == items[j].quality {
			return items[i].index < items[j].index
		}
		return items[i].quality > items[j].quality
	})
	return items
}

func acceptMediaTypeMatches(acceptMedia, candidate string) bool {
	if acceptMedia == "*/*" {
		return true
	}
	if strings.HasSuffix(acceptMedia, "/*") {
		return strings.HasPrefix(candidate, strings.TrimSuffix(acceptMedia, "*"))
	}
	return acceptMedia == candidate
}
