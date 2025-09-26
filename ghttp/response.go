package ghttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/valyala/fasthttp"
)

type UnifiedResponse interface {
	GetData() interface{}
	GetCode() int
	GetMessage() string
	IsSuccess() bool
}

// StandardResponse 标准统一响应体结构示例
type StandardResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func (s *StandardResponse) GetData() interface{} {
	return s.Data
}

func (s *StandardResponse) GetCode() int {
	return s.Code
}

func (s *StandardResponse) GetMessage() string {
	return s.Message
}

func (s *StandardResponse) IsSuccess() bool {
	return s.Code == 0 // 根据实际业务定义成功条件
}

type Response struct {
	Request *Request
}

func (r *Response) SetCommonBody(body interface{}) *Response {
	r.commonBody = body
	return r
}

func (r *Response) SetDecoder(d Decoder) *Response {
	r.decoder = d
	return r
}

func (r *Response) Unmarshal(v interface{}) error {
	return nil
}

func (r *Response) PrintRawRespWithWriter(w io.Writer) error {
	_, err := w.Write(r.BodyRaw)
	if err != nil {
		return err
	}
	return nil
}

func (r *Response) PrintRawResp() error {
	err := r.PrintRawRespWithWriter(os.Stdout)
	if err != nil {
		return err
	}
	return nil
}

func (r *Response) Decode(v interface{}) error {
	if reflect.TypeOf(v).Kind() != reflect.Ptr {
		return ErrDataFormat
	}
	buffer := bytes.NewBuffer(r.BodyRaw)
	return r.decoder.Decode(buffer, v)
}

// response.go 中修改 UnifiedResponseDecode 方法

func (r *Response) UnifiedResponseDecode(template interface{}, target interface{}) error {
	if template == nil {
		return fmt.Errorf("unified response template is nil")
	}

	if target != nil {
		if reflect.TypeOf(target).Kind() != reflect.Ptr {
			return ErrDataFormat
		}
	}

	// 创建统一响应体实例
	templateType := reflect.TypeOf(template)
	if templateType.Kind() == reflect.Ptr {
		templateType = templateType.Elem()
	}
	unifiedResp := reflect.New(templateType).Interface()

	// 使用响应中的解码器或者默认JSON解码器
	var decoder Decoder
	if r.decoder != nil {
		decoder = r.decoder
	} else {
		// 默认使用JSON解码器
		decoder = NewJsonDecoder()
	}

	// 使用指定解码器解码统一响应体
	buffer := bytes.NewBuffer(r.BodyRaw)
	err := decoder.Decode(buffer, unifiedResp)
	if err != nil {
		return err
	}

	// 提取实际数据
	var extractedData interface{}
	respValue := reflect.ValueOf(unifiedResp)
	if respValue.Kind() == reflect.Ptr {
		respValue = respValue.Elem()
	}

	// 尝试调用GetData方法
	getDataMethod := respValue.MethodByName("GetData")
	if getDataMethod.IsValid() {
		results := getDataMethod.Call(nil)
		if len(results) > 0 {
			extractedData = results[0].Interface()
		}
	} else {
		// 如果没有GetData方法，尝试直接访问Data字段
		dataField := respValue.FieldByName("Data")
		if dataField.IsValid() {
			extractedData = dataField.Interface()
		}
	}

	// 如果有目标结构体，将提取的数据填充进去
	if target != nil && extractedData != nil {
		// 对于复杂类型，我们仍然使用JSON来序列化和反序列化
		// 因为提取的数据可能已经是解析后的Go类型
		dataBytes, err := json.Marshal(extractedData)
		if err != nil {
			return err
		}
		return json.Unmarshal(dataBytes, target)
	}

	return nil
}

// IsUnifiedResponseSuccess 检查统一响应体是否表示成功
func (r *Response) IsUnifiedResponseSuccess(template interface{}) (bool, error) {
	if template == nil {
		return false, fmt.Errorf("unified response template is nil")
	}

	// 创建统一响应体实例
	templateType := reflect.TypeOf(template)
	if templateType.Kind() == reflect.Ptr {
		templateType = templateType.Elem()
	}
	unifiedResp := reflect.New(templateType).Interface()

	// 解码统一响应体
	buffer := bytes.NewBuffer(r.BodyRaw)
	err := json.NewDecoder(buffer).Decode(unifiedResp)
	if err != nil {
		return false, err
	}

	// 检查是否成功
	respValue := reflect.ValueOf(unifiedResp)
	if respValue.Kind() == reflect.Ptr {
		respValue = respValue.Elem()
	}

	// 尝试调用IsSuccess方法
	isSuccessMethod := respValue.MethodByName("IsSuccess")
	if isSuccessMethod.IsValid() {
		results := isSuccessMethod.Call(nil)
		if len(results) > 0 {
			if success, ok := results[0].Interface().(bool); ok {
				return success, nil
			}
		}
	}

	// 如果没有IsSuccess方法，尝试检查Code字段
	codeField := respValue.FieldByName("Code")
	if codeField.IsValid() {
		// 默认认为Code为0时表示成功，可以根据实际需求调整
		return codeField.Int() == 0, nil
	}

	return false, fmt.Errorf("cannot determine success status")
}

// UnifiedResponseDecodeWithDecoder 使用指定解码器解码统一响应体
func (r *Response) UnifiedResponseDecodeWithDecoder(template interface{}, target interface{}, decoder Decoder) error {
	if template == nil {
		return fmt.Errorf("unified response template is nil")
	}

	if target != nil {
		if reflect.TypeOf(target).Kind() != reflect.Ptr {
			return ErrDataFormat
		}
	}

	// 临时保存原始解码器
	originalDecoder := r.decoder
	// 设置指定的解码器
	r.decoder = decoder
	// 确保在函数结束时恢复原始解码器
	defer func() {
		r.decoder = originalDecoder
	}()

	// 复用 UnifiedResponseDecode 的逻辑
	return r.UnifiedResponseDecode(template, target)
}

// SetUnifiedResponseTemplate 设置统一响应体模板，用于特定的响应处理
func (r *Response) SetUnifiedResponseTemplate(template interface{}) {
	r.commonBody = template
}

// DecodeUnifiedResponse 使用响应中设置的模板解码统一响应体
func (r *Response) DecodeUnifiedResponse(target interface{}) error {
	if r.commonBody == nil {
		return fmt.Errorf("unified response template not set")
	}

	return r.UnifiedResponseDecode(r.commonBody, target)
}

func (r *Response) Close() {
	if r.fResp != nil {
		fasthttp.ReleaseResponse(r.fResp)
	}
}

func (r *Response) Dump() string {
	var buf bytes.Buffer
	r.DumpWriter(&buf)
	return buf.String()
}

func (r *Response) DumpWriter(w io.Writer) {
}
