package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"

	"strings"
	"testing"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

// ============================================================================
// 辅助函数
// ============================================================================

func doReq(t *testing.T, ts *httptest.Server, method, path string, body interface{}) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, ts.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return data
}

// ============================================================================
// 问题1: 创建资源返回 201 — StatusCoder 是否正常工作？
// ============================================================================

func Test_Created_StatusCode(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodPost, "/api/v1/users", map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
		"age":   30,
	})
	defer resp.Body.Close()

	// 预期 201 Created
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("【问题1】创建资源返回 %d，期望 201。StatusCoder 接口虽已实现，但使用不够直观，需要自定义结构体实现 StatusCode() 方法", resp.StatusCode)
	}
}

// ============================================================================
// 问题2: Path 参数只有 string 类型，缺少 PathInt/PathInt64 等类型化方法
// ============================================================================

func Test_Path_Params_All_Strings(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/api/v1/users/42", nil)
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.Unmarshal(readBody(t, resp), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 验证 path 参数传递成功
	userMap, ok := result["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("response has no user field: %v", result)
	}
	if userMap["id"] != "42" {
		t.Errorf("id = %v, want 42", userMap["id"])
	}

	t.Logf("【问题2】Path() 只返回 string，对于 /users/42 这种 ID 需要手动 strconv.Atoi 转换。应提供 PathInt/PathInt64/PathBool 等便捷方法")
}

// ============================================================================
// 问题3: Query 参数只有 string 类型，缺少 QueryInt/QueryBool 等
// ============================================================================

func Test_Query_Params_All_Strings(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/api/v1/users?page=2", nil)
	defer resp.Body.Close()

	var result ListUsersResp
	if err := json.Unmarshal(readBody(t, resp), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result.Page != 2 {
		t.Errorf("page = %d, want 2", result.Page)
	}

	t.Logf("【问题3】Query() 只返回 string，分页场景需要手动 strconv。应提供 QueryInt/QueryInt64/QueryBool/QueryFloat 等便捷方法")
}

// ============================================================================
// 问题4: 缺少内置 query 参数验证机制
// ============================================================================

func Test_Query_Validation(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/api/v1/users/search", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Logf("【问题4】缺少内置的 query 参数验证机制。当前只能手动检查 req.Query(\"q\") == \"\" 并返回 BadRequest，无法在结构体中用 validate tag 声明式验证 query 参数")
	}
}

// ============================================================================
// 问题5: 简单场景下泛型冗余 — struct{} 重复书写
// ============================================================================

func Test_Simple_Route_Verbosity(t *testing.T) {
	t.Logf("【问题5】健康检查等简单路由的写法极其冗余：Route[struct{}, struct{Status string `json:\"status\"`}](s).GET(\"/health\").To(func(ctx context.Context, req struct{}) (struct{Status string `json:\"status\"`}, error) { ... })。")
	t.Logf("  - 即使不需要输入，也必须写 struct{}")
	t.Logf("  - 内联结构体写法导致类型签名非常长")
	t.Logf("  - 应考虑提供不需要泛型的快捷注册方式，如 s.GET(\"/health\", handler)")
}

// ============================================================================
// 问题6: Group.Use() 在路由注册后不生效
// ============================================================================

func Test_Group_Use_After_Registration(t *testing.T) {
	var called bool
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	api := s.Group("/api")

	// 先注册路由
	ghttp.Route[ghttp.Params, struct {
		OK bool `json:"ok"`
	}](api).
		GET("/test").
		To(func(ctx context.Context, req ghttp.Params) (struct {
			OK bool `json:"ok"`
		}, error) {
			return struct {
				OK bool `json:"ok"`
			}{OK: true}, nil
		})

	// 路由注册后添加中间件
	api.Use(mw)

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/api/test", nil)
	defer resp.Body.Close()

	if !called {
		t.Logf("【问题6】Group.Use() 在路由注册之后调用不会生效。这是因为路由注册时 handler chain 已构建完毕。这是一个非常容易踩的坑，且没有编译时或运行时警告")
	}
}

// ============================================================================
// 问题7: HandlerFunc 签名无法获取 *http.Request
// ============================================================================

func Test_Handler_Cannot_Access_HttpRequest(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/api/v1/echo-host", nil)
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.Unmarshal(readBody(t, resp), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result["host"] == "unavailable" {
		t.Logf("【问题7】HandlerFunc[Req, Resp] 签名只有 (ctx, input)，无法直接获取 *http.Request。")
		t.Logf("  很多场景需要读取原始请求（如 webhook 签名验证、读取客户端 IP、获取请求头等），只能退而求其次使用 ToRaw/ToHTTPFunc")
		t.Logf("  - 通过 Params 间接获取部分信息不够用")
		t.Logf("  - 通过 ctx 存储需要自定义中间件，增加使用成本")
	}
}

// ============================================================================
// 问题8: 无 envelope 时错误响应格式不一致
// ============================================================================

func Test_Error_Response_Format_Without_Envelope(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/api/v1/users/999", nil)
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("【问题8】无 envelope 时错误响应 Content-Type = %q，不是 application/json。writeError() 使用 http.Error() 返回 text/plain，与成功响应的 JSON 格式不一致", contentType)
		t.Logf("  这导致客户端必须根据 Content-Type 分别解析成功和失败的响应，增加复杂度")
	}
}

// ============================================================================
// 问题8续: 有 envelope 时错误响应格式
// ============================================================================

func Test_Error_Response_Format_With_Envelope(t *testing.T) {
	s := buildEnvelopeServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/ok", nil)
	defer resp.Body.Close()

	body := readBody(t, resp)
	var okResult map[string]interface{}
	if err := json.Unmarshal(body, &okResult); err != nil {
		t.Fatalf("unmarshal ok: %v", err)
	}
	// DefaultEnvelope 成功时: {code: int, msg: string, data: any}
	if _, hasCode := okResult["code"]; !hasCode {
		t.Logf("DefaultEnvelope 成功响应缺少 code 字段: %v", okResult)
	}

	resp2 := doReq(t, ts, http.MethodGet, "/err", nil)
	defer resp2.Body.Close()

	body2 := readBody(t, resp2)
	var errResult map[string]interface{}
	if err := json.Unmarshal(body2, &errResult); err != nil {
		t.Fatalf("unmarshal err: %v", err)
	}
	if _, hasCode := errResult["code"]; !hasCode {
		t.Logf("DefaultEnvelope 错误响应缺少 code 字段: %v", errResult)
	}
}

// ============================================================================
// 问题9: 不支持单次注册多种方法
// ============================================================================

func Test_Multiple_Methods_Same_Path(t *testing.T) {
	t.Logf("【问题9】RouteBuilder 不支持在同一路径上注册多种方法使用同一 handler。")
	t.Logf("  例如想对 /users 同时注册 GET 和 POST，必须写两次 Route[...](s).GET(\"/users\").To(...) 和 Route[...](s).POST(\"/users\").To(...)")
	t.Logf("  而 ANY() 又会注册所有 HTTP 方法，不够精细")
	t.Logf("  建议增加 .Methods(\"GET\",\"POST\") 或链式 .GET().POST() 支持")
}

// ============================================================================
// 问题10: 不支持 query 参数自动绑定到结构体字段
// ============================================================================

func Test_Query_Param_Binding(t *testing.T) {
	t.Logf("【问题10】当前输入结构体只支持 Body（请求体）+ Params（path/query/header/cookie 的字符串字典访问）。")
	t.Logf("  不支持将 query 参数自动绑定到结构体字段，如：type ListReq struct { Page int `query:\"page\"`; Size int `query:\"size\"` }")
	t.Logf("  这意味着每个 query 参数都需要手动解析和类型转换，大量重复代码")
	t.Logf("  Gin 的 ShouldBindQuery、Echo 的 Bind 都支持此功能")
}

// ============================================================================
// 问题11: CORS 中间件缺少 Vary: Origin 头和 credentials 安全检查
// ============================================================================

func Test_CORS_Safety(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/v1/users", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request: %v", err)
	}
	defer resp.Body.Close()

	// 问题11a: 缺少 Vary: Origin
	if resp.Header.Get("Vary") == "" {
		t.Logf("【问题11a】CORS 响应缺少 Vary: Origin header。浏览器缓存可能将无 Origin 的请求与有 Origin 的请求混淆，导致缓存投毒")
	}

	// 问题11b: AllowCredentials=true 时 AllowOrigins=["*"] 不安全
	// 这在 CORS 规范中明确禁止，但 ghttp 的 CORS 中间件不检查此组合
}

