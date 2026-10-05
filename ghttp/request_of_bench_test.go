package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkLazyBodyDecode_EarlyReturn 测试提前返回场景下 lazy 解码的性能
// BenchmarkLazyBodyDecode_EarlyReturn benchmarks lazy decoding in early return scenarios
func BenchmarkLazyBodyDecode_EarlyReturn(b *testing.B) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}

	body := `{"name":"Alice","email":"alice@example.com","age":30}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
		rawReq.Header.Set("Content-Type", "application/json")

		req := &Request{Raw: rawReq}
		typedReq := NewRequestOf[User](req)

		// 模拟提前返回场景：不调用 Data()
		// Simulate early return: don't call Data()
		_ = typedReq.PathValue("id").Exists()
	}
}

// BenchmarkEagerBodyDecode_EarlyReturn 测试提前返回场景下立即解码的性能（对照组）
// BenchmarkEagerBodyDecode_EarlyReturn benchmarks eager decoding in early return scenarios (control group)
func BenchmarkEagerBodyDecode_EarlyReturn(b *testing.B) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}

	body := `{"name":"Alice","email":"alice@example.com","age":30}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
		rawReq.Header.Set("Content-Type", "application/json")

		req := &Request{Raw: rawReq}

		// 模拟立即解码（eager）
		// Simulate eager decoding
		var user User
		_ = json.NewDecoder(req.Raw.Body).Decode(&user)

		// 提前返回场景：解码已完成但未使用
		// Early return scenario: decoding completed but unused
		_ = req.Params.Get("id")
	}
}

// BenchmarkLazyBodyDecode_FullFlow 测试完整流程下 lazy 解码的性能
// BenchmarkLazyBodyDecode_FullFlow benchmarks lazy decoding in full flow
func BenchmarkLazyBodyDecode_FullFlow(b *testing.B) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}

	body := `{"name":"Alice","email":"alice@example.com","age":30}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
		rawReq.Header.Set("Content-Type", "application/json")

		req := &Request{Raw: rawReq}
		typedReq := NewRequestOf[User](req)

		// 完整流程：调用 Data() 解码
		// Full flow: call Data() to decode
		data, _ := typedReq.Data(context.Background())
		_ = data.Name
	}
}

// BenchmarkEagerBodyDecode_FullFlow 测试完整流程下立即解码的性能（对照组）
// BenchmarkEagerBodyDecode_FullFlow benchmarks eager decoding in full flow (control group)
func BenchmarkEagerBodyDecode_FullFlow(b *testing.B) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}

	body := `{"name":"Alice","email":"alice@example.com","age":30}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
		rawReq.Header.Set("Content-Type", "application/json")

		req := &Request{Raw: rawReq}

		// 模拟立即解码（eager）
		// Simulate eager decoding
		var user User
		_ = json.NewDecoder(req.Raw.Body).Decode(&user)
		_ = user.Name
	}
}

// BenchmarkLazyBodyDecode_CachedAccess 测试缓存访问的性能
// BenchmarkLazyBodyDecode_CachedAccess benchmarks cached access performance
func BenchmarkLazyBodyDecode_CachedAccess(b *testing.B) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Age   int    `json:"age"`
	}

	body := `{"name":"Alice","email":"alice@example.com","age":30}`
	rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[User](req)

	// 首次解码
	// First decode
	_, _ = typedReq.Data(context.Background())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 后续访问应使用缓存
		// Subsequent accesses should use cache
		data, _ := typedReq.Data(context.Background())
		_ = data.Name
	}
}

// BenchmarkValueInt64 测试 Value.Int64() 的性能
// BenchmarkValueInt64 benchmarks Value.Int64() performance
func BenchmarkValueInt64(b *testing.B) {
	v := newValue("123456789", true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = v.Int64()
	}
}

// BenchmarkValueInt64Or 测试 Value.Int64Or() 的性能
// BenchmarkValueInt64Or benchmarks Value.Int64Or() performance
func BenchmarkValueInt64Or(b *testing.B) {
	v := newValue("123456789", true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Int64Or(0)
	}
}

// BenchmarkValueBool 测试 Value.Bool() 的性能
// BenchmarkValueBool benchmarks Value.Bool() performance
func BenchmarkValueBool(b *testing.B) {
	v := newValue("true", true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = v.Bool()
	}
}

// BenchmarkValueBoolOr 测试 Value.BoolOr() 的性能
// BenchmarkValueBoolOr benchmarks Value.BoolOr() performance
func BenchmarkValueBoolOr(b *testing.B) {
	v := newValue("true", true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.BoolOr(false)
	}
}

// BenchmarkValueFloat64 测试 Value.Float64() 的性能
// BenchmarkValueFloat64 benchmarks Value.Float64() performance
func BenchmarkValueFloat64(b *testing.B) {
	v := newValue("3.14159", true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = v.Float64()
	}
}

// BenchmarkValueString 测试 Value.String() 的性能
// BenchmarkValueString benchmarks Value.String() performance
func BenchmarkValueString(b *testing.B) {
	v := newValue("test-string-value", true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = v.String()
	}
}

