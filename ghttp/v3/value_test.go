package v3_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"testing"
	"unsafe"

	v3 "github.com/sofiworker/gk/ghttp/v3"
)

// 测试辅助函数：设置路径参数（因为 Params.add 是未导出的）
func setParams(req *v3.Request, key, val string) {
	// 使用反射访问未导出的 add 方法
	paramsVal := reflect.ValueOf(&req.Params).Elem()
	keysField := paramsVal.FieldByName("keys")
	valsField := paramsVal.FieldByName("vals")

	// 使用 unsafe 修改未导出字段
	keysPtr := (*[]string)(unsafe.Pointer(keysField.UnsafeAddr()))
	valsPtr := (*[]string)(unsafe.Pointer(valsField.UnsafeAddr()))

	*keysPtr = append(*keysPtr, key)
	*valsPtr = append(*valsPtr, val)
}

// TestValueAPI 测试新的类型化值访问器 API
func TestValueAPI(t *testing.T) {
	// 创建测试请求
	req := httptest.NewRequest("GET", "/users/123?page=2&limit=50&active=true&tags=go&tags=web", nil)
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("Content-Type", "application/json")

	// 创建 v3.Request（简化测试）
	v3Req := &v3.Request{Request: req}
	// 使用辅助函数添加路径参数
	setParams(v3Req, "id", "123")

	// 创建 RequestOf
	reqOf := v3.RequestOf[v3.NoData]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	ctx := context.Background()
	_ = ctx

	// 测试 PathValue
	t.Run("PathValue", func(t *testing.T) {
		id, err := reqOf.PathValue("id").Int64()
		if err != nil {
			t.Fatalf("PathValue Int64 failed: %v", err)
		}
		if id != 123 {
			t.Errorf("Expected id=123, got %d", id)
		}
		t.Logf("✅ PathValue Int64: %d", id)
	})

	// 测试 QueryValue
	t.Run("QueryValue", func(t *testing.T) {
		page, err := reqOf.QueryValue("page").Int()
		if err != nil {
			t.Fatalf("QueryValue Int failed: %v", err)
		}
		if page != 2 {
			t.Errorf("Expected page=2, got %d", page)
		}

		limit := reqOf.QueryValue("limit").IntOr(20)
		if limit != 50 {
			t.Errorf("Expected limit=50, got %d", limit)
		}

		active, err := reqOf.QueryValue("active").Bool()
		if err != nil {
			t.Fatalf("QueryValue Bool failed: %v", err)
		}
		if !active {
			t.Errorf("Expected active=true, got %v", active)
		}

		t.Logf("✅ QueryValue: page=%d, limit=%d, active=%v", page, limit, active)
	})

	// 测试 QueryValues（多值）
	t.Run("QueryValues", func(t *testing.T) {
		tags := reqOf.QueryValues("tags").Strings()
		if len(tags) != 2 {
			t.Fatalf("Expected 2 tags, got %d", len(tags))
		}
		if tags[0] != "go" || tags[1] != "web" {
			t.Errorf("Expected tags=[go, web], got %v", tags)
		}

		joined := reqOf.QueryValues("tags").Join(",")
		if joined != "go,web" {
			t.Errorf("Expected 'go,web', got '%s'", joined)
		}

		t.Logf("✅ QueryValues: tags=%v, joined='%s'", tags, joined)
	})

	// 测试 HeaderValue
	t.Run("HeaderValue", func(t *testing.T) {
		auth := reqOf.HeaderValue("Authorization").String()
		if auth != "Bearer token123" {
			t.Errorf("Expected 'Bearer token123', got '%s'", auth)
		}

		contentType := reqOf.HeaderValue("Content-Type").StringOr("text/plain")
		if contentType != "application/json" {
			t.Errorf("Expected 'application/json', got '%s'", contentType)
		}

		// 不存在的 header
		missing := reqOf.HeaderValue("X-Missing").StringOr("default")
		if missing != "default" {
			t.Errorf("Expected 'default', got '%s'", missing)
		}

		t.Logf("✅ HeaderValue: auth='%s', contentType='%s', missing='%s'", auth, contentType, missing)
	})

	// 测试默认值
	t.Run("DefaultValues", func(t *testing.T) {
		// 不存在的 query 参数使用默认值
		offset := reqOf.QueryValue("offset").IntOr(0)
		if offset != 0 {
			t.Errorf("Expected offset=0, got %d", offset)
		}

		enabled := reqOf.QueryValue("enabled").BoolOr(false)
		if enabled {
			t.Errorf("Expected enabled=false, got %v", enabled)
		}

		t.Logf("✅ DefaultValues: offset=%d, enabled=%v", offset, enabled)
	})

	// 测试 Exists
	t.Run("Exists", func(t *testing.T) {
		if !reqOf.PathValue("id").Exists() {
			t.Error("Expected path 'id' to exist")
		}

		if reqOf.QueryValue("missing").Exists() {
			t.Error("Expected query 'missing' to not exist")
		}

		t.Log("✅ Exists check works")
	})
}

