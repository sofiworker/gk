package gotel

import (
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// convertAttributes 将通用 KeyValue 转换为 OpenTelemetry 的 attribute.KeyValue
func convertAttributes(attrs []KeyValue) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}

	result := make([]attribute.KeyValue, len(attrs))
	for i, attr := range attrs {
		result[i] = convertAttribute(attr)
	}
	return result
}

// convertAttribute 转换单个属性
func convertAttribute(attr KeyValue) attribute.KeyValue {
	switch v := attr.Value.(type) {
	case string:
		return attribute.String(attr.Key, v)
	case int:
		return attribute.Int(attr.Key, v)
	case int64:
		return attribute.Int64(attr.Key, v)
	case float32:
		return attribute.Float64(attr.Key, float64(v))
	case float64:
		return attribute.Float64(attr.Key, v)
	case bool:
		return attribute.Bool(attr.Key, v)
	case []string:
		return attribute.StringSlice(attr.Key, v)
	case []int:
		return attribute.IntSlice(attr.Key, v)
	case []int64:
		return attribute.Int64Slice(attr.Key, v)
	case []float64:
		return attribute.Float64Slice(attr.Key, v)
	case []bool:
		return attribute.BoolSlice(attr.Key, v)
	default:
		// 对于不支持的类型，转换为字符串
		return attribute.String(attr.Key, toString(v))
	}
}

// toString 将任意值转换为字符串
func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return "null"
	default:
		return "{{unsupported_type}}"
	}
}

// convertStatusCode 转换状态码
func convertStatusCode(code StatusCode) codes.Code {
	switch code {
	case StatusCodeOk:
		return codes.Ok
	case StatusCodeError:
		return codes.Error
	default:
		return codes.Unset
	}
}
