package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

func TestFileUploadActual(t *testing.T) {
	// 创建一个临时文件作为上传源
	tmpFile, err := os.CreateTemp("", "upload-*.txt")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := []byte("hello ghttp upload")
	if _, err := tmpFile.Write(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_ = tmpFile.Close()

	// 构建 multipart body
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("open temp file: %v", err)
	}
	if _, err := io.Copy(fileWriter, f); err != nil {
		t.Fatalf("copy file content: %v", err)
	}
	_ = f.Close()
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	// 注册一个上传路由
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[UploadImageInput, struct {
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}](s).
		POST("/upload").
		Consumes("multipart/form-data").
		Produces(ghttp.MIMEJSON).
		To(func(ctx context.Context, req UploadImageInput) (struct {
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		}, error) {
			return struct {
				Filename string `json:"filename"`
				Size     int64  `json:"size"`
			}{Filename: "test.txt", Size: int64(len(content))}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("【发现问题】文件上传实际请求失败，status=%d, body=%s", resp.StatusCode, string(body))
	}
}

// ============================================================================
// 自定义 envelope 测试
// ============================================================================

func TestCustomEnvelope(t *testing.T) {
	customEnv := func(w http.ResponseWriter, r *http.Request, statusCode int, resp interface{}, err error, contentType string, codec ghttp.Codec) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(statusCode)
		var body map[string]interface{}
		if err != nil {
			he := ghttp.AsError(err)
			if he != nil {
				body = map[string]interface{}{"errno": he.Code, "errmsg": he.Message}
			} else {
				body = map[string]interface{}{"errno": 500, "errmsg": err.Error()}
			}
		} else {
			body = map[string]interface{}{"errno": 0, "errmsg": "ok", "data": resp}
		}
		_ = codec.Marshal(w, body)
	}

	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON), ghttp.WithEnvelope(customEnv))
	ghttp.Route[struct{}, struct {
		OK bool `json:"ok"`
	}](s).
		GET("/ok").
		To(func(ctx context.Context, req struct{}) (struct {
			OK bool `json:"ok"`
		}, error) {
			return struct {
				OK bool `json:"ok"`
			}{OK: true}, nil
		})
	ghttp.Route[struct{}, struct{}](s).
		GET("/err").
		To(func(ctx context.Context, req struct{}) (struct{}, error) {
			return struct{}{}, ghttp.BadRequest("bad request")
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/ok", nil)
	defer resp.Body.Close()
	body := readBody(t, resp)
	if !strings.Contains(string(body), `"errno"`) || !strings.Contains(string(body), `"data"`) {
		t.Logf("【发现问题】自定义 envelope 成功响应格式不符合预期: %s", string(body))
	}

	resp = doRequest(t, ts, http.MethodGet, "/err", nil)
	defer resp.Body.Close()
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("err status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(string(body), `"errno"`) {
		t.Logf("【发现问题】自定义 envelope 错误响应未包含 errno: %s", string(body))
	}
}

// ============================================================================
// 内容协商测试
// ============================================================================

func TestContentNegotiation(t *testing.T) {
	// 注册 XML 输出 codec（如果尚未注册）
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	ghttp.Route[struct{}, struct {
		Message string `json:"message" xml:"message"`
	}](s).
		GET("/hello").
		Produces(ghttp.MIMEJSON).
		To(func(ctx context.Context, req struct{}) (struct {
			Message string `json:"message" xml:"message"`
		}, error) {
			return struct {
				Message string `json:"message" xml:"message"`
			}{Message: "hello"}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/hello", nil)
	req.Header.Set("Accept", "application/xml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("xml request: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	// 当前路由显式 Produces(MIMEJSON)，可能不根据 Accept 协商
	t.Logf("Accept=xml, Content-Type=%s", ct)
}

// ============================================================================
// 请求体大小限制测试
// ============================================================================

func TestMaxBodyBytes(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON), ghttp.WithMaxBodyBytes(10))
	ghttp.Route[struct {
		Body struct {
			Data string `json:"data"`
		} `json:"body"`
	}, struct{}](s).
		POST("/body").
		To(func(ctx context.Context, req struct {
			Body struct {
				Data string `json:"data"`
			} `json:"body"`
		}) (struct{}, error) {
			return struct{}{}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodPost, "/body", map[string]string{"data": "this is more than ten bytes"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Logf("【发现问题】请求体超过 MaxBodyBytes 时返回 %d，不是 413", resp.StatusCode)
	}
}

// ============================================================================
// 请求头传播测试
// ============================================================================

func TestRequestIDPropagation(t *testing.T) {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	s.Use(ghttp.RequestID())
	ghttp.Route[struct{}, struct {
		ID string `json:"id"`
	}](s).
		GET("/reqid").
		To(func(ctx context.Context, req struct{}) (struct {
			ID string `json:"id"`
		}, error) {
			// 请求方传入的 X-Request-ID 应该被保留
			id := ctx.Value("id")
			_ = id
			return struct {
				ID string `json:"id"`
			}{ID: ""}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/reqid", nil)
	req.Header.Set("X-Request-ID", "client-request-id")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("X-Request-ID") != "client-request-id" {
		t.Logf("【发现问题】客户端传入 X-Request-ID 未被保留，返回 %q", resp.Header.Get("X-Request-ID"))
	}
}

// ============================================================================
// Timeout 中间件测试
// ============================================================================

func TestTimeoutMiddleware(t *testing.T) {
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

	// 超时后，handler 应该返回非 200
	if resp.StatusCode == http.StatusOK {
		t.Logf("【发现问题】Timeout 中间件触发后仍返回 200，未处理 context 超时错误")
	}
}

// ============================================================================
// 重复路由注册测试
// ============================================================================

func TestDuplicateRoute(t *testing.T) {
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
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate route setup panic")
		}
	}()
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
}

// ============================================================================
// GET 路由的 HEAD 请求测试
// ============================================================================

func TestHeadFromGet(t *testing.T) {
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
		t.Fatalf("head request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusMethodNotAllowed {
		t.Logf("【发现问题】GET 路由不自动支持 HEAD 请求，返回 405")
	} else if resp.StatusCode == http.StatusOK && len(body) != 0 {
		t.Logf("【发现问题】HEAD 请求响应体非空（len=%d）", len(body))
	}
}

// ============================================================================
// 路由组中间件时序测试
// ============================================================================

func TestGroupMiddlewareOrder(t *testing.T) {
	var order []string
	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw1-after")
		})
	}
	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw2-after")
		})
	}

	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	api := s.Group("/api", mw1)
	api.Use(mw2)
	ghttp.Route[struct{}, struct {
		OK bool `json:"ok"`
	}](api).
		GET("/order").
		To(func(ctx context.Context, req struct{}) (struct {
			OK bool `json:"ok"`
		}, error) {
			order = append(order, "handler")
			return struct {
				OK bool `json:"ok"`
			}{OK: true}, nil
		})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/api/order", nil)
	defer resp.Body.Close()

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if !slices.Equal(order, expected) {
		t.Logf("【发现问题】中间件执行顺序不符合预期: got %v, want %v", order, expected)
	}
}

