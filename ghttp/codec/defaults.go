package codec

import "sync"

var (
	defaultManagerOnce sync.Once
	defaultManager     *CodecManager
)

// DefaultManager 返回全局默认的编解码器管理器
func DefaultManager() *CodecManager {
	defaultManagerOnce.Do(func() {
		manager := NewCodecManager()

		jsonCodec := NewJSONCodec()
		xmlCodec := NewXMLCodec()
		yamlCodec := NewYAMLCodec()
		plainCodec := NewPlainCodec()

		manager.RegisterCodec(jsonCodec)
		manager.RegisterCodec(xmlCodec)
		manager.RegisterCodec(yamlCodec)
		manager.RegisterCodec(plainCodec)
		manager.SetDefaultCodec(jsonCodec)

		defaultManager = manager
	})
	return defaultManager
}
