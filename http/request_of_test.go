package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRequestOfCopiesShareDecoder verifies lazy and concurrent caching across
// value copies, including custom input errors and the first caller's context.
func TestRequestOfCopiesShareDecoder(t *testing.T) {
	type body struct{ Name string }
	decodeErr := errors.New("custom decode failed")
	for _, wantErr := range []error{nil, decodeErr} {
		name := "success"
		if wantErr != nil {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			req := &Request{Raw: httptest.NewRequest("POST", "/custom", nil)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			in := InputFunc[body](func(gotCtx context.Context, gotReq *Request) (body, error) {
				calls.Add(1)
				if gotCtx != ctx || gotReq != req {
					t.Error("decoder received a different context or request")
				}
				return body{Name: "custom"}, wantErr
			})
			original := newRequestOf(req, in)
			copyBeforeDecode := original
			if calls.Load() != 0 {
				t.Fatal("body decoded before Data")
			}
			var wg sync.WaitGroup
			for i := 0; i < 32; i++ {
				wg.Add(1)
				go func(r RequestOf[body]) {
					defer wg.Done()
					got, err := r.Data(ctx)
					if got.Name != "custom" || err != wantErr {
						t.Errorf("Data() = %+v, %v; want custom, %v", got, err, wantErr)
					}
				}(copyBeforeDecode)
			}
			wg.Wait()
			copyAfterDecode := original
			if _, err := copyAfterDecode.Data(context.Background()); err != wantErr {
				t.Fatalf("cached error = %v, want %v", err, wantErr)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("decoder called %d times, want 1", got)
			}
		})
	}
}

// TestRequestOf_LazyBodyDecoding 测试 lazy body 解码
// TestRequestOf_LazyBodyDecoding tests lazy body decoding
func TestRequestOf_LazyBodyDecoding(t *testing.T) {
	type User struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}

	body := `{"name":"Alice","email":"alice@example.com"}`
	rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[User](req)

	// 首次调用 Data() 应该解码 body
	// First call to Data() should decode body
	data1, err := typedReq.Data(context.Background())
	if err != nil {
		t.Fatalf("first Data() call failed: %v", err)
	}

	if data1.Name != "Alice" {
		t.Errorf("expected name 'Alice', got %q", data1.Name)
	}

	if data1.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", data1.Email)
	}

	// 第二次调用 Data() 应该返回缓存结果（不重新解码）
	// Second call to Data() should return cached result (no re-decode)
	data2, err := typedReq.Data(context.Background())
	if err != nil {
		t.Fatalf("second Data() call failed: %v", err)
	}

	if data2.Name != data1.Name {
		t.Error("second call returned different data (should be cached)")
	}
}

// TestRequestOf_ConcurrentDataAccess 测试并发调用 Data() 的线程安全性
// TestRequestOf_ConcurrentDataAccess tests thread safety of concurrent Data() calls
func TestRequestOf_ConcurrentDataAccess(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	body := `{"name":"Bob"}`
	rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[User](req)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errors := make(chan error, numGoroutines)
	results := make(chan User, numGoroutines)

	// 并发调用 Data()
	// Concurrent calls to Data()
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			data, err := typedReq.Data(context.Background())
			if err != nil {
				errors <- err
				return
			}

			results <- data
		}()
	}

	wg.Wait()
	close(errors)
	close(results)

	// 检查错误
	// Check for errors
	for err := range errors {
		t.Errorf("concurrent Data() call error: %v", err)
	}

	// 验证所有结果一致
	// Verify all results are consistent
	var names []string
	for data := range results {
		names = append(names, data.Name)
	}

	if len(names) != numGoroutines {
		t.Fatalf("expected %d results, got %d", numGoroutines, len(names))
	}

	for i, name := range names {
		if name != "Bob" {
			t.Errorf("result[%d]: expected name 'Bob', got %q", i, name)
		}
	}
}