func TestGroupMiddlewareAfterRegistration(t *testing.T) {
	var called bool
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	api := s.Group("/api")
	ghttp.Route[struct{}, struct {
		OK bool `json:"ok"`
	}](api).
		GET("/admin").
		To(func(ctx context.Context, req struct{}) (struct {
			OK bool `json:"ok"`
		}, error) {
			return struct {
				OK bool `json:"ok"`
			}{OK: true}, nil
		})
	// 路由注册后再添加中间件
	api.Use(auth)

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/api/admin", nil)
	defer resp.Body.Close()

	if !called {
		t.Logf("【发现问题】Group.Use() 在路由注册后调用不生效")
	}
}

// ============================================================================
// 测试辅助函数
// ============================================================================

func doRequest(t *testing.T, s *httptest.Server, method, path string, body interface{}) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, s.URL+path, bodyReader)
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
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return data
}

// ============================================================================
// 基础 CRUD 测试
// ============================================================================

func TestListPosts(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/posts?page=1&pageSize=10", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body := readBody(t, resp)
	var result ListPostsOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, string(body))
	}

	if len(result.Posts) != 2 {
		t.Errorf("posts count = %d, want 2", len(result.Posts))
	}
	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if result.Page != 1 {
		t.Errorf("page = %d, want 1", result.Page)
	}
	if result.PageSize != 10 {
		t.Errorf("pageSize = %d, want 10", result.PageSize)
	}

	// 验证 RequestID 中间件是否注入
	if resp.Header.Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header is missing")
	}
}

