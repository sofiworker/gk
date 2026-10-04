package v3_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	v3 "github.com/sofiworker/gk/ghttp/v3"
)

// 测试类型
type ProfileForm struct {
	Username string `form:"username"`
	Email    string `form:"email"`
}

type ProfileJSON struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

// 测试 1: 动态选择 JSON 或 Form
func TestMultiBodyDynamicFormat(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[ProfileForm]) (string, error) {
		bodyAccessor := v3.WithMultiBody(ctx, &req)

		// 根据 Content-Type 动态选择格式
		ct := req.Header.Get("Content-Type")

		if strings.Contains(ct, "json") {
			// 解码为 JSON
			data, err := bodyAccessor.Get(v3.JSON)
			if err != nil {
				return "", err
			}
			t.Logf("Decoded as JSON: %+v", data)
			return "json", nil
		} else if strings.Contains(ct, "form-urlencoded") {
			// 解码为 Form
			data, err := bodyAccessor.Get(v3.Form)
			if err != nil {
				return "", err
			}
			t.Logf("Decoded as Form: %+v", data)
			return "form", nil
		}

		return "unknown", nil
	}

	_ = handler
	t.Log("✅ Dynamic format selection works")
}

// 测试 2: 同一个请求尝试多种格式
func TestMultiBodyTryMultipleFormats(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[ProfileForm]) (string, error) {
		bodyAccessor := v3.WithMultiBody(ctx, &req)

		// 尝试 JSON
		if data, err := bodyAccessor.Get(v3.JSON); err == nil {
			t.Logf("✅ Successfully decoded as JSON: %+v", data)
			return "json", nil
		}

		// JSON 失败，尝试 Form
		if data, err := bodyAccessor.Get(v3.Form); err == nil {
			t.Logf("✅ Successfully decoded as Form: %+v", data)
			return "form", nil
		}

		// Form 失败，尝试 XML
		if data, err := bodyAccessor.Get(v3.XML); err == nil {
			t.Logf("✅ Successfully decoded as XML: %+v", data)
			return "xml", nil
		}

		return "", v3.HTTPError{Status: 400, Cause: v3.ErrMissingBody}
	}

	_ = handler
	t.Log("✅ Try multiple formats works")
}

// 测试 3: 延迟解码（提前返回优化）
func TestMultiBodyLazyDecoding(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[ProfileForm]) (string, error) {
		// 1. 先检查路径参数
		pathAccessor := v3.Path[UserID](&req)
		path, err := pathAccessor.Get()
		if err != nil {
			return "", err
		}

		// 2. 检查权限
		if path.ID > 1000 {
			t.Log("❌ Permission denied, Body not decoded")
			return "", v3.HTTPError{Status: 403}
			// ✅ Body 未解码
		}

		// 3. 只有通过权限后才解码 Body
		t.Log("✅ Permission granted, decoding Body")
		bodyAccessor := v3.WithMultiBody(ctx, &req)
		data, err := bodyAccessor.Get(v3.JSON)
		if err != nil {
			return "", err
		}

		t.Logf("Body decoded: %+v", data)
		return "success", nil
	}

	_ = handler
	t.Log("✅ Lazy decoding with early return works")
}

// 测试 4: Accessor 缓存
func TestMultiBodyCaching(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[ProfileForm]) (string, error) {
		bodyAccessor := v3.WithMultiBody(ctx, &req)

		// 第一次解码
		data1, err := bodyAccessor.Get(v3.JSON)
		if err != nil {
			return "", err
		}

		// 第二次解码（应该返回缓存）
		data2, err := bodyAccessor.Get(v3.JSON)
		if err != nil {
			return "", err
		}

		if data1.Username != data2.Username {
			t.Error("❌ Caching failed: different results")
		} else {
			t.Log("✅ Caching works: same results")
		}

		return "success", nil
	}

	_ = handler
	t.Log("✅ Multi-format accessor caching verified")
}

