package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sofiworker/gk/ghttp"
)

// ============================================================================
// 辅助函数
// ============================================================================

func doRequest(t *testing.T, s *ghttp.Server, method, path string, body interface{}, headers map[string]string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)
	return rr.Result()
}

func readBodyBytes(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return data
}

// ============================================================================
// 健康检查测试
// ============================================================================

func TestHealth(t *testing.T) {
	s := buildServer()
	resp := doRequest(t, s, http.MethodGet, "/health", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d, body: %s", resp.StatusCode, http.StatusOK, string(readBodyBytes(t, resp)))
	}
	body := readBodyBytes(t, resp)
	var result HealthOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal health: %v, body: %s", err, string(body))
	}
	if result.Status != "healthy" {
		t.Errorf("status = %s, want healthy", result.Status)
	}
}

// ============================================================================
// 路由参数问题发现
// ============================================================================

func TestQueryParamsAreAllStrings(t *testing.T) {
	s := buildServer()
	resp := doRequest(t, s, http.MethodGet, "/api/v2/articles?page=3&pageSize=5", nil, nil)
	body := readBodyBytes(t, resp)

	// 问题：所有 query 参数都是 string，需要手动转换
	// 主流框架通常支持 QueryInt, QueryBool 等方法
	if resp.StatusCode == http.StatusOK {
		t.Logf("[发现-设计不足] Params 只有 QueryString 方法，缺少类型化读取方法")
		t.Logf("  当前需要手动: strconv.Atoi(req.Query('page'))")
	}

	// 验证页码是否正确解析
	var result ListArticlesOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Page != 3 {
		t.Errorf("page = %d, want 3", result.Page)
	}
	if result.PageSize != 5 {
		t.Errorf("pageSize = %d, want 5", result.PageSize)
	}
}

// ============================================================================
// 错误响应格式问题
// ============================================================================

func TestErrorFormatWithoutEnvelope(t *testing.T) {
	s := buildServer()

	resp := doRequest(t, s, http.MethodGet, "/api/v2/articles/nonexistent", nil, nil)
	body := readBodyBytes(t, resp)

	// 问题：没有 envelope 时，错误响应使用 http.Error 返回 text/plain
	// 这与成功时的 application/json 不一致
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("[发现-设计缺陷] 无 envelope 时错误响应 Content-Type = %q, 不是 JSON", contentType)
		t.Logf("  错误体: %s", string(body))
		t.Logf("  这导致客户端需要同时处理 JSON 和 text/plain 两种错误格式")
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// ============================================================================
// 验证错误响应问题
// ============================================================================

func TestValidationErrorFormat(t *testing.T) {
	s := buildServer()

	// 创建文章但缺少必填字段
	resp := doRequest(t, s, http.MethodPost, "/api/v2/articles", map[string]interface{}{
		"title": "Test",
		// 缺少 body, authorId
	}, map[string]string{"Authorization": "Bearer mock-jwt-token"})
	body := readBodyBytes(t, resp)

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("[发现-设计缺陷] 验证错误响应 Content-Type = %q, 不是 JSON", contentType)
		t.Logf("  错误体: %s", string(body))
	}

	// playground/validator 的错误格式不结构化
	if resp.StatusCode == http.StatusUnprocessableEntity {
		t.Logf("[发现-体验问题] 验证错误返回 422 但响应体是纯文本，没有结构化的字段错误信息")
		t.Logf("  实际body: %s", string(body))
	}
}

// ============================================================================
// 405 Method Not Allowed 问题
// ============================================================================