func TestGetPost(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 正常获取
	resp := doRequest(t, ts, http.MethodGet, "/api/v1/posts/1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body := readBody(t, resp)
	var post Post
	if err := json.Unmarshal(body, &post); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, string(body))
	}
	if post.ID != "1" {
		t.Errorf("post ID = %s, want 1", post.ID)
	}

	// 不存在的帖子
	resp = doRequest(t, ts, http.MethodGet, "/api/v1/posts/999", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// 问题发现1：无 envelope 时错误响应是 text/plain
	// 这里验证错误响应格式是否一致
	resp = doRequest(t, ts, http.MethodGet, "/api/v1/posts/999", nil)
	body = readBody(t, resp)
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("【发现问题】无 envelope 时错误响应 Content-Type = %q，不是 application/json", contentType)
		t.Logf("  错误响应体: %s", string(body))
	}
}

func TestCreatePost(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/posts", map[string]interface{}{
		"title":   "New Post",
		"content": "This is content",
		"author":  "tester",
		"tags":    []string{"go", "http"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", resp.StatusCode, http.StatusOK, string(readBody(t, resp)))
	}

	body := readBody(t, resp)
	var result CreatePostOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, string(body))
	}
	if result.Post.Title != "New Post" {
		t.Errorf("title = %s, want New Post", result.Post.Title)
	}
}

func TestUpdatePost(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	newTitle := "Updated Title"
	resp := doRequest(t, ts, http.MethodPut, "/api/v1/posts/1", map[string]interface{}{
		"title": newTitle,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", resp.StatusCode, http.StatusOK, string(readBody(t, resp)))
	}

	body := readBody(t, resp)
	var result UpdatePostOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, string(body))
	}
	if result.Post.Title != newTitle {
		t.Errorf("title = %s, want %s", result.Post.Title, newTitle)
	}
}

func TestDeletePost(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodDelete, "/api/v1/posts/1", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// 问题发现2：DeleteOutput 是空结构体，但返回了 200 而非声明的 204
	// 声明了 Responds(204) 但实际返回 200
	body := readBody(t, resp)
	t.Logf("Delete response body: %q (len=%d)", string(body), len(body))

	// 验证已删除
	resp = doRequest(t, ts, http.MethodGet, "/api/v1/posts/1", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("after delete, status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	resp.Body.Close()
}

// ============================================================================
// 路由匹配测试
// ============================================================================

func TestRouteConflict_SearchVsId(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 路由注册顺序：先 /posts/{id} 再 /posts/search
	// 这在 RadixRouter 中可能会产生冲突
	resp := doRequest(t, ts, http.MethodGet, "/api/v1/posts/search?q=hello", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("search status = %d, want %d, body: %s", resp.StatusCode, http.StatusOK, string(readBody(t, resp)))
		return
	}

	body := readBody(t, resp)
	var result SearchOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, string(body))
	}
	if result.Keyword != "hello" {
		t.Errorf("keyword = %s, want hello", result.Keyword)
	}
}

func TestNotFound(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/nonexistent", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	resp.Body.Close()
}

func TestMethodNotAllowed(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 对 /api/v1/posts/{id} 发 PATCH 请求（未注册）
	resp := doRequest(t, ts, http.MethodPatch, "/api/v1/posts/1", nil)

	// 问题发现3：路径存在但方法不存在时，应返回 405 Method Not Allowed
	// 而非 404 Not Found
	if resp.StatusCode == http.StatusNotFound {
		t.Logf("【发现问题】路径存在但方法不匹配时返回 404 而非 405 Method Not Allowed")
	} else if resp.StatusCode == http.StatusMethodNotAllowed {
		// 检查 Allow header
		allow := resp.Header.Get("Allow")
		if allow == "" {
			t.Logf("【发现问题】405 响应缺少 Allow header")
		}
	}
	resp.Body.Close()
}

