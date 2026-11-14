package otel

import (
	"fmt"

	"github.com/sofiworker/gk/gotel"
	"go.opentelemetry.io/otel/attribute"
)

func attributes(values []gotel.KeyValue) []attribute.KeyValue {
	result := make([]attribute.KeyValue, 0, len(values))
	for _, kv := range values {
		key := attribute.Key(kv.Key)
		switch value := kv.Value.(type) {
		case string:
			result = append(result, key.String(value))
		case bool:
			result = append(result, key.Bool(value))
		case int:
			result = append(result, key.Int(value))
		case int32:
			result = append(result, key.Int64(int64(value)))
		case int64:
			result = append(result, key.Int64(value))
		case float32:
			result = append(result, key.Float64(float64(value)))
		case float64:
			result = append(result, key.Float64(value))
		case []string:
			result = append(result, key.StringSlice(value))
		case []bool:
			result = append(result, key.BoolSlice(value))
		case []int:
			result = append(result, key.IntSlice(value))
		case []int64:
			result = append(result, key.Int64Slice(value))
		case []float64:
			result = append(result, key.Float64Slice(value))
		default:
			result = append(result, key.String(fmt.Sprint(value)))
		}
	}
	return result
}