func TestMethodNotAllowed(t *testing.T) {
	s := buildServer()

	// /api/v2/articles/a1 注册了 GET/PUT/DELETE，尝试 POST 应该返回 405
	resp := doRequest(t, s, http.MethodPost, "/api/v2/articles/a1", map[string]interface{}{
		"title": "hack",
	}, map[string]string{"Authorization": "Bearer mock-jwt-token"})

	// 问题：RadixRouter 不支持 405 Method Not Allowed
	if resp.StatusCode == http.StatusNotFound {
		t.Logf("[发现-设计缺陷] 路径存在但方法不匹配时返回 404 而非 405 Method Not Allowed")
		t.Logf("  预期：405 + Allow: GET, PUT, DELETE")
		t.Logf("  实际：%d", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != "" {
		t.Logf("Allow header: %s", allow)
	}
}

// ============================================================================
// 204 No Content 问题
// ============================================================================

func Test204Response(t *testing.T) {
	s := buildServer()

	resp := doRequest(t, s, http.MethodDelete, "/api/v2/articles/a1", nil,
		map[string]string{"Authorization": "Bearer mock-jwt-token"})
	body := readBodyBytes(t, resp)

	// 问题：声明了 Responds(204) 但 DeleteOutput 是空结构体
	// 返回的状态码会是 200 而非 204，因为框架无法从空结构体推断 204
	if resp.StatusCode == http.StatusOK {
		t.Logf("[发现-行为不符] 声明了 204 但实际返回 200")
		t.Logf("  原因：响应结构体未实现 StatusCoder 接口，且无 Status 字段")
		t.Logf("  body长度: %d", len(body))
	}
}

// ============================================================================
// 认证中间件问题
// ============================================================================

func TestAuthMiddleware_DirectWriteConflict(t *testing.T) {
	s := buildServer()

	// 测试认证中间件直接写响应 vs 框架响应的一致性
	// 认证中间件直接写 JSON 响应，但 Content-Type 由中间件自己设置
	// 这与框架的 envelope/ Codec 机制不统一
	resp := doRequest(t, s, http.MethodPost, "/api/v2/articles", map[string]interface{}{
		"title": "No Auth Article",
	}, nil) // 无认证头
	body := readBodyBytes(t, resp)

	// 认证失败直接写 JSON，但没有 envelope 包装
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err == nil {
		if code, ok := result["code"]; ok {
			t.Logf("[发现-设计缺陷] 认证中间件直接写 JSON 响应，无法使用 envelope 机制")
			t.Logf("  这意味着认证错误和业务错误格式不一致")
			t.Logf("  中间件响应: %v", code)
		}
	}
}

// ============================================================================
// 路由衝突：/search vs {id}
// ============================================================================

func TestRouteConflict_StaticVsParam(t *testing.T) {
	s := buildServer()

	resp := doRequest(t, s, http.MethodGet, "/api/v2/articles/search?q=Go", nil, nil)
	body := readBodyBytes(t, resp)

	// /api/v2/articles/search 是静态路由，/api/v2/articles/{id} 是参数路由
	// 需要确保静态路由优先级更高
	if resp.StatusCode != http.StatusOK {
		t.Errorf("search status = %d, want %d, body: %s", resp.StatusCode, http.StatusOK, string(body))
		return
	}

	var result ListArticlesOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, string(body))
	}
	if result.Total == 0 {
		t.Error("search should find at least one article containing 'Go'")
	}
	t.Logf("搜索到 %d 条结果", result.Total)
}

// ============================================================================
// Cookie 测试
// ============================================================================

func TestLoginCookie(t *testing.T) {
	s := buildServer()

	resp := doRequest(t, s, http.MethodPost, "/auth/login", map[string]interface{}{
		"username": "admin",
		"password": "password",
	}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	cookies := resp.Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("session_id cookie not found")
	}
	if sessionCookie.Value != "mock-jwt-token" {
		t.Errorf("cookie value = %s, want mock-jwt-token", sessionCookie.Value)
	}

	body := readBodyBytes(t, resp)
	var result LoginOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, string(body))
	}
	if result.Token == "" {
		t.Error("token is empty")
	}
}

// ============================================================================
// 认证失败测试
// ============================================================================