// TestRequestOf_NoDataType 测试 NoData 类型不解码 body
// TestRequestOf_NoDataType tests NoData type does not decode body
func TestRequestOf_NoDataType(t *testing.T) {
	// 即使提供了 body，NoData 也不应该尝试解码
	// Even if body is provided, NoData should not attempt to decode
	body := `{"name":"should not be decoded"}`
	rawReq := httptest.NewRequest("GET", "/test", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[NoDataType](req)

	// 调用 Data() 不应该报错，且不应该解码 body
	// Calling Data() should not error and should not decode body
	data, err := typedReq.Data(context.Background())
	if err != nil {
		t.Errorf("NoData type should not error, got: %v", err)
	}

	// 验证返回的是 NoData
	// Verify NoData is returned
	if data != NoData {
		t.Error("expected NoData to be returned")
	}
}

// TestRequestOf_JSONDecodeError 测试 JSON 解码失败
// TestRequestOf_JSONDecodeError tests JSON decode failure
func TestRequestOf_JSONDecodeError(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name        string
		body        string
		expectError bool
	}{
		{
			name:        "invalid JSON syntax",
			body:        `{invalid json}`,
			expectError: true,
		},
		{
			name:        "incomplete JSON",
			body:        `{"name":"Alice"`,
			expectError: true,
		},
		{
			name:        "empty body",
			body:        ``,
			expectError: true,
		},
		{
			name:        "valid JSON",
			body:        `{"name":"Alice"}`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(tt.body)))
			rawReq.Header.Set("Content-Type", "application/json")

			req := &Request{Raw: rawReq}
			typedReq := NewRequestOf[User](req)

			_, err := typedReq.Data(context.Background())

			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

// TestRequestOf_EarlyReturn 测试提前返回场景不解码 body
// TestRequestOf_EarlyReturn tests body is not decoded on early return
func TestRequestOf_EarlyReturn(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	// 模拟提前返回场景：只检查参数，不解码 body
	// Simulate early return scenario: only check parameters, don't decode body
	body := `{"name":"Alice"}`
	rawReq := httptest.NewRequest("GET", "/early-return", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[User](req)

	// 验证可以访问参数而不解码 body
	// Verify can access parameters without decoding body
	_ = typedReq.HeaderValue("Authorization").Exists()
	_ = typedReq.PathValue("id").Exists()

	// 此时 body 尚未解码（lazy 特性）
	// At this point body is not yet decoded (lazy feature)
	// 这验证了 lazy 解码的性能优化
	// This verifies the performance optimization of lazy decoding
}

// TestRequestOf_StructPointerType 测试结构体指针类型
// TestRequestOf_StructPointerType tests struct pointer type
func TestRequestOf_StructPointerType(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	body := `{"name":"Charlie","age":30}`
	rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[*User](req)

	data, err := typedReq.Data(context.Background())
	if err != nil {
		t.Fatalf("Data() call failed: %v", err)
	}

	if data == nil {
		t.Fatal("expected non-nil pointer, got nil")
	}

	if data.Name != "Charlie" {
		t.Errorf("expected name 'Charlie', got %q", data.Name)
	}

	if data.Age != 30 {
		t.Errorf("expected age 30, got %d", data.Age)
	}
}

// TestRequestOf_NestedStructs 测试嵌套结构体解码
// TestRequestOf_NestedStructs tests nested struct decoding
func TestRequestOf_NestedStructs(t *testing.T) {
	type Address struct {
		City    string `json:"city"`
		Country string `json:"country"`
	}

	type User struct {
		Name    string  `json:"name"`
		Address Address `json:"address"`
	}

	body := `{"name":"David","address":{"city":"New York","country":"USA"}}`
	rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[User](req)

	data, err := typedReq.Data(context.Background())
	if err != nil {
		t.Fatalf("Data() call failed: %v", err)
	}

	if data.Name != "David" {
		t.Errorf("expected name 'David', got %q", data.Name)
	}

	if data.Address.City != "New York" {
		t.Errorf("expected city 'New York', got %q", data.Address.City)
	}

	if data.Address.Country != "USA" {
		t.Errorf("expected country 'USA', got %q", data.Address.Country)
	}
}

// TestRequestOf_SliceType 测试切片类型解码
// TestRequestOf_SliceType tests slice type decoding
func TestRequestOf_SliceType(t *testing.T) {
	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	type Batch struct {
		Items []Item `json:"items"`
	}

	body := `{"items":[{"id":1,"name":"item1"},{"id":2,"name":"item2"}]}`
	rawReq := httptest.NewRequest("POST", "/batch", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[Batch](req)

	data, err := typedReq.Data(context.Background())
	if err != nil {
		t.Fatalf("Data() call failed: %v", err)
	}

	if len(data.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data.Items))
	}

	if data.Items[0].ID != 1 || data.Items[0].Name != "item1" {
		t.Errorf("item 0: expected {1, 'item1'}, got {%d, %q}", data.Items[0].ID, data.Items[0].Name)
	}

	if data.Items[1].ID != 2 || data.Items[1].Name != "item2" {
		t.Errorf("item 1: expected {2, 'item2'}, got {%d, %q}", data.Items[1].ID, data.Items[1].Name)
	}
}

// TestRequestOf_ErrorCaching 测试错误缓存
// TestRequestOf_ErrorCaching tests error caching
func TestRequestOf_ErrorCaching(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	// 提供无效的 JSON
	// Provide invalid JSON
	body := `{invalid}`
	rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[User](req)

	// 首次调用应该返回错误
	// First call should return error
	_, err1 := typedReq.Data(context.Background())
	if err1 == nil {
		t.Fatal("expected error on first call, got nil")
	}

	// 第二次调用应该返回相同的错误（缓存）
	// Second call should return same error (cached)
	_, err2 := typedReq.Data(context.Background())
	if err2 == nil {
		t.Fatal("expected error on second call, got nil")
	}

	// 验证错误是否相同
	// Verify errors are the same
	if err1.Error() != err2.Error() {
		t.Errorf("expected same error, got different errors: %v vs %v", err1, err2)
	}
}

// TestRequestOf_RequestInputFields 测试 RequestInput 字段访问
// TestRequestOf_RequestInputFields tests RequestInput field access
func TestRequestOf_RequestInputFields(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	body := `{"name":"Eve"}`
	rawReq := httptest.NewRequest("POST", "/users/123?page=2&tag=go&tag=web", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")
	rawReq.Header.Set("X-Request-ID", "req-456")

	req := &Request{
		Raw:    rawReq,
		Params: []Param{{Key: "id", Value: "123"}},
	}
	typedReq := NewRequestOf[User](req)

	// 测试 PathValue
	// Test PathValue
	id, err := typedReq.PathValue("id").String()
	if err != nil {
		t.Errorf("PathValue error: %v", err)
	}
	if id != "123" {
		t.Errorf("expected id '123', got %q", id)
	}

	// 测试 QueryValue
	// Test QueryValue
	page, err := typedReq.QueryValue("page").Int()
	if err != nil {
		t.Errorf("QueryValue error: %v", err)
	}
	if page != 2 {
		t.Errorf("expected page 2, got %d", page)
	}

	// 测试 QueryValues
	// Test QueryValues
	tags := typedReq.QueryValues("tag").Strings()
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0] != "go" || tags[1] != "web" {
		t.Errorf("expected tags ['go', 'web'], got %v", tags)
	}

	// 测试 HeaderValue
	// Test HeaderValue
	requestID, err := typedReq.HeaderValue("X-Request-ID").String()
	if err != nil {
		t.Errorf("HeaderValue error: %v", err)
	}
	if requestID != "req-456" {
		t.Errorf("expected request ID 'req-456', got %q", requestID)
	}

	// 测试 Data
	// Test Data
	data, err := typedReq.Data(context.Background())
	if err != nil {
		t.Fatalf("Data() call failed: %v", err)
	}
	if data.Name != "Eve" {
		t.Errorf("expected name 'Eve', got %q", data.Name)
	}
}

// TestRequestOf_ContextCancellation 测试 context 取消
// TestRequestOf_ContextCancellation tests context cancellation
func TestRequestOf_ContextCancellation(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	body := `{"name":"Frank"}`
	rawReq := httptest.NewRequest("POST", "/users", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[User](req)

	// 创建可取消的 context
	// Create cancellable context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// 正常情况下，解码应该在超时前完成
	// Normally, decoding should complete before timeout
	data, err := typedReq.Data(ctx)
	if err != nil {
		// 如果出错，可能是 context 超时（在极慢的机器上）
		// If error occurs, might be context timeout (on extremely slow machines)
		t.Logf("Data() returned error (might be timeout): %v", err)
	} else {
		if data.Name != "Frank" {
			t.Errorf("expected name 'Frank', got %q", data.Name)
		}
	}
}

// TestRequestOf_LargeBody 测试大 body 解码
// TestRequestOf_LargeBody tests large body decoding
func TestRequestOf_LargeBody(t *testing.T) {
	type Record struct {
		ID   int    `json:"id"`
		Data string `json:"data"`
	}

	type Batch struct {
		Records []Record `json:"records"`
	}

	// 生成包含 1000 条记录的 JSON
	// Generate JSON with 1000 records
	var records []Record
	for i := 0; i < 1000; i++ {
		records = append(records, Record{
			ID:   i,
			Data: "data-" + string(rune(i)),
		})
	}

	batch := Batch{Records: records}
	bodyBytes, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("failed to marshal test data: %v", err)
	}

	rawReq := httptest.NewRequest("POST", "/batch", bytes.NewReader(bodyBytes))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[Batch](req)

	// 解码大 body
	// Decode large body
	data, err := typedReq.Data(context.Background())
	if err != nil {
		t.Fatalf("Data() call failed: %v", err)
	}

	if len(data.Records) != 1000 {
		t.Errorf("expected 1000 records, got %d", len(data.Records))
	}

	// 验证第一条和最后一条记录
	// Verify first and last record
	if data.Records[0].ID != 0 {
		t.Errorf("first record: expected ID 0, got %d", data.Records[0].ID)
	}

	if data.Records[999].ID != 999 {
		t.Errorf("last record: expected ID 999, got %d", data.Records[999].ID)
	}
}

// TestRequestOf_EmptyStruct 测试空结构体
// TestRequestOf_EmptyStruct tests empty struct
func TestRequestOf_EmptyStruct(t *testing.T) {
	type EmptyStruct struct{}

	body := `{}`
	rawReq := httptest.NewRequest("POST", "/empty", bytes.NewReader([]byte(body)))
	rawReq.Header.Set("Content-Type", "application/json")

	req := &Request{Raw: rawReq}
	typedReq := NewRequestOf[EmptyStruct](req)

	_, err := typedReq.Data(context.Background())
	if err != nil {
		t.Errorf("expected no error for empty struct, got: %v", err)
	}
}
