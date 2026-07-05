package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

// 测试1: 基本路由注册和参数提取
type CreateUserInput struct {
	ghttp.Params `json:"-"`
	Body         struct {
		Name  string `json:"name" validate:"required"`
		Email string `json:"email" validate:"required,email"`
		Age   int    `json:"age" validate:"min=0,max=150"`
	} `json:"body"`
}

type CreateUserOutput struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

// 测试2: 仅使用Params，无请求体
type GetUserInput struct {
	ghttp.Params `json:"-"`
}

type GetUserOutput struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// 测试3: 复杂查询参数
type ListUsersInput struct {
	ghttp.Params `json:"-"`
}

type ListUsersOutput struct {
	Users []GetUserOutput `json:"users"`
	Total int             `json:"total"`
}

// 测试4: 文件上传
type UploadFileInput struct {
	ghttp.Params `json:"-"`
}

type UploadFileOutput struct {
	FileName string `json:"file_name"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

// 测试5: 错误处理
type ErrorTestInput struct {
	ghttp.Params `json:"-"`
	Body         struct {
		ShouldError bool `json:"should_error"`
	} `json:"body"`
}

type ErrorTestOutput struct {
	Message string `json:"message"`
}

func main() {
	// 创建服务器
	s := ghttp.New(
		ghttp.WithAddress(":8080"),
		ghttp.WithProduces(ghttp.MIMEJSON),
		ghttp.WithConsumes(ghttp.MIMEJSON),
		ghttp.WithReadTimeout(30*time.Second),
		ghttp.WithWriteTimeout(30*time.Second),
		ghttp.WithIdleTimeout(60*time.Second),
		ghttp.WithMaxBodyBytes(4<<20), // 4MB
	)

	// 注册中间件
	s.Use(ghttp.RequestID())
	s.Use(ghttp.CORS(ghttp.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Authorization", "Content-Type"},
	}))
	s.Use(ghttp.RequestLogger())
	s.Use(ghttp.Recoverer())
	s.Use(ghttp.Timeout(10 * time.Second))

	// 测试1: 创建用户 - 基本POST请求
	// 正确的API使用方式：
	// - Responds().With().Desc().End() 链式调用后返回 RouteBuilder
	// - 可以链式调用多个 Responds
	// - 最后调用 To() 注册处理函数
	ghttp.Route[CreateUserInput, CreateUserOutput](s).
		POST("/users").
		Doc(ghttp.Summary("创建新用户")).
		To(func(ctx context.Context, req CreateUserInput) (CreateUserOutput, error) {
			return CreateUserOutput{
				ID:    "123",
				Name:  req.Body.Name,
				Email: req.Body.Email,
				Age:   req.Body.Age,
			}, nil
		})

	// 测试2: 获取用户 - GET请求带路径参数
	ghttp.Route[GetUserInput, GetUserOutput](s).
		GET("/users/{id}").
		Doc(ghttp.Summary("获取用户信息")).
		To(func(ctx context.Context, req GetUserInput) (GetUserOutput, error) {
			userID := req.Path("id")
			if userID == "" {
				return GetUserOutput{}, ghttp.Err(http.StatusBadRequest, "missing user id")
			}

			return GetUserOutput{
				ID:    userID,
				Name:  "Test User",
				Email: "test@example.com",
			}, nil
		})

	// 测试3: 列表用户 - GET请求带查询参数
	ghttp.Route[ListUsersInput, ListUsersOutput](s).
		GET("/users").
		Doc(ghttp.Summary("获取用户列表")).
		To(func(ctx context.Context, req ListUsersInput) (ListUsersOutput, error) {
			// 问题1: 查询参数需要手动提取，没有结构体自动绑定
			// 主流框架通常支持通过 struct tags 自动绑定查询参数到结构体字段
			page := req.DefaultQuery("page", "1")
			pageSize := req.DefaultQuery("page_size", "10")
			_ = page
			_ = pageSize

			return ListUsersOutput{
				Users: []GetUserOutput{
					{ID: "1", Name: "User 1", Email: "user1@example.com"},
					{ID: "2", Name: "User 2", Email: "user2@example.com"},
				},
				Total: 2,
			}, nil
		})

	// 测试4: 更新用户 - PUT请求
	ghttp.Route[CreateUserInput, CreateUserOutput](s).
		PUT("/users/{id}").
		Doc(ghttp.Summary("更新用户信息")).
		To(func(ctx context.Context, req CreateUserInput) (CreateUserOutput, error) {
			userID := req.Path("id")
			_ = userID

			return CreateUserOutput{
				ID:    userID,
				Name:  req.Body.Name,
				Email: req.Body.Email,
				Age:   req.Body.Age,
			}, nil
		})

	// 测试5: 删除用户 - DELETE请求
	ghttp.Route[GetUserInput, struct{}](s).
		DELETE("/users/{id}").
		Doc(ghttp.Summary("删除用户")).
		To(func(ctx context.Context, req GetUserInput) (struct{}, error) {
			userID := req.Path("id")
			_ = userID
			return struct{}{}, nil
		})

	// 测试6: 文件上传
	// 问题2: 文件上传的API设计不够清晰
	// 需要查看如何正确处理 multipart/form-data
	// 修正常量名称: MIMEMultipartPOSTForm
	ghttp.Route[UploadFileInput, UploadFileOutput](s).
		POST("/upload").
		Doc(ghttp.Summary("上传文件")).
		Consumes(ghttp.MIMEMultipartPOSTForm).
		To(func(ctx context.Context, req UploadFileInput) (UploadFileOutput, error) {
			// 如何从输入中获取上传的文件？
			// 可能需要通过 Params 访问原始的 *http.Request
			// 这是一个API设计问题：文件上传场景下的输入访问不够直观
			return UploadFileOutput{
				FileName: "test.txt",
				Size:     1024,
				URL:      "/files/test.txt",
			}, nil
		})

	// 测试7: WebSocket
	// 注意：WebSocket、SSE、Static 等不需要调用 Responds()，直接调用 ToWebSocket()
	ghttp.Route[struct{}, struct{}](s).
		GET("/ws").
		Doc(ghttp.Summary("WebSocket连接")).
		ToWebSocket(func(ctx context.Context, params ghttp.Params, conn *ghttp.WebSocketConn) error {
			for {
				var msg map[string]interface{}
				if err := conn.ReadJSON(&msg); err != nil {
					return err
				}
				if err := conn.WriteJSON(msg); err != nil {
					return err
				}
			}
		})

	// 测试8: SSE
	ghttp.Route[struct{}, struct{}](s).
		GET("/events").
		Doc(ghttp.Summary("SSE事件流")).
		ToSSE(func(ctx context.Context, params ghttp.Params, w *ghttp.SSEWriter) error {
			for i := 0; i < 10; i++ {
				w.WriteEvent("message", fmt.Sprintf("event %d", i))
				time.Sleep(time.Second)
			}
			return nil
		})

	// 测试9: 静态文件服务
	ghttp.Route[struct{}, struct{}](s).
		GET("/static/{path...}").
		Doc(ghttp.Summary("静态文件服务")).
		ToStatic("./public")

	// 测试10: 错误处理
	ghttp.Route[ErrorTestInput, ErrorTestOutput](s).
		POST("/error-test").
		Doc(ghttp.Summary("错误处理测试")).
		To(func(ctx context.Context, req ErrorTestInput) (ErrorTestOutput, error) {
			if req.Body.ShouldError {
				// 问题3: 如何返回自定义业务错误码？
				// 当前只能返回 HTTP 状态码
				// 如果需要返回自定义错误码（如 40001），需要使用 envelope 或自定义错误类型
				return ErrorTestOutput{}, ghttp.Err(http.StatusBadRequest, "business error")
			}

			return ErrorTestOutput{
				Message: "success",
			}, nil
		})

	// 测试11: 获取原始请求和响应
	ghttp.Route[struct{}, struct{}](s).
		GET("/raw").
		Doc(ghttp.Summary("原始HTTP访问测试")).
		ToHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("raw handler"))
		}))

	// 问题4: 分组路由的使用方式不够清晰
	// 从代码看，Group 返回 *Group，但如何使用 Route 构建器与 Group？
	apiGroup := s.Group("/api/v1")

	// 问题5: 如何在 Group 上使用 Route 构建器？
	// 尝试1: ghttp.Route[Req, Resp](apiGroup) - 可能可以工作
	// 尝试2: 使用 Group 的方法注册路由

	// 测试在分组中注册路由
	// 需要先查看 Group 类型有哪些方法

	_ = apiGroup

	// 测试12: 启动服务器
	fmt.Println("Server starting on :8080")
	fmt.Println("Try: curl http://localhost:8080/users/123")
	fmt.Println("Try: curl -X POST http://localhost:8080/users -H 'Content-Type: application/json' -d '{\"name\":\"test\",\"email\":\"test@example.com\",\"age\":25}'")

	// 优雅关闭
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		fmt.Println("\nShutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.Shutdown(ctx); err != nil {
			fmt.Printf("Server shutdown error: %v\n", err)
		}
	}()

	// 启动服务器
	if err := s.Run(); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}