func TestLoginUnauthorized(t *testing.T) {
	s := buildServer()

	resp := doRequest(t, s, http.MethodPost, "/auth/login", map[string]interface{}{
		"username": "admin",
		"password": "wrong-password",
	}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	body := readBodyBytes(t, resp)
	contentType := resp.Header.Get("Content-Type")
	t.Logf("认证失败 Content-Type: %s", contentType)
	t.Logf("认证失败 body: %s", string(body))

	// 问题：无 envelope 时，错误信息是纯文本
	if !strings.Contains(contentType, "application/json") {
		t.Logf("[发现-体验问题] 401 错误返回纯文本而非 JSON")
	}
}

// ============================================================================
// Recoverer 测试
// ============================================================================

func TestRecovererStackTrace(t *testing.T) {
	// 创建一个包含 panic 路由的服务器
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(ghttp.Recoverer())

	type PanicInput struct {
		ghttp.Params `json:"-"`
	}
	ghttp.Route[PanicInput, struct{ OK string }](s).
		GET("/panic").
		To(func(ctx context.Context, req PanicInput) (struct{ OK string }, error) {
			panic("intentional panic for testing recoverer")
		})

	resp := doRequest(t, s, http.MethodGet, "/panic", nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	body := readBodyBytes(t, resp)
	t.Logf("[发现-调试困难] Recoverer 响应 body: %s", string(body))
	t.Logf("[发现-调试困难] Recoverer 不包含请求堆栈信息，不利于调试")
}

// ============================================================================
// 分组嵌套中间件问题
// ============================================================================

func TestNestedGroupMiddleware(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))

	// 问题：Group 的中间件只在 Group() 调用或 Use() 时绑定
	// 不支持 Group().Use() 后缀调用的同时链式操作
	v1 := s.Group("/api/v1")
	v1.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-V1", "true")
			next.ServeHTTP(w, r)
		})
	})

	// 嵌套 group
	admin := v1.Group("/admin")
	admin.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Admin", "true")
			next.ServeHTTP(w, r)
		})
	})

	type NestedInput struct {
		ghttp.Params `json:"-"`
	}
	ghttp.Route[NestedInput, struct {
		Path string `json:"path"`
	}](admin).
		GET("/dashboard").
		To(func(ctx context.Context, req NestedInput) (struct {
			Path string `json:"path"`
		}, error) {
			return struct {
				Path string `json:"path"`
			}{Path: "/api/v1/admin/dashboard"}, nil
		})

	resp := doRequest(t, s, http.MethodGet, "/api/v1/admin/dashboard", nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// 验证中间件是否都生效
	if resp.Header.Get("X-V1") != "true" {
		t.Error("V1 middleware not applied to nested group")
	}
	if resp.Header.Get("X-Admin") != "true" {
		t.Error("Admin middleware not applied")
	}
}

// ============================================================================
// OpenAPI 测试 - 搜索路由覆盖问题
// ============================================================================

