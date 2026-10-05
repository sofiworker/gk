package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http/httptest"
	"testing"
)

// isNaN 检查浮点数是否为 NaN
// isNaN checks if a float is NaN
func isNaN(f float64) bool {
	return math.IsNaN(f)
}

// FuzzValueInt64 对 Value.Int64() 进行模糊测试
// FuzzValueInt64 performs fuzz testing on Value.Int64()
func FuzzValueInt64(f *testing.F) {
	// 添加种子语料
	// Add seed corpus
	f.Add("0")
	f.Add("42")
	f.Add("-100")
	f.Add("9223372036854775807")  // max int64
	f.Add("-9223372036854775808") // min int64
	f.Add("")
	f.Add("abc")
	f.Add("123.45")
	f.Add("  42  ")

	f.Fuzz(func(t *testing.T, input string) {
		v := newValue(input, true)
		result, err := v.Int64()

		// 验证结果一致性：如果没有错误，再次调用应返回相同结果
		// Verify result consistency: if no error, calling again should return same result
		if err == nil {
			result2, err2 := v.Int64()
			if err2 != nil {
				t.Errorf("second call failed but first succeeded")
			}
			if result != result2 {
				t.Errorf("inconsistent results: %d vs %d", result, result2)
			}
		}

		// 验证 IntOr 的一致性
		// Verify IntOr consistency
		defaultVal := int64(999)
		orResult := v.Int64Or(defaultVal)
		if err == nil && orResult != result {
			t.Errorf("Int64Or returned %d, but Int64 returned %d", orResult, result)
		}
		if err != nil && orResult != defaultVal {
			t.Errorf("Int64Or should return default %d on error, got %d", defaultVal, orResult)
		}
	})
}

// FuzzValueBool 对 Value.Bool() 进行模糊测试
// FuzzValueBool performs fuzz testing on Value.Bool()
func FuzzValueBool(f *testing.F) {
	// 添加种子语料
	// Add seed corpus
	f.Add("true")
	f.Add("false")
	f.Add("1")
	f.Add("0")
	f.Add("t")
	f.Add("f")
	f.Add("TRUE")
	f.Add("FALSE")
	f.Add("")
	f.Add("yes")
	f.Add("no")
	f.Add("abc")

	f.Fuzz(func(t *testing.T, input string) {
		v := newValue(input, true)
		result, err := v.Bool()

		// 空字符串应返回 false 且无错误
		// Empty string should return false with no error
		if input == "" && v.Exists() {
			if err != nil {
				t.Errorf("empty string should not error, got: %v", err)
			}
			if result != false {
				t.Errorf("empty string should return false, got: %v", result)
			}
		}

		// 验证 BoolOr 的一致性
		// Verify BoolOr consistency
		defaultVal := true
		orResult := v.BoolOr(defaultVal)
		if err == nil && orResult != result {
			t.Errorf("BoolOr returned %v, but Bool returned %v", orResult, result)
		}
		if err != nil && orResult != defaultVal {
			t.Errorf("BoolOr should return default %v on error, got %v", defaultVal, orResult)
		}
	})
}

// FuzzValueFloat64 对 Value.Float64() 进行模糊测试
// FuzzValueFloat64 performs fuzz testing on Value.Float64()
func FuzzValueFloat64(f *testing.F) {
	// 添加种子语料
	// Add seed corpus
	f.Add("0.0")
	f.Add("3.14")
	f.Add("-2.5")
	f.Add("1.7976931348623157e+308") // max float64
	f.Add("2.2250738585072014e-308") // min positive float64
	f.Add("")
	f.Add("abc")
	f.Add("1e10")
	f.Add("NaN")
	f.Add("Inf")

	f.Fuzz(func(t *testing.T, input string) {
		v := newValue(input, true)
		result, err := v.Float64()

		// 验证结果一致性
		// Verify result consistency
		if err == nil {
			result2, err2 := v.Float64()
			if err2 != nil {
				t.Errorf("second call failed but first succeeded")
			}
			// NaN != NaN，需要特殊处理
			// NaN != NaN, needs special handling
			if result != result2 && !(isNaN(result) && isNaN(result2)) {
				t.Errorf("inconsistent results: %f vs %f", result, result2)
			}
		}

		// 验证 Float64Or 的一致性
		// Verify Float64Or consistency
		defaultVal := 123.456
		orResult := v.Float64Or(defaultVal)
		if err == nil {
			// NaN != NaN，需要特殊处理
			// NaN != NaN, needs special handling
			if orResult != result && !(isNaN(orResult) && isNaN(result)) {
				t.Errorf("Float64Or returned %f, but Float64 returned %f", orResult, result)
			}
		}
		if err != nil && orResult != defaultVal {
			t.Errorf("Float64Or should return default %f on error, got %f", defaultVal, orResult)
		}
	})
}

