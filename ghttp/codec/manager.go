package codec

import (
	"strings"
	"sync"

	"github.com/sofiworker/gk/gcodec"
)

// Codec 统一的 HTTP 编解码接口，封装字节编解码能力。
type Codec interface {
	Encode(v interface{}) ([]byte, error)
	Decode(data []byte, v interface{}) error
}

type Manager struct {
	mu           sync.RWMutex
	codecs       map[string]Codec
	defaultCodec Codec
}

var (
	defaultManager     *Manager
	defaultManagerOnce sync.Once
)

// DefaultManager 返回带有默认编解码器的全局管理器。
func DefaultManager() *Manager {
	defaultManagerOnce.Do(func() {
		defaultManager = NewManagerWithDefaults()
	})
	return defaultManager
}

// NewManagerWithDefaults 创建一个新的管理器并注册 JSON/XML/YAML/Plain 等默认编解码器。
func NewManagerWithDefaults() *Manager {
	m := NewManager()
	m.RegisterDefaults()
	return m
}

// NewManager 创建空的管理器。
func NewManager() *Manager {
	return &Manager{
		codecs: make(map[string]Codec),
	}
}

// Clone 深拷贝当前管理器及其编解码器注册表。
func (m *Manager) Clone() *Manager {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clone := &Manager{
		codecs:       make(map[string]Codec, len(m.codecs)),
		defaultCodec: m.defaultCodec,
	}
	for k, v := range m.codecs {
		clone.codecs[k] = v
	}
	return clone
}

// Register 注册指定 Content-Type 的编解码器，首次注册的编解码器会被设为默认编解码器。
func (m *Manager) Register(contentType string, c Codec) {
	if c == nil {
		return
	}
	ct := normalizeContentType(contentType)
	if ct == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.codecs[ct] = c
	if m.defaultCodec == nil {
		m.defaultCodec = c
	}
}

// SetDefault 设置默认编解码器。
func (m *Manager) SetDefault(c Codec) {
	if c == nil {
		return
	}
	m.mu.Lock()
	m.defaultCodec = c
	m.mu.Unlock()
}

// GetCodec 根据 Content-Type 获取已注册的编解码器。
func (m *Manager) GetCodec(contentType string) Codec {
	ct := normalizeContentType(contentType)
	if ct == "" {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.codecs[ct]
}

// DefaultCodec 返回默认编解码器。
func (m *Manager) DefaultCodec() Codec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultCodec
}

// RegisterDefaults 注册默认编解码器。
func (m *Manager) RegisterDefaults() {
	m.Register("application/json", wrapBytesCodec(gcodec.NewJSONCodec()))
	m.Register("application/xml", wrapBytesCodec(gcodec.NewXMLCodec()))
	m.Register("text/xml", wrapBytesCodec(gcodec.NewXMLCodec()))
	m.Register("application/x-yaml", wrapBytesCodec(gcodec.NewYAMLCodec()))
	m.Register("application/yaml", wrapBytesCodec(gcodec.NewYAMLCodec()))
	m.Register("text/yaml", wrapBytesCodec(gcodec.NewYAMLCodec()))
	m.Register("text/plain", wrapBytesCodec(gcodec.NewPlainCodec()))
}

// wrapBytesCodec 适配 gcodec.BytesCodec 为 Codec 接口。
func wrapBytesCodec(codec gcodec.BytesCodec) Codec {
	return bytesCodecAdapter{codec: codec}
}

type bytesCodecAdapter struct {
	codec gcodec.BytesCodec
}

func (a bytesCodecAdapter) Encode(v interface{}) ([]byte, error) {
	return a.codec.EncodeBytes(v)
}

func (a bytesCodecAdapter) Decode(data []byte, v interface{}) error {
	return a.codec.DecodeBytes(data, v)
}

func normalizeContentType(ct string) string {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if idx := strings.IndexAny(ct, ";,"); idx >= 0 {
		ct = ct[:idx]
	}
	return ct
}
