package v4_test

import (
	"context"
	"testing"

	"github.com/sofiworker/gk/ghttp"
	v4 "github.com/sofiworker/gk/ghttp/v4"
)

// 测试基本类型
// Test basic types

type UserID struct {
	ID int64 `path:"id"`
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// 示例 1: 简单的 POST 处理器（只有 body）
// Example 1: Simple POST handler (body only)
func TestSimplePost(t *testing.T) {
	handler := func(ctx context.Context, req v4.RequestSimple[CreateUserRequest]) (User, error) {
		body, err := req.Body.Get()
		if err != nil {
			return User{}, err
		}

		return User{
			ID:       1,
			Username: body.Username,
			Email:    body.Email,
		}, nil
	}

	server := ghttp.New()
	v4.Post("/users", handler).MustRegister(server)

	t.Log("Simple POST handler registered successfully")
}

// 示例 2: RESTful GET 处理器（带路径参数）
// Example 2: RESTful GET handler (with path parameters)
func TestGetWithPath(t *testing.T) {
	handler := func(ctx context.Context, req v4.RequestOfMulti[UserID, v4.EmptyBody]) (User, error) {
		path, err := req.Path.Get()
		if err != nil {
			return User{}, err
		}

		// 模拟从数据库查询
		// Simulate database query
		return User{
			ID:       path.ID,
			Username: "test_user",
			Email:    "test@example.com",
		}, nil
	}

	server := ghttp.New()
	v4.Get("/users/{id}", handler).MustRegister(server)

	t.Log("GET handler with path parameters registered successfully")
}

// 示例 3: PATCH 处理器（路径 + body）
// Example 3: PATCH handler (path + body)
func TestPatchWithPathAndBody(t *testing.T) {
	handler := func(ctx context.Context, req v4.RequestOfMulti[UserID, CreateUserRequest]) (User, error) {
		// 延迟读取：先检查路径参数
		// Lazy access: check path parameter first
		path, err := req.Path.Get()
		if err != nil {
			return User{}, err
		}

		// 如果用户不存在，提前返回（body 未读取）
		// If user doesn't exist, return early (body not read)
		if path.ID > 1000 {
			return User{}, v4.NotFound("user not found")
		}

		// 只在需要时才读取 body
		// Read body only when needed
		body, err := req.Body.Get()
		if err != nil {
			return User{}, err
		}

		return User{
			ID:       path.ID,
			Username: body.Username,
			Email:    body.Email,
		}, nil
	}

	server := ghttp.New()
	v4.Patch("/users/{id}", handler).MustRegister(server)

	t.Log("PATCH handler with path and body registered successfully")
}

// 示例 4: 查询参数
// Example 4: Query parameters
type ListUsersQuery struct {
	Page     int `query:"page" default:"1"`
	PageSize int `query:"page_size" default:"20"`
}

func TestGetWithQuery(t *testing.T) {
	handler := func(ctx context.Context, req v4.RequestWithQuery[ListUsersQuery, v4.EmptyBody]) ([]User, error) {
		query, err := req.Query.Get()
		if err != nil {
			return nil, err
		}

		// 模拟分页查询
		// Simulate paginated query
		users := make([]User, 0, query.PageSize)
		for i := 0; i < query.PageSize; i++ {
			users = append(users, User{
				ID:       int64((query.Page-1)*query.PageSize + i + 1),
				Username: "user",
				Email:    "user@example.com",
			})
		}

		return users, nil
	}

	server := ghttp.New()
	v4.Get("/users", handler).MustRegister(server)

	t.Log("GET handler with query parameters registered successfully")
}
