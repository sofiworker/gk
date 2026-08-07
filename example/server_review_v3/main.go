// Package main 从使用者角度真实使用 ghttp 的 server 端全部 API，
// 旨在发现设计缺陷、易用性问题和场景覆盖不足。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

// ============================================================================
// 场景1: 基础 CRUD API — 最常见的 REST 服务
// ============================================================================

type CreateUserReq struct {
	ghttp.Params `json:"-"`

	Body struct {
		Name  string `json:"name" validate:"required" doc:"用户名"`
		Email string `json:"email" validate:"required,email" doc:"邮箱"`
		Age   int    `json:"age" validate:"min=0,max=150" doc:"年龄"`
	} `json:"body"`
}

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Age       int       `json:"age"`
	CreatedAt time.Time `json:"createdAt"`
}

type UserResp struct {
	User User `json:"user"`
}

type GetUserReq struct {
	ghttp.Params `json:"-"`
}

type ListUsersReq struct {
	ghttp.Params `json:"-"`
}

type ListUsersResp struct {
	Users []User `json:"users"`
	Total int    `json:"total"`
	Page  int    `json:"page"`
}

// ============================================================================
// 场景2: 仅使用 Params 的只读接口 — 无请求体
// ============================================================================

type SearchReq struct {
	ghttp.Params `json:"-"`
}

type SearchResp struct {
	Results []string `json:"results"`
	Query   string   `json:"query"`
}

// ============================================================================
// 场景3: 无输入无输出的健康检查
// ============================================================================

// 场景4: 响应带自定义状态码
type CreatedResp struct {
	User User `json:"user"`
}

// ============================================================================
// 场景5: 使用 Group 组织路由
// ============================================================================

// ============================================================================
// 场景6: 中间件组合
// ============================================================================

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ============================================================================
// 场景7: 使用 envelope 统一响应格式
// ============================================================================

// ============================================================================
// 场景8: 尝试在 handler 中获取原始 *http.Request
// ============================================================================

// ============================================================================
// 场景9: 尝试重定向
// ============================================================================

// ============================================================================
// 场景10: 尝试文件上传
// ============================================================================

type UploadReq struct {
	ghttp.Params `json:"-"`

	Body struct {
		Description string `json:"description" form:"description" doc:"描述"`
	} `json:"body"`
}

// ============================================================================
// 构建 server
// ============================================================================

