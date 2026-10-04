package v3_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	v3 "github.com/sofiworker/gk/ghttp/v3"
)

// Benchmark 场景 1: 简单 GET 请求（无参数）
func BenchmarkV3_SimpleGET(b *testing.B) {
	server := v3.NewServer()
	handler := func(ctx context.Context, req v3.RequestOf[v3.NoData]) (string, error) {
		return "ok", nil
	}
	_ = server.Register(v3.Get("/ping", handler))

	req := httptest.NewRequest("GET", "/ping", nil)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp := httptest.NewRecorder()
		server.ServeHTTP(resp, req)
	}
}

func BenchmarkGin_SimpleGET(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/ping", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/ping", nil)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
	}
}

// Benchmark 场景 2: 带 path 参数的 GET 请求
func BenchmarkV3_GETWithPath(b *testing.B) {
	server := v3.NewServer()
	handler := func(ctx context.Context, req v3.RequestOf[v3.NoData]) (string, error) {
		id, _ := req.PathValue("id").Int64()
		_ = id
		return "ok", nil
	}
	_ = server.Register(v3.Get("/users/{id}", handler))

	req := httptest.NewRequest("GET", "/users/123", nil)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp := httptest.NewRecorder()
		server.ServeHTTP(resp, req)
	}
}

func BenchmarkGin_GETWithPath(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/users/:id", func(c *gin.Context) {
		id := c.Param("id")
		_ = id
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/users/123", nil)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
	}
}

// Benchmark 场景 3: 带 query 参数的 GET 请求
func BenchmarkV3_GETWithQuery(b *testing.B) {
	server := v3.NewServer()
	handler := func(ctx context.Context, req v3.RequestOf[v3.NoData]) (string, error) {
		page := req.QueryValue("page").IntOr(1)
		limit := req.QueryValue("limit").IntOr(20)
		_, _ = page, limit
		return "ok", nil
	}
	_ = server.Register(v3.Get("/users", handler))

	req := httptest.NewRequest("GET", "/users?page=2&limit=50", nil)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp := httptest.NewRecorder()
		server.ServeHTTP(resp, req)
	}
}

func BenchmarkGin_GETWithQuery(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/users", func(c *gin.Context) {
		page := c.DefaultQuery("page", "1")
		limit := c.DefaultQuery("limit", "20")
		_, _ = page, limit
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/users?page=2&limit=50", nil)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
	}
}

// Benchmark 场景 4: POST JSON 请求
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func BenchmarkV3_POSTWithJSON(b *testing.B) {
	server := v3.NewServer()
	handler := func(ctx context.Context, req v3.RequestOf[CreateUserRequest]) (string, error) {
		data, err := req.Data(ctx)
		if err != nil {
			return "", err
		}
		_ = data.Name
		_ = data.Email
		_ = data.Age
		return "ok", nil
	}
	_ = server.Register(v3.Post("/users", handler))

	body := `{"name":"Alice","email":"alice@example.com","age":25}`
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		server.ServeHTTP(resp, req)
	}
}

func BenchmarkGin_POSTWithJSON(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/users", func(c *gin.Context) {
		var req CreateUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.String(400, "bad request")
			return
		}
		_ = req.Name
		_ = req.Email
		_ = req.Age
		c.String(200, "ok")
	})

	body := `{"name":"Alice","email":"alice@example.com","age":25}`
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
	}
}

// Benchmark 场景 5: 提前返回场景（权限检查）
// 这个场景测试 v3 的 lazy body 问题
func BenchmarkV3_EarlyReturn(b *testing.B) {
	server := v3.NewServer()
	handler := func(ctx context.Context, req v3.RequestOf[CreateUserRequest]) (string, error) {
		// 模拟权限检查（提前返回）
		authToken := req.HeaderValue("Authorization").String()
		if authToken != "valid-token" {
			return "", v3.HTTPError{Status: 403}
		}
		// 如果通过权限检查，才访问 Body
		data, err := req.Data(ctx)
		if err != nil {
			return "", err
		}
		_ = data.Name
		return "ok", nil
	}
	_ = server.Register(v3.Post("/users", handler))

	// 大 JSON body
	body := `{"name":"Alice","email":"alice@example.com","age":25,"bio":"Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."}`
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "invalid-token") // 触发提前返回
		resp := httptest.NewRecorder()
		server.ServeHTTP(resp, req)
	}
}

func BenchmarkGin_EarlyReturn(b *testing.B) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.POST("/users", func(c *gin.Context) {
		// 模拟权限检查（提前返回）
		authToken := c.GetHeader("Authorization")
		if authToken != "valid-token" {
			c.String(403, "forbidden")
			return
		}
		// 如果通过权限检查，才解码 Body
		var req CreateUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.String(400, "bad request")
			return
		}
		_ = req.Name
		c.String(200, "ok")
	})

	// 大 JSON body
	body := `{"name":"Alice","email":"alice@example.com","age":25,"bio":"Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."}`
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "invalid-token") // 触发提前返回
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
	}
}