// FuzzJSONDecode 对 JSON 解码进行模糊测试
// FuzzJSONDecode performs fuzz testing on JSON decoding
func FuzzJSONDecode(f *testing.F) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}

	// 添加种子语料
	// Add seed corpus
	f.Add(`{"name":"Alice","email":"alice@example.com","age":30}`)
	f.Add(`{"name":"Bob"}`)
	f.Add(`{}`)
	f.Add(`{"name":""}`)
	f.Add(`{"age":-1}`)
	f.Add(`{"name":"test","extra":"field"}`)
	f.Add(`null`)
	f.Add(`[]`)
	f.Add(`""`)

	f.Fuzz(func(t *testing.T, jsonInput string) {
		rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(jsonInput)))
		rawReq.Header.Set("Content-Type", "application/json")

		req := &Request{Raw: rawReq}
		typedReq := NewRequestOf[User](req)

		// 尝试解码
		// Attempt to decode
		data, err := typedReq.Data(context.Background())

		// 如果成功解码，验证数据可以重新序列化
		// If successfully decoded, verify data can be re-serialized
		if err == nil {
			marshaled, marshalErr := json.Marshal(data)
			if marshalErr != nil {
				t.Errorf("decoded data cannot be marshaled back: %v", marshalErr)
			}

			// 验证再次解码得到相同结果
			// Verify decoding again produces same result
			var data2 User
			unmarshalErr := json.Unmarshal(marshaled, &data2)
			if unmarshalErr != nil {
				t.Errorf("re-unmarshaling failed: %v", unmarshalErr)
			}

			if data.Name != data2.Name || data.Email != data2.Email || data.Age != data2.Age {
				t.Errorf("round-trip produced different data")
			}
		}

		// 验证多次调用返回相同结果（缓存测试）
		// Verify multiple calls return same result (cache test)
		data2, err2 := typedReq.Data(context.Background())
		if (err == nil) != (err2 == nil) {
			t.Errorf("second call has different error status")
		}
		if err == nil {
			if data.Name != data2.Name || data.Email != data2.Email || data.Age != data2.Age {
				t.Errorf("cached data is different from original")
			}
		}
	})
}

// FuzzValueIntSlice 对 Values.IntSlice() 进行模糊测试
// FuzzValueIntSlice performs fuzz testing on Values.IntSlice()
func FuzzValueIntSlice(f *testing.F) {
	// 添加种子语料
	// Add seed corpus
	f.Add("1,2,3")
	f.Add("0")
	f.Add("-1,-2,-3")
	f.Add("")
	f.Add("a,b,c")
	f.Add("1,2,abc")

	f.Fuzz(func(t *testing.T, input string) {
		var parts []string
		if input == "" {
			parts = []string{}
		} else {
			parts = []string{input}
			// 尝试按逗号分割
			// Try to split by comma
			if len(input) > 0 {
				// 简单模拟多值场景
				// Simple simulation of multi-value scenario
				parts = []string{input}
			}
		}

		vs := newValues(parts)
		result, err := vs.IntSlice()

		// 如果成功，验证长度一致
		// If successful, verify length matches
		if err == nil {
			if len(result) != len(parts) {
				t.Errorf("result length %d != input length %d", len(result), len(parts))
			}
		}

		// 验证 Int64Slice 的一致性
		// Verify Int64Slice consistency
		result64, err64 := vs.Int64Slice()
		if (err == nil) != (err64 == nil) {
			t.Errorf("IntSlice and Int64Slice have different error status")
		}
		if err == nil && err64 == nil {
			if len(result) != len(result64) {
				t.Errorf("IntSlice and Int64Slice returned different lengths")
			}
			for i := range result {
				if int64(result[i]) != result64[i] {
					t.Errorf("IntSlice[%d]=%d, but Int64Slice[%d]=%d", i, result[i], i, result64[i])
				}
			}
		}
	})
}
