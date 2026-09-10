package ghttp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
)

// OutputSpec 是输出契约:注册期固定格式/状态码,encode 在请求期写响应。(Out, error)
// 不默认 JSON、不默认 200,格式与状态码必须显式声明。
// OutputSpec is the output contract: format/status fixed at registration, encode
// writes the response at request time. (Out, error) defaults to neither JSON nor
// 200; format and status must be declared explicitly.
type OutputSpec[T any] interface {
	encode(resp *Response, v T) error
}

// outputDoc 是一个输出契约的注册期元数据(状态码、Content-Type、是否有响应体),供
// OpenAPI 生成描述 responses。它是纯数据快照,不持有 encoder 或任何运行期对象。
// outputDoc is an output contract's registration-time metadata (status,
// Content-Type, whether a body exists) used by OpenAPI generation to describe
// responses. It is a pure data snapshot holding no encoder or runtime object.
type outputDoc struct {
	status      int
	contentType string
	hasBody     bool
	typ         reflect.Type
}

// outputDescriber 由输出契约实现以自述其响应元数据。包内密封:仅内置契约实现它,
// 用户自定义契约不实现时 OpenAPI 退化为"200 + 无 schema",不影响运行。
// outputDescriber is implemented by output contracts to describe their own
// response metadata. Package-sealed: only built-in contracts implement it, and a
// user contract that does not simply degrades OpenAPI to "200 without schema"
// without affecting runtime behavior.
type outputDescriber interface {
	describeOutput() outputDoc
}

// outputDocOf 读取一个输出契约的响应元数据;未实现 outputDescriber 时返回 200 兜底。
// outputDocOf reads an output contract's response metadata, falling back to a
// plain 200 when outputDescriber is not implemented.
func outputDocOf[T any](out OutputSpec[T]) outputDoc {
	if d, ok := out.(outputDescriber); ok {
		return d.describeOutput()
	}
	return outputDoc{status: http.StatusOK, hasBody: true, typ: reflect.TypeOf((*T)(nil)).Elem()}
}

// jsonOutput 以 JSON 编码输出 T,状态码可经 Status 覆盖(默认 200)。默认用内置 JSONCodec
// 编码,也可经 WithEncoder 替换为任意 ResponseEncoder(自定义 JSON 库、XML、模板等)。
// jsonOutput encodes T as JSON; status overridable via Status (default 200).
// Encodes with the built-in JSONCodec by default, or any ResponseEncoder via
// WithEncoder (custom JSON lib, XML, templates, etc.).
type jsonOutput[T any] struct {
	status int
	enc    ResponseEncoder // nil 表示用内置 JSONCodec;非 nil 则用该编码器。
}

// Status 覆盖该输出的状态码,返回新值(值语义,便于链式:JSON[T]().Status(201))。
// Status overrides this output's status code, returning the new value (value
// semantics for chaining: JSON[T]().Status(201)).
func (o jsonOutput[T]) Status(code int) jsonOutput[T] {
	o.status = code
	return o
}

// WithEncoder 用自定义编码器替换内置 JSON 编码,返回新值。只换编码器、状态码不变。
// WithEncoder replaces the built-in JSON encoding with a custom encoder,
// returning the new value; only the encoder changes, status stays.
func (o jsonOutput[T]) WithEncoder(enc ResponseEncoder) jsonOutput[T] {
	o.enc = enc
	return o
}

// jsonEncodeBufPool 池化默认 JSON 输出的编码缓冲:encode 每请求都需要"先缓冲后提交"
// (见 encode 内注释),裸 bytes.Buffer + json.NewEncoder 是 typed 命中热路径上最大的
// 单项分配(基准中占 GetParamsSmall 约 2 alloc/40% 字节)。池化后稳态零新增分配。
// 超大响应用后不回池(回池会把峰值容量永久驻留)。
// jsonEncodeBufPool pools the default JSON output's encode buffer: encode must
// buffer-then-commit on every request (see the comment inside encode), and a bare
// bytes.Buffer + json.NewEncoder was the largest single allocation on the typed
// hit path (~2 allocs/40% of bytes in GetParamsSmall). Pooled, the steady state
// allocates nothing new. Oversized buffers are not returned (returning one would
// pin its peak capacity forever).
var jsonEncodeBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// maxPooledEncodeBuf 是编码缓冲回池的容量上限(64KiB):常规 JSON 响应远小于它,
// 偶发的超大响应不应把大块内存长期锁进池里。
// maxPooledEncodeBuf caps the capacity a buffer may have to re-enter the pool
// (64 KiB): normal JSON responses are far smaller, and an occasional huge response
// must not pin a large block in the pool indefinitely.
const maxPooledEncodeBuf = 64 << 10

