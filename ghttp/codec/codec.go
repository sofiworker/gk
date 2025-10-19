package codec

import "sync"

type Decode interface {
	Decode(data []byte, v interface{}) error
}

type Encode interface {
	Encode(v interface{}) ([]byte, error)
}

// Codec 编解码器接口
type Codec interface {
	// Encode 将数据编码为字节流
	Encode

	// Decode 将字节流解码为数据
	Decode

	// ContentType 返回该编解码器对应的content type
	ContentType() string

	// Supports 检查是否支持指定的content type
	Supports(contentType string) bool
}

// CodecManager 编解码器管理器
type CodecManager struct {
	mu           sync.RWMutex
	codecs       []Codec
	defaultCodec Codec
}

// NewCodecManager 创建新的编解码器管理器
func NewCodecManager() *CodecManager {
	return &CodecManager{
		codecs: make([]Codec, 0),
	}
}

// RegisterCodec 注册编解码器
func (cm *CodecManager) RegisterCodec(codec Codec) {
	if codec == nil {
		return
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 避免重复注册同一个实例
	for _, existing := range cm.codecs {
		if existing == codec {
			return
		}
	}

	cm.codecs = append(cm.codecs, codec)
	if cm.defaultCodec == nil {
		cm.defaultCodec = codec
	}
}

// SetDefaultCodec 设置默认编解码器
func (cm *CodecManager) SetDefaultCodec(codec Codec) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.defaultCodec = codec
}

// DefaultCodec 返回当前默认编解码器
func (cm *CodecManager) DefaultCodec() Codec {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.defaultCodec
}

// GetCodec 根据content type获取合适的编解码器
func (cm *CodecManager) GetCodec(contentType string) Codec {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if contentType != "" {
		for _, codec := range cm.codecs {
			if codec.Supports(contentType) {
				return codec
			}
		}
	}

	return cm.defaultCodec
}

// List 返回当前已注册的编解码器列表（拷贝）
func (cm *CodecManager) List() []Codec {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	out := make([]Codec, len(cm.codecs))
	copy(out, cm.codecs)
	return out
}
