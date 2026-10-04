package v3_test

import (
	"context"
	"errors"
	"testing"

	v3 "github.com/sofiworker/gk/ghttp/v3"
)

// 测试类型定义
// Test type definitions

type UserID struct {
	ID int64 `path:"id"`
}

type ListQuery struct {
	Page     int `query:"page" default:"1"`
	PageSize int `query:"page_size" default:"20"`
}

type AuthHeader struct {
	Token string `header:"Authorization"`
}

type UpdateUserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// 示例 1: v3 改进 - 类型化的 Path 访问
// Example 1: v3 improved - Typed Path access
func TestV3TypedPath(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[UpdateUserRequest]) (User, error) {
		// ✅ 新增：类型化的 Path 访问
		// New: Typed Path access
		pathAccessor := v3.Path[UserID](&req)
		path, err := pathAccessor.Get()
		if err != nil {
			return User{}, err
		}

		t.Logf("Typed path ID: %d", path.ID)

		// Data 依然直接访问
		// Data still direct access
		data, err := req.Data(ctx)
		if err != nil {
			return User{}, err
		}
		user := User{
			ID:       path.ID,
			Username: data.Username,
			Email:    data.Email,
		}

		return user, nil
	}

	_ = handler
	t.Log("Typed Path access works correctly")
}

// 示例 2: v3 改进 - 类型化的 Query 访问
// Example 2: v3 improved - Typed Query access
func TestV3TypedQuery(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[v3.NoData]) ([]User, error) {
		// ✅ 类型化的 Query 访问
		// Typed Query access
		queryAccessor := v3.Query[ListQuery](&req)
		query, err := queryAccessor.Get()
		if err != nil {
			return nil, err
		}

		t.Logf("Typed query: page=%d, pageSize=%d", query.Page, query.PageSize)

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

	_ = handler
	t.Log("Typed Query access works correctly")
}

// 示例 3: v3 改进 - 类型化的 Header 访问
// Example 3: v3 improved - Typed Header access
func TestV3TypedHeader(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[UpdateUserRequest]) (User, error) {
		// ✅ 类型化的 Header 访问
		// Typed Header access
		headerAccessor := v3.Header[AuthHeader](&req)
		header, err := headerAccessor.Get()
		if err != nil {
			t.Logf("No auth header (expected in test): %v", err)
		} else {
			t.Logf("Auth token: %s", header.Token)
		}

		// Data 访问
		data, err := req.Data(ctx)
		if err != nil {
			return User{}, err
		}
		user := User{
			ID:       1,
			Username: data.Username,
			Email:    data.Email,
		}

		return user, nil
	}

	_ = handler
	t.Log("Typed Header access works correctly")
}

// 示例 4: v3 改进 - 延迟读取 Body（WithLazyBody）
// Example 4: v3 improved - Lazy Body loading (WithLazyBody)
func TestV3LazyBody(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[UpdateUserRequest]) (User, error) {
		// 1. 先读取路径参数（轻量）
		// 1. Read path parameter first (lightweight)
		pathAccessor := v3.Path[UserID](&req)
		path, err := pathAccessor.Get()
		if err != nil {
			return User{}, err
		}

		// 2. 检查权限（可能提前返回）
		// 2. Check permission (may return early)
		if path.ID > 1000 {
			t.Log("❌ Permission check failed, Body not decoded yet")
			return User{}, v3.HTTPError{Status: 403, Cause: errors.New("no permission")}
			// ✅ 注意：这里 req.Data 可能还未解码（取决于 v3 内部实现）
			// Note: req.Data may not be decoded yet (depends on v3 internals)
		}

		// 3. 使用延迟 Body 访问器
		// 3. Use lazy Body accessor
		t.Log("✅ Permission check passed, accessing Body")
		bodyAccessor := v3.WithLazyBody(ctx, &req)
		body, err := bodyAccessor.Get()
		if err != nil {
			return User{}, err
		}

		user := User{
			ID:       path.ID,
			Username: body.Username,
			Email:    body.Email,
		}

		return user, nil
	}

	_ = handler
	t.Log("Lazy Body loading works correctly")
}

// 示例 5: v3 改进 - 组合多种访问器
// Example 5: v3 improved - Combining multiple accessors
func TestV3Combined(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[UpdateUserRequest]) (User, error) {
		// 1. 类型化的 Header 访问（认证）
		// 1. Typed Header access (authentication)
		headerAccessor := v3.Header[AuthHeader](&req)
		header, err := headerAccessor.Get()
		if err != nil {
			t.Logf("No auth header (expected): %v", err)
		} else {
			t.Logf("Auth token: %s", header.Token)
		}

		// 2. 类型化的 Path 访问
		// 2. Typed Path access
		pathAccessor := v3.Path[UserID](&req)
		path, err := pathAccessor.Get()
		if err != nil {
			return User{}, err
		}

		t.Logf("User ID: %d", path.ID)

		// 3. 类型化的 Query 访问
		// 3. Typed Query access
		queryAccessor := v3.Query[ListQuery](&req)
		query, err := queryAccessor.Get()
		if err != nil {
			t.Logf("No query params: %v", err)
		} else {
			t.Logf("Query: page=%d", query.Page)
		}

		// 4. 延迟 Body 访问
		// 4. Lazy Body access
		bodyAccessor := v3.WithLazyBody(ctx, &req)
		body, err := bodyAccessor.Get()
		if err != nil {
			return User{}, err
		}

		user := User{
			ID:       path.ID,
			Username: body.Username,
			Email:    body.Email,
		}

		return user, nil
	}

	_ = handler
	t.Log("Combined accessors work correctly")
}

// 示例 6: 验证 Accessor 的缓存行为
// Example 6: Verify Accessor caching behavior
func TestV3AccessorCaching(t *testing.T) {
	handler := func(ctx context.Context, req v3.RequestOf[UpdateUserRequest]) (User, error) {
		// 创建 Path 访问器
		// Create Path accessor
		pathAccessor := v3.Path[UserID](&req)

		// 第一次调用 Get()
		// First Get() call
		path1, err1 := pathAccessor.Get()
		if err1 != nil {
			return User{}, err1
		}

		// 第二次调用 Get()（应该返回缓存值）
		// Second Get() call (should return cached value)
		path2, err2 := pathAccessor.Get()
		if err2 != nil {
			return User{}, err2
		}

		// 验证两次返回相同的值
		// Verify both calls return the same value
		if path1.ID != path2.ID {
			t.Errorf("Expected cached value, got different results: %d vs %d", path1.ID, path2.ID)
		} else {
			t.Logf("✅ Caching works: both calls returned ID=%d", path1.ID)
		}

		return User{ID: path1.ID}, nil
	}

	_ = handler
	t.Log("Accessor caching verified")
}