// ============================================================================
// CORS 测试
// ============================================================================

func TestCORS(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/v1/posts", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type,Authorization")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for wildcard credentials config", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want empty for wildcard credentials config", got)
	}
	if got := resp.Header.Get("Vary"); got == "" {
		t.Fatal("Vary header missing")
	}
}

// ============================================================================
// Cookie 测试
// ============================================================================

func TestLoginCookie(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/auth/login", map[string]interface{}{
		"username": "admin",
		"password": "password",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", resp.StatusCode, http.StatusOK, string(readBody(t, resp)))
	}

	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookies in response")
	}

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
	if !sessionCookie.HttpOnly {
		t.Error("cookie should be HttpOnly")
	}
}

// ============================================================================
// SSE 测试
// ============================================================================

func TestSSE(t *testing.T) {
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
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("Content-Type = %s, want text/event-stream", contentType)
	}

	// 读取 SSE 事件
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE body: %v", err)
	}

	bodyStr := string(data)
	if !strings.Contains(bodyStr, "event: tick") {
		t.Errorf("SSE body should contain 'event: tick', got: %s", bodyStr[:min(200, len(bodyStr))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// OpenAPI 测试
// ============================================================================

func TestOpenAPI(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/openapi.json", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body := readBody(t, resp)

	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal openapi: %v, body: %s", err, string(body))
	}

	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi version = %v, want 3.1.0", doc["openapi"])
	}

	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("paths is not a map")
	}

	// 验证路径存在
	expectedPaths := []string{
		"/api/v1/posts",
		"/api/v1/posts/{id}",
		"/api/v1/posts/search",
		"/api/v1/posts/{id}/comments",
		"/api/v1/events",
		"/api/v1/auth/login",
		"/health",
	}
	for _, p := range expectedPaths {
		if _, ok := paths[p]; !ok {
			t.Errorf("path %q not found in OpenAPI spec", p)
		}
	}

	// 问题发现6：OpenAPI 中 /api/v1/posts/search 可能被 {id} 覆盖
	// 因为注册顺序是先 {id} 后 search
	if _, ok := paths["/api/v1/posts/search"]; !ok {
		t.Logf("【发现问题】/api/v1/posts/search 被 /api/v1/posts/{id} 在 OpenAPI 中覆盖")
	}
}

// ============================================================================
// 验证器测试
// ============================================================================

func TestValidation(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 创建帖子但缺少必填字段
	resp := doRequest(t, ts, http.MethodPost, "/api/v1/posts", map[string]interface{}{
		"title": "Test",
		// 缺少 content 和 author
	})
	defer resp.Body.Close()

	// 验证器应该返回 422 Unprocessable Entity
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("validation error status = %d, want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}

	// 问题发现7：验证错误的响应格式是否结构化
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("【发现问题】验证错误响应 Content-Type = %q，不是 application/json", contentType)
	}
	body := readBody(t, resp)
	t.Logf("Validation error body: %s", string(body))
}

// ============================================================================
// 中间件测试
// ============================================================================

func TestMiddlewareOrder(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/posts", nil)
	defer resp.Body.Close()

	// 验证 server 级中间件是否生效
	if resp.Header.Get("X-Request-ID") == "" {
		t.Error("RequestID middleware not applied")
	}
}

func TestRecoverer(t *testing.T) {
	// 创建一个会 panic 的路由
	s := buildServer()

	// 直接添加一个会 panic 的路由
	ghttp.Route[ghttp.Params, struct {
		OK string `json:"ok"`
	}](s).GET("/panic").To(func(ctx context.Context, req ghttp.Params) (struct {
		OK string `json:"ok"`
	}, error) {
		panic("intentional panic for testing")
	})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/panic", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("panic status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	// 问题发现8：Recoverer 不打印堆栈信息
	// 验证错误响应格式
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Logf("【发现问题】Recoverer 错误响应 Content-Type = %q，不是 application/json", contentType)
	}
	body := readBody(t, resp)
	t.Logf("Recoverer error body: %s", string(body))
}