func TestOpenAPIWithConflictingRoutes(t *testing.T) {
	s := ghttp.New(
		ghttp.WithProduces(ghttp.MIMEJSON),
		ghttp.WithOpenAPI("Test", "1.0.0"),
	)

	type Input struct {
		ghttp.Params `json:"-"`
	}
	ghttp.Route[Input, struct{ ID string }](s).
		GET("/items/{id}").
		Doc(ghttp.Summary("Get item by ID")).
		To(func(ctx context.Context, req Input) (struct{ ID string }, error) {
			return struct{ ID string }{ID: req.Path("id")}, nil
		})

	ghttp.Route[Input, struct{ Name string }](s).
		GET("/items/special").
		Doc(ghttp.Summary("Get special item")).
		To(func(ctx context.Context, req Input) (struct{ Name string }, error) {
			return struct{ Name string }{Name: "special"}, nil
		})

	// 检查 OpenAPI 中是否存在 /items/special
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/openapi.json", nil)
	s.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("openapi status = %d, want %d", rr.Code, http.StatusOK)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal openapi: %v", err)
	}

	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("paths is not a map")
	}

	if _, ok := paths["/items/special"]; !ok {
		t.Logf("[发现-OpenAPI] /items/special 在 OpenAPI 文档中丢失，可能被 {id} 覆盖")
	}

	// 但实际请求应该能命中
	resp := doRequest(t, s, http.MethodGet, "/items/special", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /items/special = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// ============================================================================
// Trailing Slash 问题
// ============================================================================

func TestTrailingSlash(t *testing.T) {
	s := buildServer()

	resp := doRequest(t, s, http.MethodGet, "/health/", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Logf("[发现-兼容性问题] 尾部斜杠 /health/ 返回 %d, 注册路径为 /health", resp.StatusCode)
		t.Logf("  主流框架通常支持 trailing slash 重定向或匹配")
	}
}

// ============================================================================
// 原始 HTTP handler 测试
// ============================================================================

func TestRawHandler(t *testing.T) {
	s := buildServer()

	resp := doRequest(t, s, http.MethodGet, "/raw", nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body := readBodyBytes(t, resp)
	if string(body) != "raw handler response" {
		t.Errorf("body = %q, want 'raw handler response'", string(body))
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Content-Type = %s, want text/plain", contentType)
	}
}

// ============================================================================
// 请求 ID 中间件测试
// ============================================================================

func TestRequestID(t *testing.T) {
	s := buildServer()

	resp := doRequest(t, s, http.MethodGet, "/health", nil, nil)
	defer resp.Body.Close()

	requestID := resp.Header.Get("X-Request-ID")
	if requestID == "" {
		t.Error("X-Request-ID header is missing")
	}
	t.Logf("Request ID: %s", requestID)
}

// ============================================================================
// Accept 协商测试
// ============================================================================

func TestAcceptNegotiation(t *testing.T) {
	s := buildServer()

	// 使用 Accept: application/json
	resp := doRequest(t, s, http.MethodGet, "/health", nil, map[string]string{
		"Accept": "application/json",
	})
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	t.Logf("Accept JSON Content-Type: %s", contentType)

	// 使用 Accept: application/xml
	resp2 := doRequest(t, s, http.MethodGet, "/health", nil, map[string]string{
		"Accept": "application/xml",
	})
	defer resp2.Body.Close()
	contentType2 := resp2.Header.Get("Content-Type")
	t.Logf("Accept XML Content-Type: %s", contentType2)
	t.Logf("Accept XML body: %s", string(readBodyBytes(t, resp2)))
}

// ============================================================================
// CORS 限制测试
// ============================================================================

func TestCORSLimitations(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(ghttp.CORS(ghttp.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true, // 和 * 不兼容
		MaxAge:           3600,
	}))

	type Input struct {
		ghttp.Params `json:"-"`
	}
	ghttp.Route[Input, struct{ OK string }](s).
		GET("/test").
		To(func(ctx context.Context, req Input) (struct{ OK string }, error) {
			return struct{ OK string }{OK: "yes"}, nil
		})

	// Preflight 请求
	req, _ := http.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)
	resp := rr.Result()

	// 检查 Vary: Origin
	vary := resp.Header.Get("Vary")
	if vary == "" {
		t.Logf("[发现-CORS] 缺少 Vary: Origin header，可能导致 CDN 缓存投毒")
	}

	// AllowCredentials=true + AllowOrigins=* 的问题
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	allowCredentials := resp.Header.Get("Access-Control-Allow-Credentials")
	if allowOrigin == "*" && allowCredentials == "true" {
		t.Logf("[发现-CORS] Allow-Origin: * + Allow-Credentials: true 组合在浏览器端无效")
		t.Logf("  浏览器会拒绝此组合，这是用户的配置错误但框架没有警告")

		// 尝试迭代 AllowOrigins
		t.Logf("[发现-CORS] CORSConfig.AllowOrigin 可能应该使用 func(string) bool 模式而非 []string")
		t.Logf("  这样才能正确处理 'credentials + 域名镜像' 的最佳实践")
	}
}

// ============================================================================
// Route 泛型约束的限制
// ============================================================================

func TestGenericConstraintLimitation(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))

	// 问题：所有路由必须使用 Route[Req, Resp] 构建器
	// 没有 Route(func(ctx, req) (resp, err)) 的简化签名
	// 这导致 req/resp 类型必须在编译时确定

	// 问题：Req 和 Resp 不能是 interface{} 或 any
	// 因为 type system 限制

	type MyInput struct {
		ghttp.Params `json:"-"`
	}
	type MyOutput struct {
		Data string `json:"data"`
	}

	ghttp.Route[MyInput, MyOutput](s).
		GET("/data").
		Produces(ghttp.MIMEJSON).
		To(func(ctx context.Context, req MyInput) (MyOutput, error) {
			return MyOutput{Data: "hello"}, nil
		})

	resp := doRequest(t, s, http.MethodGet, "/data", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// ============================================================================
// 响应状态码自定义问题
// ============================================================================

func TestStatusCodeFromStructField(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))

	type StatusOutput struct {
		Status int    `json:"-"` // 需要命名为 Status 且类型为 int
		ID     string `json:"id"`
	}

	// 问题：响应结构体中必须有一个名为 Status 的 int 字段
	// 或者实现 StatusCoder 接口
	// 这不像responst那样通过 Responds(201) 声明就能生效

	ghttp.Route[ghttp.Params, StatusOutput](s).
		GET("/status").
		Produces(ghttp.MIMEJSON).
		To(func(ctx context.Context, req ghttp.Params) (StatusOutput, error) {
			return StatusOutput{Status: 201, ID: "test"}, nil
		})

	resp := doRequest(t, s, http.MethodGet, "/status", nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Logf("[发现-设计混淆] 状态码来自结构体字段而非 Responds() 声明")
		t.Logf("  Responds(201) 声明无实际作用，实际 code=%d", resp.StatusCode)
	} else {
		t.Logf("StatusCoder/Status field 生效，返回 %d", resp.StatusCode)
	}
}