func (o jsonOutput[T]) encode(resp *Response, v T) error {
	status := o.status
	if status == 0 {
		status = http.StatusOK
	}
	if o.enc != nil {
		// 自定义 encoder:用其声明的 Content-Type(若尚未被设置),避免输出丢失
		// 正确的 Content-Type(此前该分支从不设置,XML 等自定义输出被误当默认类型)。
		// Custom encoder: set its declared Content-Type (unless already set) so
		// the output does not lose the correct Content-Type (this branch never
		// set it before, mislabeling XML and other custom outputs).
		if resp.Header().Get("Content-Type") == "" {
			if ct := o.enc.ContentType(); ct != "" {
				resp.Header().Set("Content-Type", ct)
			}
		}
		resp.WriteHeader(status)
		return o.enc.Encode(resp, v)
	}
	// 默认路径必须先编码进缓冲再提交:编码失败时头尚未写出,错误链仍能回规范的 500;
	// 直接流式写 resp 会在失败时留下"已提交 status + 半截 JSON"。缓冲取自池,见
	// jsonEncodeBufPool。
	// The default path must encode into a buffer before committing: on an encode
	// failure the header is not yet out and the error chain can still send a
	// canonical 500, whereas streaming straight into resp would leave "committed
	// status + half a JSON body". The buffer comes from the pool; see
	// jsonEncodeBufPool.
	buf := jsonEncodeBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		jsonEncodeBufPool.Put(buf)
		return err
	}
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	resp.WriteHeader(status)
	_, err := resp.Write(buf.Bytes())
	if buf.Cap() <= maxPooledEncodeBuf {
		jsonEncodeBufPool.Put(buf)
	}
	return err
}

// JSON 返回一个 JSON 输出契约,默认状态码 200,可用 .Status(code) 覆盖、.WithEncoder 替换编码器。
// JSON returns a JSON output contract, default status 200, overridable with
// .Status(code) and .WithEncoder(enc).
func JSON[T any]() jsonOutput[T] { return jsonOutput[T]{} }

// describeOutput 报告本契约的响应元数据:状态码(默认 200)、实际写出的 Content-Type
// (自定义 encoder 时取其声明)与响应体类型。
// describeOutput reports this contract's response metadata: status (default 200),
// the Content-Type actually written (the custom encoder's declaration when set),
// and the body type.
func (o jsonOutput[T]) describeOutput() outputDoc {
	status := o.status
	if status == 0 {
		status = http.StatusOK
	}
	ct := "application/json"
	if o.enc != nil {
		ct = o.enc.ContentType()
	}
	return outputDoc{status: status, contentType: ct, hasBody: true, typ: reflect.TypeOf((*T)(nil)).Elem()}
}

// noContent 是无响应体输出:写状态码(默认 204),不写 body。O 为占位类型。
// noContent is a body-less output: writes the status (default 204), no body. O
// is a placeholder type.
type noContent[T any] struct{ status int }

func (o noContent[T]) Status(code int) noContent[T] {
	o.status = code
	return o
}

func (o noContent[T]) encode(resp *Response, _ T) error {
	status := o.status
	if status == 0 {
		status = http.StatusNoContent
	}
	resp.WriteHeader(status)
	return nil
}

// NoContent 返回一个无响应体输出契约(默认 204)。业务函数仍需返回一个 O 值(通常用
// struct{} 之类的占位类型),但不会被写出。
// NoContent returns a body-less output contract (default 204). The business
// function still returns an O value (typically a placeholder like struct{}),
// which is not written.
func NoContent[T any]() noContent[T] { return noContent[T]{} }

// describeOutput 报告本契约的响应元数据:状态码(默认 204)且无响应体。
// describeOutput reports this contract's response metadata: the status (default
// 204) and no body.
func (o noContent[T]) describeOutput() outputDoc {
	status := o.status
	if status == 0 {
		status = http.StatusNoContent
	}
	return outputDoc{status: status, hasBody: false}
}
