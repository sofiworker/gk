package ghttp

// renderFormat 表示 Render[T] 的序列化格式策略。
// renderFormat is the serialization-format strategy of Render[T].
type renderFormat uint8

const (
	// renderAuto 未显式声明:走 Accept 协商(零值,等价裸 Resp)。
	// renderAuto is the zero value: negotiate by Accept, like a bare Resp.
	renderAuto renderFormat = iota
	// renderJSON 强制 JSON(goccy)。
	// renderJSON forces JSON (goccy).
	renderJSON
	// renderXML 强制 XML。
	// renderXML forces XML.
	renderXML
	// renderRaw 原始字节直出,不序列化。
	// renderRaw writes raw bytes verbatim, no serialization.
	renderRaw
)

// Render 是响应包装:显式声明序列化格式。
// Render is a response wrapper declaring an explicit serialization format.
// 零值等价于返回裸 Data(走 Accept 协商)。RenderJSON/RenderXML/RenderBytes
// 声明显式格式;框架识别 Render[T] 后解包 Data 再序列化,StatusCoder /
// ResponseHeaderWriter / Envelope / OpenAPI 都作用于解包后的 Data。
// The zero value behaves like a bare Resp (Accept negotiation). RenderJSON /
// RenderXML / RenderBytes declare an explicit format; the framework unwraps
// Data before serializing, and StatusCoder / ResponseHeaderWriter / Envelope /
// OpenAPI all apply to the unwrapped Data.
type Render[T any] struct {
	Data   T
	format renderFormat
	ct     string // 仅 renderRaw 需要；used only by renderRaw.
}

// RenderJSON 声明响应按 JSON 序列化(goccy)。
// RenderJSON declares the response serializes as JSON (goccy).
func RenderJSON[T any](data T) Render[T] { return Render[T]{Data: data, format: renderJSON} }

// RenderXML 声明响应按 XML 序列化。
// RenderXML declares the response serializes as XML.
func RenderXML[T any](data T) Render[T] { return Render[T]{Data: data, format: renderXML} }

// RenderBytes 声明响应为原始字节直出,contentType 指定 Content-Type,不序列化。
// RenderBytes declares the response as raw bytes written verbatim with the
// given Content-Type; no serialization occurs.
func RenderBytes(data []byte, contentType string) Render[[]byte] {
	return Render[[]byte]{Data: data, format: renderRaw, ct: contentType}
}

// renderUnwrapper 让 writeTypedResponse 解包 Render[T](类型擦除)。
// renderUnwrapper lets writeTypedResponse unwrap Render[T] (type-erased).
type renderUnwrapper interface {
	unwrapRender() (any, renderFormat, string)
}

func (r Render[T]) unwrapRender() (any, renderFormat, string) {
	return r.Data, r.format, r.ct
}