func buildServer() *ghttp.Server {
	s := ghttp.New(
		ghttp.WithAddress(":0"),
		ghttp.WithProduces(ghttp.MIMEJSON),
		ghttp.WithConsumes(ghttp.MIMEJSON),
		ghttp.WithOpenAPI("Review API", "1.0.0"),
	)

	// 全局中间件
	s.Use(ghttp.RequestID())
	s.Use(ghttp.Recoverer())

	// --- 场景1: CRUD ---
	api := s.Group("/api/v1")
	api.Use(ghttp.RequestLogger())

	// 创建用户使用自写响应终结器明确拥有 HTTP 201。
	ghttp.Route[CreateUserReq, CreatedResp](api).
		POST("/users").
		Doc(ghttp.Summary("创建用户")).
		ToHTTPFunc(func(w http.ResponseWriter, _ *http.Request, req CreateUserReq) error {
			user := User{
				ID:        "user-1",
				Name:      req.Body.Name,
				Email:     req.Body.Email,
				Age:       req.Body.Age,
				CreatedAt: time.Now(),
			}
			w.Header().Set("Content-Type", ghttp.MIMEJSON)
			w.WriteHeader(http.StatusCreated)
			return json.NewEncoder(w).Encode(CreatedResp{User: user})
		})

	ghttp.Route[GetUserReq, UserResp](api).
		GET("/users/{id}").
		Doc(ghttp.Summary("获取用户")).
		To(func(ctx context.Context, req GetUserReq) (UserResp, error) {
			id := req.Path("id")
			// 问题2: Path() 只返回 string，没有 PathInt() 等类型化方法
			_ = id
			return UserResp{User: User{ID: id, Name: "test"}}, nil
		})

	ghttp.Route[ListUsersReq, ListUsersResp](api).
		GET("/users").
		Doc(ghttp.Summary("获取用户列表")).
		To(func(ctx context.Context, req ListUsersReq) (ListUsersResp, error) {
			// 问题3: 没有类型化的 Query 读取方法
			// 只能 req.Query("page") 然后手动 strconv.Atoi
			page := 1
			if p := req.Query("page"); p != "" {
				var n int
				if _, err := fmt.Sscanf(p, "%d", &n); err == nil && n > 0 {
					page = n
				}
			}
			return ListUsersResp{
				Users: []User{{ID: "1", Name: "test"}},
				Total: 1,
				Page:  page,
			}, nil
		})

	// --- 场景2: 搜索 — 仅用 query ---
	ghttp.Route[SearchReq, SearchResp](api).
		GET("/users/search").
		Doc(ghttp.Summary("搜索用户")).
		To(func(ctx context.Context, req SearchReq) (SearchResp, error) {
			q := req.Query("q")
			if q == "" {
				// 问题4: 没有内置的参数验证方式（只能手动检查）
				return SearchResp{}, ghttp.BadRequest("query parameter 'q' is required")
			}
			return SearchResp{Results: []string{q}, Query: q}, nil
		})

	// --- 场景3: 健康检查 — 无输入无输出 ---
	// 问题5: 即使不需要请求参数，Route 也必须指定 Req 和 Resp 泛型参数
	// 这里用 struct{} 但 handler 签名也要写 struct{}
	ghttp.Route[struct{}, struct {
		Status string `json:"status"`
	}](s).
		GET("/health").
		Doc(ghttp.Summary("健康检查")).
		To(func(ctx context.Context, req struct{}) (struct {
			Status string `json:"status"`
		}, error) {
			return struct {
				Status string `json:"status"`
			}{Status: "ok"}, nil
		})

	// --- 场景4: 带认证的路由组 ---
	// 问题6: Group 中间件只能在 Group() 构造时传入或之后 Use() 追加
	// 但如果先注册路由再 Use()，中间件不会生效（因为路由注册时立即构建 handler chain）
	authGroup := api.Group("/admin", authMiddleware)

	ghttp.Route[ghttp.Params, struct {
		Role string `json:"role"`
	}](authGroup).
		GET("/dashboard").
		Doc(ghttp.Summary("管理员面板")).
		To(func(ctx context.Context, req ghttp.Params) (struct {
			Role string `json:"role"`
		}, error) {
			return struct {
				Role string `json:"role"`
			}{Role: "admin"}, nil
		})

	// --- 场景5: 尝试获取原始 Request ---
	// 问题7: HandlerFunc 签名是 func(ctx context.Context, input Req) (Resp, error)
	// 没有直接方式获取 *http.Request，只能通过 ctx 或 Params 间接获取部分信息
	// 如果需要读取原始请求体（如 webhook 签名验证），只能用 ToRaw 或 ToHTTPFunc
	ghttp.Route[struct{}, struct {
		Host string `json:"host"`
	}](api).
		GET("/echo-host").
		Doc(ghttp.Summary("回显主机")).
		To(func(ctx context.Context, req struct{}) (struct {
			Host string `json:"host"`
		}, error) {
			// 问题7的延续: 无法从 ctx 或 handler 参数获取 *http.Request
			// 只能通过自定义中间件注入到 ctx
			return struct {
				Host string `json:"host"`
			}{Host: "unavailable"}, nil
		})

	// --- 场景6: 重定向 ---
	ghttp.Route[struct{}, struct{}](api).
		GET("/old-path").
		Doc(ghttp.Summary("旧路径重定向")).
		ToRedirect(http.StatusMovedPermanently, "/api/v1/users")

	// --- 场景7: SSE ---
	ghttp.Route[ghttp.Params, struct{}](api).
		GET("/events").
		Doc(ghttp.Summary("SSE 事件推送")).
		ToSSE(func(ctx context.Context, params ghttp.Params, stream *ghttp.SSEWriter) error {
			count := 0
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					count++
					if err := stream.WriteJSON("tick", map[string]any{
						"count": count,
						"time":  time.Now().Format(time.RFC3339),
					}); err != nil {
						return err
					}
					if count >= 3 {
						return nil
					}
				}
			}
		})

	// --- 场景8: 尝试自定义错误响应 ---
	// 问题8: 没有统一的错误处理钩子（除了 envelope）
	// 在没有 envelope 时，writeError 使用 http.Error() 返回 text/plain

	// --- 场景9: 尝试在同一路由上注册多种方法 ---
	// 问题9: RouteBuilder 不支持单次注册多种方法（如 GET+POST 同一 handler）
	// 必须分别调用 Route 两次

	// --- 场景10: 尝试 Query 参数自动绑定到结构体 ---
	// 问题10: 当前输入结构体只支持 Body（请求体）+ Params（path/query/header/cookie）
	// 不支持将 query 参数自动绑定到结构体字段（类似 Gin 的 ShouldBindQuery）

	// --- 场景11: 尝试处理 CORS 预检 ---
	// 问题11: CORS 中间件对所有请求都处理，包括非预检请求
	// 不支持基于路由的 CORS 配置
	s.Use(ghttp.CORS(ghttp.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           3600,
	}))

	// --- 场景12: 静态文件 ---
	// 问题12: ToStatic 需要 WithVFSPath 配置或传入 root 参数
	// 没有 "embed.FS" 支持的便捷方式
	ghttp.Route[struct{}, struct{}](s).
		GET("/static").
		ToStatic("./public")

	// --- 场景13: 尝试路由匹配 /users/{id} 和 /users/search 的冲突 ---
	// 这两个路由在 RadixRouter 中可能产生匹配冲突
	// 取决于注册顺序

	return s
}

// ============================================================================
// 额外测试: envelope 模式
// ============================================================================

func buildEnvelopeServer() *ghttp.Server {
	s := ghttp.New(
		ghttp.WithProduces(ghttp.MIMEJSON),
		ghttp.WithEnvelope(ghttp.DefaultEnvelope),
	)

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

	return s
}

func main() {
	s := buildServer()
	fmt.Println("Review server starting on :0")
	if err := s.Run(); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}