// ============================================================================
// 文件上传测试
// ============================================================================

func TestFileUploadNotSupported(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 问题发现9：文件上传需要 multipart/form-data，但 RouteBuilder 的 To() handler
	// 需要定义 Body struct，而 multipart 解析依赖 form tag
	// 当前 ghttp 的 multipart 支持需要 Body 字段，但文件上传场景的输入类型设计不直观
	t.Logf("【发现问题】文件上传 API 设计不直观：需要 Body struct + form tag，但 type constraint 要求泛型")
}

// ============================================================================
// 路径参数类型转换测试
// ============================================================================

func TestPathParamsAllStrings(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 问题发现10：所有 path/query 参数都是 string 类型
	// 没有类型化的参数读取方法（QueryInt, QueryBool 等）
	t.Logf("【发现问题】path/query 参数全部是 string，缺少类型化便捷方法")

	// 验证 Params API 只返回 string
	resp := doRequest(t, ts, http.MethodGet, "/api/v1/posts?page=2&pageSize=5", nil)
	defer resp.Body.Close()

	body := readBody(t, resp)
	var result ListPostsOutput
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Page != 2 {
		t.Errorf("page = %d, want 2", result.Page)
	}
	if result.PageSize != 5 {
		t.Errorf("pageSize = %d, want 5", result.PageSize)
	}
}

// ============================================================================
// 多路由方法测试
// ============================================================================

func TestMultipleMethodsOnSamePath(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// /api/v1/posts 同时注册了 GET 和 POST
	resp := doRequest(t, ts, http.MethodGet, "/api/v1/posts", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	resp = doRequest(t, ts, http.MethodPost, "/api/v1/posts", map[string]interface{}{
		"title":   "T",
		"content": "C",
		"author":  "A",
	})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()
}

// ============================================================================
// 响应状态码测试
// ============================================================================

func TestStatusCodeFromResponse(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 创建帖子 — 声明了 Responds(201) 但实际返回什么？
	resp := doRequest(t, ts, http.MethodPost, "/api/v1/posts", map[string]interface{}{
		"title":   "Status Test",
		"content": "Content",
		"author":  "Tester",
	})
	defer resp.Body.Close()

	// 问题发现11：声明了 Responds(201) 但 handler 返回的结构体没有 Status 字段
	// 也未实现 StatusCoder 接口，所以框架默认返回 200
	if resp.StatusCode == http.StatusOK {
		t.Logf("【发现问题】声明了 Responds(201) 但实际返回 200，因为响应结构体没有 Status 字段也未实现 StatusCoder")
	} else if resp.StatusCode == http.StatusCreated {
		// 正确行为
	}
}

// ============================================================================
// 嵌套 Group 测试
// ============================================================================

func TestNestedGroup(t *testing.T) {
	s := buildServer()

	// 问题发现12：Group 不支持设置独立的中间件后链式继续
	// 需要在 Group() 调用时一次性传入中间件
	v1 := s.Group("/api/v1")
	admin := v1.Group("/admin")

	ghttp.Route[ghttp.Params, struct {
		Admin string `json:"admin"`
	}](admin).GET("/dashboard").To(func(ctx context.Context, req ghttp.Params) (struct {
		Admin string `json:"admin"`
	}, error) {
		return struct {
			Admin string `json:"admin"`
		}{Admin: "yes"}, nil
	})

	ts := httptest.NewServer(s)
	defer ts.Close()

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/admin/dashboard", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("nested group status = %d, want %d, body: %s",
			resp.StatusCode, http.StatusOK, string(readBody(t, resp)))
	}
}

// ============================================================================
// Trailing Slash 测试
// ============================================================================

func TestTrailingSlash(t *testing.T) {
	s := buildServer()
	ts := httptest.NewServer(s)
	defer ts.Close()

	// 问题发现13：/api/v1/posts/ 和 /api/v1/posts 行为是否一致
	resp := doRequest(t, ts, http.MethodGet, "/api/v1/posts/", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Logf("【发现问题】trailing slash /api/v1/posts/ 返回 %d，注册路径为 /api/v1/posts",
			resp.StatusCode)
	}
}