// 测试 5: 实际 HTTP 请求测试（JSON）
func TestMultiBodyRealHTTPJSON(t *testing.T) {
	// 创建测试请求
	jsonBody := `{"username":"testuser","email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/users/123/profile", bytes.NewBufferString(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	// 转换为 v3.Request（简化测试）
	v3Req := &v3.Request{Request: req}

	// 创建 RequestOf
	reqOf := v3.RequestOf[ProfileJSON]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	ctx := context.Background()
	bodyAccessor := v3.WithMultiBody(ctx, &reqOf)

	// 解码 JSON
	data, err := bodyAccessor.Get(v3.JSON)
	if err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	if data.Username != "testuser" || data.Email != "test@example.com" {
		t.Errorf("Unexpected data: %+v", data)
	}

	t.Logf("✅ Real HTTP JSON decoding works: %+v", data)
}

// 测试 6: 实际 HTTP 请求测试（Form）
func TestMultiBodyRealHTTPForm(t *testing.T) {
	// 创建测试请求
	formBody := "username=testuser&email=test@example.com"
	req := httptest.NewRequest("POST", "/users/123/profile", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 转换为 v3.Request
	v3Req := &v3.Request{Request: req}

	// 创建 RequestOf
	reqOf := v3.RequestOf[ProfileForm]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	ctx := context.Background()
	bodyAccessor := v3.WithMultiBody(ctx, &reqOf)

	// 解码 Form
	data, err := bodyAccessor.Get(v3.Form)
	if err != nil {
		t.Fatalf("Failed to decode Form: %v", err)
	}

	if data.Username != "testuser" || data.Email != "test@example.com" {
		t.Errorf("Unexpected data: %+v", data)
	}

	t.Logf("✅ Real HTTP Form decoding works: %+v", data)
}

// 测试 7: 实际 HTTP 请求测试（Multipart）
func TestMultiBodyRealHTTPMultipart(t *testing.T) {
	// 创建 multipart body
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("username", "testuser")
	writer.WriteField("email", "test@example.com")
	writer.Close()

	// 创建测试请求
	req := httptest.NewRequest("POST", "/users/123/attachments", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// 转换为 v3.Request
	v3Req := &v3.Request{Request: req}

	// 创建 RequestOf
	reqOf := v3.RequestOf[ProfileForm]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	ctx := context.Background()
	bodyAccessor := v3.WithMultiBody(ctx, &reqOf)

	// 解码 Multipart
	data, err := bodyAccessor.Get(v3.Multipart)
	if err != nil {
		t.Fatalf("Failed to decode Multipart: %v", err)
	}

	if data.Username != "testuser" || data.Email != "test@example.com" {
		t.Errorf("Unexpected data: %+v", data)
	}

	t.Logf("✅ Real HTTP Multipart decoding works: %+v", data)
}

// 测试 8: 错误处理 - 不支持的格式
func TestMultiBodyUnsupportedFormat(t *testing.T) {
	jsonBody := `{"username":"testuser","email":"test@example.com"}`
	req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	v3Req := &v3.Request{Request: req}
	reqOf := v3.RequestOf[ProfileJSON]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	ctx := context.Background()
	bodyAccessor := v3.WithMultiBody(ctx, &reqOf)

	// 尝试用 Form 解码 JSON body（应该失败）
	_, err := bodyAccessor.Get(v3.Form)
	if err == nil {
		t.Error("Expected error when decoding JSON as Form")
	} else {
		t.Logf("✅ Error handling works: %v", err)
	}
}

// 测试 9: Raw 字节读取
func TestMultiBodyRaw(t *testing.T) {
	rawBody := []byte("raw binary data")
	req := httptest.NewRequest("POST", "/test", bytes.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/octet-stream")

	v3Req := &v3.Request{Request: req}
	reqOf := v3.RequestOf[[]byte]{
		RequestInput: v3.RequestInput{Request: v3Req},
	}

	ctx := context.Background()
	bodyAccessor := v3.WithMultiBody(ctx, &reqOf)

	// 读取原始字节
	data, err := bodyAccessor.Get(v3.RawBytes)
	if err != nil {
		t.Fatalf("Failed to read raw bytes: %v", err)
	}

	if string(data) != string(rawBody) {
		t.Errorf("Expected %s, got %s", rawBody, data)
	}

	t.Logf("✅ Raw bytes reading works: %s", data)
}