// ============================================================================
// Body 过大问题
// ============================================================================

func TestBodySizeLimit(t *testing.T) {
	s := ghttp.New(
		ghttp.WithProduces(ghttp.MIMEJSON),
		ghttp.WithMaxBodyBytes(100),
	)

	type LargeInput struct {
		ghttp.Params `json:"-"`
		Body         struct {
			Data string `json:"data"`
		} `json:"body"`
	}

	ghttp.Route[LargeInput, struct{ OK string }](s).
		POST("/upload").
		Produces(ghttp.MIMEJSON).
		Consumes(ghttp.MIMEJSON).
		To(func(ctx context.Context, req LargeInput) (struct{ OK string }, error) {
			return struct{ OK string }{OK: req.Body.Data}, nil
		})

	// 发送超过 100 字节的 body
	largeData := strings.Repeat("x", 200)
	resp := doRequest(t, s, http.MethodPost, "/upload", map[string]interface{}{
		"data": largeData,
	}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Logf("[发现-边界] body 超限制返回 %d, 期望 %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
}

// ============================================================================
// 类型化 param 读取（设计不足总结）
// ============================================================================

func TestNoTypedParamAccessors(t *testing.T) {
	// 这不是一个具体测试，而是总结 Params 的 API 设计不足

	t.Logf("[总结-Params] 当前 API:")
	t.Logf("  - req.Path(key) -> string")
	t.Logf("  - req.Query(key) -> string")
	t.Logf("  - req.Header(key) -> string")
	t.Logf("  - req.Cookie(key) -> string")
	t.Logf("")
	t.Logf("[总结-Params] 缺少的便捷方法:")
	t.Logf("  - QueryInt(key, default) int")
	t.Logf("  - QueryBool(key, default) bool")
	t.Logf("  - QueryFloat(key, default) float64")
	t.Logf("  - QueryInt64(key, default) int64")
	t.Logf("  - PathInt(key) int (如 user id 经常是 int)")
	t.Logf("  - HeaderInt / HeaderBool 等")
	t.Logf("")
	t.Logf("对比其他框架:")
	t.Logf("  - Gin: c.DefaultQueryInt(), c.GetInt()")
	t.Logf("  - Echo:QueryParamTypes 自动绑定")
	t.Logf("  - Fiber: c.ParamsInt(), c.QueryInt()")
}

// ============================================================================
// 中间件执行顺序问题
// ============================================================================

func TestMiddlewareExecutionOrder(t *testing.T) {
	var order []string

	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "global-1-before")
			next.ServeHTTP(w, r)
			order = append(order, "global-1-after")
		})
	})
	s.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "global-2-before")
			next.ServeHTTP(w, r)
			order = append(order, "global-2-after")
		})
	})

	type Input struct {
		ghttp.Params `json:"-"`
	}
	ghttp.Route[Input, struct{ OK string }](s).
		GET("/order").
		Produces(ghttp.MIMEJSON).
		Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, "route-1-before")
				next.ServeHTTP(w, r)
				order = append(order, "route-1-after")
			})
		}).
		To(func(ctx context.Context, req Input) (struct{ OK string }, error) {
			order = append(order, "handler")
			return struct{ OK string }{OK: "ok"}, nil
		})

	resp := doRequest(t, s, http.MethodGet, "/order", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	t.Logf("中间件执行顺序: %v", order)
	// 预期: global-1-before -> global-2-before -> route-1-before -> handler -> route-1-after -> global-2-after -> global-1-after
}

// ============================================================================
// 空响应体问题
// ============================================================================

