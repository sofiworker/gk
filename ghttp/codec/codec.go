package codec

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
	cm.codecs = append(cm.codecs, codec)
}

// SetDefaultCodec 设置默认编解码器
func (cm *CodecManager) SetDefaultCodec(codec Codec) {
	cm.defaultCodec = codec
}

// GetCodec 根据content type获取合适的编解码器
func (cm *CodecManager) GetCodec(contentType string) Codec {
	// 首先查找精确匹配的编解码器
	for _, codec := range cm.codecs {
		if codec.Supports(contentType) {
			return codec
		}
	}

	// 如果没有找到匹配的，返回默认编解码器
	return cm.defaultCodec
}