// TestValueAPIRealHandler 测试真实 handler 场景
func TestValueAPIRealHandler(t *testing.T) {
	// 模拟真实的 handler
	handler := func(ctx context.Context, req v3.RequestOf[v3.NoData]) (string, error) {
		// 1. 获取 path 参数
		userID, err := req.PathValue("id").Int64()
		if err != nil {
			return "", v3.HTTPError{Status: 400, Cause: err}
		}

		// 2. 获取 query 参数（带默认值）
		page := req.QueryValue("page").IntOr(1)
		limit := req.QueryValue("limit").IntOr(20)

		// 3. 获取 header
		authToken := req.HeaderValue("Authorization").String()
		if authToken == "" {
			return "", v3.HTTPError{Status: 401, Cause: errors.New("unauthorized")}
		}

		// 4. 业务逻辑
		result := fmt.Sprintf("UserID=%d, Page=%d, Limit=%d, Auth=%s", userID, page, limit, authToken)
		return result, nil
	}

	// 创建测试请求
	req := httptest.NewRequest("GET", "/users/456?page=3&limit=100", nil)
	req.Header.Set("Authorization", "Bearer secret")

	v3Req := &v3.Request{Request: req}
	setParams(v3Req, "id", "456")

	reqOf := v3.RequestOf[v3.NoData]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	ctx := context.Background()
	result, err := handler(ctx, reqOf)
	if err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	t.Logf("✅ Real handler result: %s", result)
}

// TestValueAPIErrorHandling 测试错误处理
func TestValueAPIErrorHandling(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?invalid=abc", nil)
	v3Req := &v3.Request{Request: req}

	reqOf := v3.RequestOf[v3.NoData]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	t.Run("InvalidInt", func(t *testing.T) {
		_, err := reqOf.QueryValue("invalid").Int()
		if err == nil {
			t.Error("Expected error when parsing 'abc' as int")
		}
		t.Logf("✅ Invalid int error: %v", err)
	})

	t.Run("MissingRequired", func(t *testing.T) {
		_, err := reqOf.QueryValue("missing").Int64()
		if err == nil {
			t.Error("Expected error for missing parameter")
		}
		t.Logf("✅ Missing parameter error: %v", err)
	})

	t.Run("WithDefaultNoError", func(t *testing.T) {
		// 使用 Or 方法不会返回错误
		val := reqOf.QueryValue("missing").IntOr(999)
		if val != 999 {
			t.Errorf("Expected 999, got %d", val)
		}
		t.Logf("✅ Default value: %d", val)
	})
}

// TestValueAPIComparison 对比新旧 API
func TestValueAPIComparison(t *testing.T) {
	req := httptest.NewRequest("GET", "/users/789?page=5", nil)
	v3Req := &v3.Request{Request: req}
	setParams(v3Req, "id", "789")

	reqOf := v3.RequestOf[v3.NoData]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	t.Run("OldAPI", func(t *testing.T) {
		// ❌ 旧 API：需要手动转换
		idStr := reqOf.Path("id")
		pageStr := reqOf.QueryFirstValue("page")

		t.Logf("Old API: id='%s' (string), page='%s' (string)", idStr, pageStr)
		t.Log("⚠️ 需要手动 strconv.ParseInt")
	})

	t.Run("NewAPI", func(t *testing.T) {
		// ✅ 新 API：类型安全
		id, _ := reqOf.PathValue("id").Int64()
		page := reqOf.QueryValue("page").IntOr(1)

		t.Logf("New API: id=%d (int64), page=%d (int)", id, page)
		t.Log("✅ 类型安全，自动转换")
	})
}