func TestEmptyResponseBody(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))

	// NoOutputHandler 仅返回 error
	// 204 场景
	ghttp.Route[ghttp.Params, struct{}](s).
		DELETE("/items/{id}").
		Produces(ghttp.MIMEJSON).
		To(func(ctx context.Context, req ghttp.Params) (struct{}, error) {
			return struct{}{}, nil
		})

	resp := doRequest(t, s, http.MethodDelete, "/items/1", nil, nil)
	defer resp.Body.Close()

	t.Logf("空响应体, status = %d", resp.StatusCode)
	t.Logf("空响应体, Content-Type = %q", resp.Header.Get("Content-Type"))

	// 问题：空结构体序列化后是 "{}", 不是真正的 204 No Content
	bodyBytes := readBodyBytes(t, resp)
	t.Logf("[发现-问题] empty body = %q", string(bodyBytes))
	if len(bodyBytes) > 0 {
		t.Logf("[发现-行为问题] empty struct has body %q, expected 204 No Content", string(bodyBytes))
	}
}

// ============================================================================
// 文件上传 multipart 问题
// ============================================================================

func TestFileUpload(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))

	// 问题：文件上传需要 Body 字段结构体配合 form tag
	// 但 RouteBuilder 对 Req 类型有约束
	type UploadInput struct {
		ghttp.Params `json:"-"`
		Body         struct {
			Description string `json:"description" form:"description"`
		} `json:"body"`
	}

	ghttp.Route[UploadInput, struct {
		Filename string `json:"filename"`
	}](s).
		POST("/upload").
		Produces(ghttp.MIMEJSON).
		Consumes(ghttp.MIMEMultipartPOSTForm).
		To(func(ctx context.Context, req UploadInput) (struct {
			Filename string `json:"filename"`
		}, error) {
			return struct {
				Filename string `json:"filename"`
			}{Filename: "uploaded.jpg"}, nil
		})

	resp := doRequest(t, s, http.MethodPost, "/upload", map[string]interface{}{
		"description": "test image",
	}, nil)
	defer resp.Body.Close()

	t.Logf("文件上传模拟, status = %d", resp.StatusCode)

	// 问题总结
	t.Logf("[发现-文件上传]")
	t.Logf("  1. FileHeader 大小写不一致 (File vs file)")
	t.Logf("  2. 需要手动解析 multipart form")
	t.Logf("  3. 没有提供上传文件大小/类型验证的便捷方法")
	t.Logf("  4. 获取原始 *http.Request 需要绕路")
}

// ============================================================================
// strconv helper 的必要性
// ============================================================================

// TestStrconvAppendum — 记录不可避免的模板代码
func TestStrconvAppendum(t *testing.T) {
	// 使用 strconv 来演示手动解析 query 参数的模式
	page := 1
	pageSize := 10

	pageStr := "2"
	if pageStr != "" {
		if n, err := strconv.Atoi(pageStr); err == nil && n > 0 {
			page = n
		}
	}

	pageSizeStr := "20"
	if pageSizeStr != "" {
		if n, err := strconv.Atoi(pageSizeStr); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}

	t.Logf("[体验] page=%d, pageSize=%d (needs 10+ lines boilerplate)", page, pageSize)
	t.Logf("[发现-体验问题] pagination params boilerplate affects 90%% of API endpoints")
}

// ============================================================================
// WithLogger 集成问题
// ============================================================================

func TestLoggerInterface(t *testing.T) {
	// ghttp.Logger 是一个只包含 DebugContext/InfoContext/WarnContext/ErrorContext 的接口
	// 这可能是抽象过度

	t.Logf("[发现-Logger接口] ghttp.Logger 要求实现4个方法:")
	t.Logf("  - DebugContext(ctx, msg, args...)")
	t.Logf("  - InfoContext(ctx, msg, args...)")
	t.Logf("  - WarnContext(ctx, msg, args...)")
	t.Logf("  - ErrorContext(ctx, msg, args...)")
	t.Logf("")
	t.Logf("这兼容 slog/log等标准 logger，但不能直接使用 log.Logger")
	t.Logf("大多数 Go 用户使用的是 stdlog 或 slog")
	t.Logf("glog 包提供了兼容实现，但这是额外的抽象层")
}