func Test_CORS_Credentials_With_Wildcard(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(ghttp.CORS(ghttp.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowCredentials: true, // 不安全！浏览器会拒绝
	}))
	ghttp.Route[struct{}, struct {
		OK bool `json:"ok"`
	}](s).
		GET("/test").
		To(func(ctx context.Context, req struct{}) (struct {
			OK bool `json:"ok"`
		}, error) {
			return struct {
				OK bool `json:"ok"`
			}{OK: true}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/test", nil)
	req.Header.Set("Origin", "https://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	creds := resp.Header.Get("Access-Control-Allow-Credentials")

	if origin == "*" && creds == "true" {
		t.Logf("【问题11b】AllowCredentials=true + AllowOrigins=[\"*\"] 组合不安全，浏览器会拒绝。CORS 中间件应在配置时检查并拒绝此组合，或在运行时回退为不发送 credentials")
	}
}

// ============================================================================
// 问题12: 静态文件不支持 embed.FS
// ============================================================================

func Test_Static_Files_No_Embed(t *testing.T) {
	t.Logf("【问题12】ToStatic 只支持文件系统路径或 http.FileSystem。")
	t.Logf("  不支持 Go 1.16+ 的 embed.FS（需要适配 fs.FS 到 http.FileSystem）")
	t.Logf("  对于想将静态资源嵌入二进制的场景，需要用户自行实现适配器")
}

// ============================================================================
// 问题13: 路由 /users/{id} 和 /users/search 的优先级问题
// ============================================================================

func Test_Route_Priority_Static_Vs_Param(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 先注册 /users/{id}，再注册 /users/search
	// 访问 /users/search 时，RadixRouter 应优先匹配静态路径
	resp := doReq(t, ts, http.MethodGet, "/api/v1/users/search?q=test", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Logf("【问题13】/users/search 被 /users/{id} 拦截，返回 %d。路由匹配优先级不清晰，缺少文档说明静态路由和参数路由的匹配顺序", resp.StatusCode)
	} else {
		var result SearchResp
		if err := json.Unmarshal(readBody(t, resp), &result); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if result.Query != "test" {
			t.Logf("search 路由匹配成功但 query 不对: %v", result)
		}
	}
}

// ============================================================================
// 问题14: DELETE 返回 200 而非 204 — 空响应体的默认状态码
// ============================================================================

func Test_Empty_Response_Status_Code(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[ghttp.Params, struct{}](s).
		DELETE("/items/{id}").
		Doc(ghttp.Summary("删除项目")).
		To(func(ctx context.Context, req ghttp.Params) (struct{}, error) {
			return struct{}{}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodDelete, "/items/1", nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Logf("【问题14】DELETE 返回 200 而非 204。空结构体的默认状态码应为 204 No Content，或至少提供一种声明式方式指定成功状态码而不需要实现 StatusCoder 接口")
	}
}

// ============================================================================
// 问题15: 重复路由注册被静默覆盖
// ============================================================================

func Test_Duplicate_Route_Silent_Override(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[struct{}, struct {
		V int `json:"v"`
	}](s).
		GET("/dup").
		To(func(ctx context.Context, req struct{}) (struct {
			V int `json:"v"`
		}, error) {
			return struct {
				V int `json:"v"`
			}{V: 1}, nil
		})
	ghttp.Route[struct{}, struct {
		V int `json:"v"`
	}](s).
		GET("/dup").
		To(func(ctx context.Context, req struct{}) (struct {
			V int `json:"v"`
		}, error) {
			return struct {
				V int `json:"v"`
			}{V: 2}, nil
		})

	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate route setup panic")
		}
	}()
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/dup", nil))
}

// ============================================================================
// 问题16: RadixRouter 405 时不返回 Allow header
// ============================================================================

func Test_MethodNotAllowed_No_Allow_Header(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[struct{}, struct{}](s).
		GET("/only-get").
		To(func(ctx context.Context, req struct{}) (struct{}, error) {
			return struct{}{}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodPost, "/only-get", nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMethodNotAllowed {
		allow := resp.Header.Get("Allow")
		if allow == "" {
			t.Logf("【问题16】405 Method Not Allowed 响应缺少 Allow header。HTTP 规范要求 405 响应必须包含 Allow header 列出支持的请求方法")
		}
	} else if resp.StatusCode == http.StatusNotFound {
		t.Logf("【问题16】路径存在但方法不匹配时返回 404 而非 405 Method Not Allowed。RadixRouter 按方法分组，方法不匹配时不会去其他方法组查找，直接返回 404")
	}
}

// ============================================================================
// 问题17: GET 路由不自动支持 HEAD
// ============================================================================

func Test_HEAD_Not_Auto_Supported(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[struct{}, struct {
		Message string `json:"message"`
	}](s).
		GET("/head-test").
		To(func(ctx context.Context, req struct{}) (struct {
			Message string `json:"message"`
		}, error) {
			return struct {
				Message string `json:"message"`
			}{Message: "hello"}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodHead, ts.URL+"/head-test", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotFound {
		t.Logf("【问题17】GET 路由不自动支持 HEAD 请求，返回 %d。HTTP 规范说明 HEAD 应与 GET 等价但无响应体，框架应自动支持", resp.StatusCode)
	}
}

// ============================================================================
// 问题18: Timeout 中间件只是设了 ctx 超时，不中断处理
// ============================================================================

func Test_Timeout_Middleware_Behavior(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(ghttp.Timeout(50 * time.Millisecond))
	ghttp.Route[struct{}, struct{}](s).
		GET("/slow").
		To(func(ctx context.Context, req struct{}) (struct{}, error) {
			select {
			case <-time.After(200 * time.Millisecond):
				return struct{}{}, nil
			case <-ctx.Done():
				return struct{}{}, ctx.Err()
			}
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/slow")
	if err != nil {
		t.Fatalf("slow request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Logf("【问题18】Timeout 中间件只设置 context 超时，不自动返回 504 Gateway Timeout。handler 必须自己检查 ctx.Done()，否则超时后仍正常返回。不直观且容易忘记处理")
	}
}

// ============================================================================
// 问题19: 请求体验证失败时的错误响应不是 JSON
// ============================================================================

func Test_Validation_Error_Response_Format(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodPost, "/api/v1/users", map[string]interface{}{
		"name": "Alice",
		// 缺少必填的 email
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Logf("验证失败状态码 = %d, 期望 422", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("【问题19】验证错误响应 Content-Type = %q，不是 application/json。框架内部错误（验证失败、解析失败等）应统一返回 JSON 格式，与业务错误保持一致", contentType)
	}
}

// ============================================================================
// 问题20: 无法在不实现 StatusCoder 的情况下声明路由的成功状态码
// ============================================================================

func Test_Declared_Responds_Not_Affecting_Actual_Status(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[struct{}, struct {
		ID string `json:"id"`
	}](s).
		POST("/items").
		Doc(ghttp.Summary("创建项目")).
		To(func(ctx context.Context, req struct{}) (struct {
			ID string `json:"id"`
		}, error) {
			return struct {
				ID string `json:"id"`
			}{ID: "new-item"}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodPost, "/items", nil)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Logf("【问题20】声明了 Responds(201) 但实际返回 200。Responds() 只影响 OpenAPI 文档生成，不影响实际运行时行为。这是一个语义陷阱——用户自然会期望声明了 201 就会返回 201")
	}
}

// ============================================================================
// 问题21: Recoverer 返回 text/plain 而非 JSON
// ============================================================================

func Test_Recoverer_Response_Format(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(ghttp.Recoverer())
	ghttp.Route[ghttp.Params, struct{}](s).
		GET("/panic").
		To(func(ctx context.Context, req ghttp.Params) (struct{}, error) {
			panic("intentional panic")
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/panic", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("panic status = %d, want 500", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("【问题21】Recoverer panic 恢复后返回 Content-Type = %q，不是 application/json。框架级错误响应格式应与成功响应一致（在 WithProduces 为 JSON 时）", contentType)
	}
}

// ============================================================================
// 问题22: 文件上传 Body 结构体设计不直观
// ============================================================================

func Test_File_Upload_API(t *testing.T) {
	// 尝试构建一个文件上传路由
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[UploadReq, struct {
		Desc string `json:"desc"`
	}](s).
		POST("/upload").
		Consumes("multipart/form-data").
		Produces(ghttp.MIMEJSON).
		To(func(ctx context.Context, req UploadReq) (struct {
			Desc string `json:"desc"`
		}, error) {
			return struct {
				Desc string `json:"desc"`
			}{Desc: req.Body.Description}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	// 构建 multipart 请求
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("description", "test image")
	fw, _ := w.CreateFormFile("file", "test.png")
	_, _ = fw.Write([]byte("fake image data"))
	_ = w.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body := readBody(t, resp)
		t.Logf("【问题22】文件上传请求返回 %d，body: %s。文件上传 API 设计不够直观：需要在 Body 结构体中使用 form tag 绑定字段，但整个设计围绕 JSON 优先", resp.StatusCode, string(body))
	}
}

// ============================================================================
// 问题23: 无法从 handler 中设置响应头
// ============================================================================

func Test_Cannot_Set_Response_Headers(t *testing.T) {
	t.Logf("【问题23】HandlerFunc[Req, Resp] 的签名是 (ctx, input) -> (resp, error)，无法直接操作 http.ResponseWriter。")
	t.Logf("  如果需要设置自定义响应头（如 Cache-Control、X-Total-Count 等），只能：")
	t.Logf("  1. 通过 CookieWriter 接口设置 Set-Cookie（只支持 cookie）")
	t.Logf("  2. 退回 ToHTTPFunc/ToRaw 手动处理")
	t.Logf("  建议在响应结构体中支持 HeaderWriter 接口，类似 CookieWriter")
}

// ============================================================================
// 问题24: 没有路由命名的概念，无法通过名称生成 URL
// ============================================================================

func Test_No_Named_Routes(t *testing.T) {
	t.Logf("【问题24】没有路由命名的概念（如 OperationID 仅用于 OpenAPI 文档），无法通过路由名称反向生成 URL。")
	t.Logf("  在模板渲染、重定向等场景下，需要硬编码 URL 路径，容易出错")
}

// ============================================================================
// 问题25: 没有请求上下文值的标准注入方式
// ============================================================================

func Test_No_Standard_Context_Values(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/api/v1/users/1", nil)
	defer resp.Body.Close()

	if resp.Header.Get("X-Request-ID") == "" {
		t.Logf("【问题25】RequestID 中间件将 ID 写入响应头，但没有注入到 request context 中。handler 中无法通过标准方式获取 Request-ID。应提供一个标准的 ctx value 获取方法如 GetRequestID(ctx)")
	}
}

// ============================================================================
// 问题26: 没有 NotFound/MethodNotAllowed 自定义处理
// ============================================================================

func Test_No_Custom_NotFound_Handler(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/nonexistent", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("【问题26】404/405 响应由 RadixRouter 直接写入，无法自定义错误格式。没有 Server.NotFound(handler) 或 Server.MethodNotAllowed(handler) 的配置项。无法统一错误响应格式")
	}
}

// ============================================================================
// 问题27: 没有 Request 生命周期钩子
// ============================================================================

func Test_No_Lifecycle_Hooks(t *testing.T) {
	t.Logf("【问题27】缺少请求级别的生命周期钩子，如 OnRequestStart/OnRequestEnd/OnPanic。")
	t.Logf("  想在所有请求前后执行通用逻辑（如指标收集、链路追踪），只能通过中间件实现")
	t.Logf("  中间件虽灵活，但缺乏框架级的统一标准和约定")
}

// ============================================================================
// 问题28: WithProduces 和 WithConsumes 只在 Server 级别，缺少 Route 级别的默认值覆盖
// ============================================================================

// 注: RouteBuilder 已经有 .Produces() 和 .Consumes() 方法，所以这个问题不大
// 但 Group 级别的 produces/consumes 继承是正确的

// ============================================================================
// 问题29: Trailing slash 行为
// ============================================================================

func Test_Trailing_Slash(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/api/v1/users/", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Logf("【问题29】/api/v1/users/ (有尾部斜杠) 返回 %d，而 /api/v1/users (无尾部斜杠) 返回 200。Trailing slash 行为不一致或缺少自动重定向", resp.StatusCode)
	}
}

// ============================================================================
// 问题30: 无法注册一个 "fallback" 或 "catch-all" 处理器
// ============================================================================

func Test_No_CatchAll_Handler(t *testing.T) {
	t.Logf("【问题30】没有 catch-all 路由模式。RadixRouter 不支持 /api/* 或 /{path...} 的通配符匹配用于 SPA 前端回退。")
	t.Logf("  ToStaticFS 使用 /*path 但这是特殊处理，普通路由无法使用通配符")
}

// ============================================================================
// 问题31: OpenAPI 文档不支持从 validate tag 推导约束
// ============================================================================

func Test_OpenAPI_Validate_Tags(t *testing.T) {
	t.Logf("【问题31】OpenAPI schema 生成只识别 doc/required/minLength/maxLength 等自定义 tag，不识别 validate tag。")
	t.Logf("  例如 `validate:\"required,email\"` 不会生成 OpenAPI 的 required: true 和 format: email")
	t.Logf("  用户需要在字段上同时写 validate tag 和 required/doc tag，重复且容易不一致")
}

// ============================================================================
// 问题32: SSE 不支持写入 retry 字段
// ============================================================================

func Test_SSE_No_Retry(t *testing.T) {
	t.Logf("【问题32】SSEWriter 只有 WriteEvent 和 WriteJSON 方法，不支持写入 retry: 字段。")
	t.Logf("  SSE 规范允许服务端发送 retry: 指示客户端重连间隔，但当前 API 未暴露")
}

func Test_WebSocket_JSON_Echo(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))

	type message struct {
		Text string `json:"text"`
		Room string `json:"room"`
	}

	ghttp.Route[struct{}, struct{}](s).
		GET("/ws/{room}").
		ToWebSocket(func(ctx context.Context, params ghttp.Params, conn *ghttp.WebSocketConn) error {
			var in message
			if err := conn.ReadJSON(&in); err != nil {
				return err
			}
			in.Text = "echo:" + in.Text
			in.Room = params.Path("room")
			return conn.WriteJSON(in)
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	conn, err := ghttp.NewClient().WebSocket(strings.NewReplacer("http://", "ws://", "https://", "wss://").Replace(ts.URL + "/ws/review"))
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(message{Text: "hello"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var out message
	if err := conn.ReadJSON(&out); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if out.Text != "echo:hello" || out.Room != "review" {
		t.Fatalf("out = %#v, want echoed text and room", out)
	}
}

// ============================================================================
// 问题34: 没有 Server 级别的优雅重启支持
// ============================================================================

func Test_No_Graceful_Restart(t *testing.T) {
	t.Logf("【问题34】只有 Shutdown(ctx) 和 Close()，没有优雅重启（即在不中断服务的情况下重新加载配置或路由）。")
	t.Logf("  生产环境需要此功能实现零停机部署")
}

// ============================================================================
// 问题35: RequestLogger 中间件没有请求体大小日志
// ============================================================================

func Test_RequestLogger(t *testing.T) {
	t.Logf("【问题35】RequestLogger 只记录 method/path/status/duration，不记录请求体和响应体大小。")
	t.Logf("  对于监控和调试，缺少请求大小信息")
}

// ============================================================================
// 综合: 完整 CRUD 流程
// ============================================================================

func Test_CRUD_CreateAndGet(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 创建
	resp := doReq(t, ts, http.MethodPost, "/api/v1/users", map[string]interface{}{
		"name":  "Bob",
		"email": "bob@example.com",
		"age":   25,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body := readBody(t, resp)
		t.Errorf("create status = %d, want 201, body: %s", resp.StatusCode, string(body))
	}

	// 获取
	resp2 := doReq(t, ts, http.MethodGet, "/api/v1/users/1", nil)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("get status = %d, want 200", resp2.StatusCode)
	}
}

// ============================================================================
// 问题36: Body 解析默认 fallback 到 JSON，不根据 Content-Type 选择 codec
// ============================================================================

func Test_Body_Parsing_Default_JSON(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[struct {
		ghttp.Params `json:"-"`
		Body         struct {
			Name string `json:"name"`
		} `json:"body"`
	}, struct {
		Name string `json:"name"`
	}](s).
		POST("/xml-body").
		Consumes(ghttp.MIMEXML).
		To(func(ctx context.Context, req struct {
			ghttp.Params `json:"-"`
			Body         struct {
				Name string `json:"name"`
			} `json:"body"`
		}) (struct {
			Name string `json:"name"`
		}, error) {
			return struct {
				Name string `json:"name"`
			}{Name: req.Body.Name}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	// 发送 XML body
	xmlBody := `<Name>xml-test</Name>`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/xml-body", strings.NewReader(xmlBody))
	req.Header.Set("Content-Type", "application/xml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("xml request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.Unmarshal(readBody(t, resp), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if result["name"] != "xml-test" {
		t.Logf("【问题36】Consumes 声明了 application/xml 但 Body 解析可能未按 Content-Type 选择 codec。parseBody() 的 default 分支 fallback 到 JSON 解析")
	}
}

// ============================================================================
// 问题37: 没有全局的 panic 错误消息可定制化
// ============================================================================

func Test_Panic_Error_Message(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(ghttp.Recoverer())
	ghttp.Route[ghttp.Params, struct{}](s).
		GET("/panic").
		To(func(ctx context.Context, req ghttp.Params) (struct{}, error) {
			panic("sensitive internal error")
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodGet, "/panic", nil)
	defer resp.Body.Close()

	body := string(readBody(t, resp))
	if strings.Contains(body, "sensitive internal error") {
		t.Logf("【问题37】Recoverer 将 panic 消息直接返回给客户端，可能泄露敏感信息。生产环境应返回通用错误消息，将详细错误记录到日志")
	}
}

// ============================================================================
// 问题38: 验证错误未结构化返回
// ============================================================================

func Test_Validation_Error_Structure(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodPost, "/api/v1/users", map[string]interface{}{
		"name": "Alice",
		// 缺少 email
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Logf("验证失败状态码 = %d, 期望 422", resp.StatusCode)
		return
	}

	body := readBody(t, resp)
	contentType := resp.Header.Get("Content-Type")

	if !strings.Contains(contentType, "application/json") {
		t.Logf("【问题38】验证错误响应不是 JSON 格式 (Content-Type: %s)，body: %s", contentType, string(body))
	} else {
		var errResp map[string]interface{}
		if err := json.Unmarshal(body, &errResp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		// 验证错误应该包含字段级的错误详情
		if _, hasFields := errResp["fields"]; !hasFields {
			t.Logf("【问题38】验证错误响应缺少字段级错误详情。当前只返回 go-playground/validator 的原始错误消息，不够结构化。应返回 {\"fields\": [{\"field\": \"email\", \"message\": \"required\"}]} 格式")
		}
	}
}

// ============================================================================
// 问题39: Server 不暴露 http.Server 的 Handler field
// ============================================================================

func Test_Server_HttpServer_Access(t *testing.T) {
	t.Logf("【问题39】Server 内部持有 httpServer 但不暴露。用户无法在运行时访问 http.Server 来修改行为（如动态调整 Timeout、设置 ServeMux 等）")
}

// ============================================================================
// 问题40: 没有运行时路由信息查询
// ============================================================================

func Test_No_Runtime_Route_Info(t *testing.T) {
	t.Logf("【问题40】没有 Server.Routes() 或类似方法返回当前注册的所有路由信息。")
	t.Logf("  在调试、日志记录、健康检查等场景下需要知道当前路由表")
	t.Logf("  OpenAPI 只在使用 WithOpenAPI 时可用，且不包含中间件信息")
}

// ============================================================================
// 问题41: Group 不支持设置独立的 Validator 或 Envelope
// ============================================================================

func Test_Group_No_Independent_Validator(t *testing.T) {
	t.Logf("【问题41】Group 只支持设置独立的 Produces、Consumes 和 Middleware，但不支持设置独立的 Validator 或 Envelope。")
	t.Logf("  不同 API 组可能需要不同的验证规则（如内部 API 宽松，外部 API 严格）")
	t.Logf("  不同 API 组可能需要不同的响应信封格式")
}

// ============================================================================
// 问题42: 没有 Server.SetOption 运行时修改配置
// ============================================================================

func Test_No_Runtime_Config_Update(t *testing.T) {
	t.Logf("【问题42】所有配置只能在 New() 时通过 ServerOption 传入，没有运行时修改的机制。")
	t.Logf("  例如动态调整 MaxBodyBytes、切换 Logger 等")
}

// ============================================================================
// 综合测试: SSE 事件流
// ============================================================================

func Test_SSE_Events(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d, want 200", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("SSE Content-Type = %s, want text/event-stream", ct)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE: %v", err)
	}

	if !strings.Contains(string(data), "event: tick") {
		t.Errorf("SSE body missing 'event: tick': %s", string(data[:min(200, len(data))]))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// 问题43: 测试中无法方便地创建带路径参数的请求
// ============================================================================

func Test_Testing_With_Path_Params(t *testing.T) {
	t.Logf("【问题43】使用 httptest.NewRecorder() + httptest.NewRequest() 测试 ghttp 路由时，路径参数由 RadixRouter 内部解析，不需要手动设置。这部分设计合理。")
	t.Logf("  但 Server.ServeHTTP 会调用 finalizeRoutes()，每次都会检查 routed 标志，确保只初始化一次。这是正确的。")
}

// ============================================================================
// 问题44: 没有 Request 的大小限制配置与 413 状态码的统一处理
// ============================================================================

func Test_MaxBodyBytes_413_Response(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON), ghttp.WithMaxBodyBytes(10))
	ghttp.Route[struct {
		ghttp.Params `json:"-"`
		Body         struct {
			Data string `json:"data"`
		} `json:"body"`
	}, struct{}](s).
		POST("/body").
		To(func(ctx context.Context, req struct {
			ghttp.Params `json:"-"`
			Body         struct {
				Data string `json:"data"`
			} `json:"body"`
		}) (struct{}, error) {
			return struct{}{}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doReq(t, ts, http.MethodPost, "/body", map[string]string{
		"data": "this is way more than ten bytes of data",
	})
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Logf("【问题44】413 请求体过大时返回 Content-Type = %q，不是 application/json。框架级错误响应应统一格式", ct)
		}
	}
}

// ============================================================================
// 问题45: Cookie 读取只能取值，无法获取完整的 *http.Cookie 属性
// ============================================================================

func Test_Cookie_Limited_API(t *testing.T) {
	t.Logf("【问题45】Params.Cookie(key) 只返回 cookie 值 string。如果需要读取 cookie 的属性（如 Expires、HttpOnly、SameSite 等），只能通过 Params.Cookies() 获取完整列表再遍历。")
	t.Logf("  不如提供 Params.CookieDetail(key) *http.Cookie 方法直接获取")
}

// ============================================================================
// 问题46: 没有内置的限流中间件
// ============================================================================

func Test_No_RateLimiting(t *testing.T) {
	t.Logf("【问题46】没有内置的 RateLimit 中间件。生产 API 几乎都需要限流，框架应提供常见的限流策略（如令牌桶、滑动窗口）作为可选中间件")
}

// ============================================================================
// 问题47: 没有内置的认证中间件
// ============================================================================

func Test_No_Auth_Middleware(t *testing.T) {
	t.Logf("【问题47】没有内置的认证中间件（如 JWT、Basic Auth、API Key）。虽然可以自行实现，但框架提供常用认证方案可以减少重复代码")
}

// ============================================================================
// 问题48: RequestID 生成使用 crypto/rand 但未暴露获取方法
// ============================================================================

func Test_RequestID_No_Context_Access(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(ghttp.RequestID())
	ghttp.Route[struct{}, struct {
		ID string `json:"id"`
	}](s).
		GET("/reqid").
		To(func(ctx context.Context, req struct{}) (struct {
			ID string `json:"id"`
		}, error) {
			// 无法从 ctx 获取 Request-ID
			return struct {
				ID string `json:"id"`
			}{ID: ""}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/reqid", nil)
	req.Header.Set("X-Request-ID", "test-123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	// 验证响应头中有 Request-ID
	if resp.Header.Get("X-Request-ID") == "" {
		t.Logf("RequestID 响应头为空")
	}

	// 但 handler 内部拿不到
	var result map[string]interface{}
	if err := json.Unmarshal(readBody(t, resp), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result["id"] == "" {
		t.Logf("【问题48】RequestID 中间件将 ID 写入响应头，但 handler 无法通过标准 API 获取。应提供 GetRequestID(r *http.Request) string 或将 ID 注入 context")
	}
}

// ============================================================================
// 问题49: 编译时路由错误以 panic 表现，运行时才发现
// ============================================================================

func Test_Route_Setup_Error_As_Panic(t *testing.T) {
	t.Logf("【问题49】路由注册错误（如方法无效、produces 未设置）通过 recordSetupError() 记录，")
	t.Logf("  在 finalizeRoutes() 时 panic。这意味着错误不是在注册时立即暴露，而是在第一次请求时才 panic。")
	t.Logf("  对于库来说，注册时返回 error 或立即 panic 更安全")
}

// ============================================================================
// 问题50: 没有 HTTP/2 推送支持
// ============================================================================

func Test_No_HTTP2_Push(t *testing.T) {
	t.Logf("【问题50】没有 HTTP/2 Server Push 的支持。虽然这不是常见需求，但对于现代 Web 应用是一个有用的优化点")
}