// BenchmarkPathValue 测试 PathValue() 的性能
// BenchmarkPathValue benchmarks PathValue() performance
func BenchmarkPathValue(b *testing.B) {
	req := &Request{
		Params: Params{
			{Key: "id", Value: "123"},
			{Key: "name", Value: "test"},
			{Key: "action", Value: "update"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := PathValue(req, "name")
		_, _ = v.String()
	}
}

// BenchmarkQueryValue 测试 QueryValue() 的性能
// BenchmarkQueryValue benchmarks QueryValue() performance
func BenchmarkQueryValue(b *testing.B) {
	rawReq := httptest.NewRequest("GET", "/test?page=1&limit=20&sort=name&order=asc", nil)
	req := &Request{Raw: rawReq}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := QueryValue(req, "page")
		_, _ = v.Int()
	}
}

// BenchmarkQueryValues 测试 QueryValues() 的性能
// BenchmarkQueryValues benchmarks QueryValues() performance
func BenchmarkQueryValues(b *testing.B) {
	rawReq := httptest.NewRequest("GET", "/test?tags=go&tags=rust&tags=python&tags=java&tags=cpp", nil)
	req := &Request{Raw: rawReq}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vs := QueryValues(req, "tags")
		_ = vs.Strings()
	}
}

// BenchmarkHeaderValue 测试 HeaderValue() 的性能
// BenchmarkHeaderValue benchmarks HeaderValue() performance
func BenchmarkHeaderValue(b *testing.B) {
	rawReq := &http.Request{
		Header: http.Header{
			"Authorization": []string{"Bearer token123"},
			"Content-Type":  []string{"application/json"},
			"X-Request-ID":  []string{"req-456"},
		},
	}
	req := &Request{Raw: rawReq}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := HeaderValue(req, "Authorization")
		_, _ = v.String()
	}
}

// BenchmarkValuesIntSlice 测试 Values.IntSlice() 的性能
// BenchmarkValuesIntSlice benchmarks Values.IntSlice() performance
func BenchmarkValuesIntSlice(b *testing.B) {
	vs := newValues([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = vs.IntSlice()
	}
}

// BenchmarkValuesInt64Slice 测试 Values.Int64Slice() 的性能
// BenchmarkValuesInt64Slice benchmarks Values.Int64Slice() performance
func BenchmarkValuesInt64Slice(b *testing.B) {
	vs := newValues([]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = vs.Int64Slice()
	}
}

// BenchmarkRequestOf_SmallBody 测试小 body 解码性能
// BenchmarkRequestOf_SmallBody benchmarks small body decoding performance
func BenchmarkRequestOf_SmallBody(b *testing.B) {
	type User struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	body := `{"id":1,"name":"Alice"}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
		rawReq.Header.Set("Content-Type", "application/json")

		req := &Request{Raw: rawReq}
		typedReq := NewRequestOf[User](req)

		data, _ := typedReq.Data(context.Background())
		_ = data.Name
	}
}

// BenchmarkRequestOf_LargeBody 测试大 body 解码性能
// BenchmarkRequestOf_LargeBody benchmarks large body decoding performance
func BenchmarkRequestOf_LargeBody(b *testing.B) {
	type Record struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Data string `json:"data"`
	}

	type Batch struct {
		Records []Record `json:"records"`
	}

	// 生成 100 条记录
	// Generate 100 records
	var records []Record
	for i := 0; i < 100; i++ {
		records = append(records, Record{
			ID:   i,
			Name: "record-" + string(rune(i)),
			Data: "some data here",
		})
	}

	batch := Batch{Records: records}
	bodyBytes, _ := json.Marshal(batch)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rawReq := httptest.NewRequest("POST", "/batch", bytes.NewReader(bodyBytes))
		rawReq.Header.Set("Content-Type", "application/json")

		req := &Request{Raw: rawReq}
		typedReq := NewRequestOf[Batch](req)

		data, _ := typedReq.Data(context.Background())
		_ = data.Records
	}
}

// BenchmarkRequestOf_NestedStruct 测试嵌套结构体解码性能
// BenchmarkRequestOf_NestedStruct benchmarks nested struct decoding performance
func BenchmarkRequestOf_NestedStruct(b *testing.B) {
	type Address struct {
		Street  string `json:"street"`
		City    string `json:"city"`
		Country string `json:"country"`
	}

	type User struct {
		Name    string  `json:"name"`
		Email   string  `json:"email"`
		Address Address `json:"address"`
	}

	body := `{"name":"Bob","email":"bob@example.com","address":{"street":"123 Main St","city":"New York","country":"USA"}}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
		rawReq.Header.Set("Content-Type", "application/json")

		req := &Request{Raw: rawReq}
		typedReq := NewRequestOf[User](req)

		data, _ := typedReq.Data(context.Background())
		_ = data.Address.City
	}
}

// BenchmarkRequestInput_AllAccessors 测试所有访问器的组合性能
// BenchmarkRequestInput_AllAccessors benchmarks combined performance of all accessors
func BenchmarkRequestInput_AllAccessors(b *testing.B) {
	rawReq := httptest.NewRequest("GET", "/test?page=1&limit=20", nil)
	rawReq.Header.Set("Authorization", "Bearer token")
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{
		Raw: rawReq,
		Params: Params{
			{Key: "id", Value: "123"},
		},
	}

	input := RequestInput{req: req}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = input.PathValue("id").Int()
		_, _ = input.QueryValue("page").Int()
		_, _ = input.QueryValue("limit").Int()
		_, _ = input.HeaderValue("Authorization").String()
	}
}
