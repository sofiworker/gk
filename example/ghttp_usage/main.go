// Package main implements a realistic blog API server using ghttp.
// 目的：从使用者角度真实地使用 ghttp 的 server 端 API，发现设计不足和易用性问题。
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

// ============================================================================
// 领域模型
// ============================================================================

type Post struct {
	ID        string    `json:"id" doc:"帖子ID" example:"post-001"`
	Title     string    `json:"title" doc:"标题" required:"true" minLength:"1" maxLength:"200"`
	Content   string    `json:"content" doc:"正文" required:"true"`
	Author    string    `json:"author" doc:"作者"`
	Tags      []string  `json:"tags,omitempty" doc:"标签列表"`
	CreatedAt time.Time `json:"createdAt" doc:"创建时间"`
	UpdatedAt time.Time `json:"updatedAt" doc:"更新时间"`
}

type Comment struct {
	ID       string `json:"id" doc:"评论ID"`
	PostID   string `json:"postId" doc:"所属帖子ID"`
	Author   string `json:"author" doc:"评论者" required:"true"`
	Content  string `json:"content" doc:"评论内容" required:"true"`
	ParentID string `json:"parentId,omitempty" doc:"父评论ID（楼中楼）"`
}

// ============================================================================
// 输入结构体
// ============================================================================

// ListPostsInput — 只需要 query 参数和 path 参数，无请求体
type ListPostsInput struct {
	ghttp.Params `json:"-"`
}

// GetPostInput — path 参数获取单个帖子
type GetPostInput struct {
	ghttp.Params `json:"-"`
}

// CreatePostInput — 需要请求体
type CreatePostInput struct {
	ghttp.Params `json:"-"`

	Body struct {
		Title   string   `json:"title" validate:"required,min=1,max=200" doc:"标题"`
		Content string   `json:"content" validate:"required" doc:"正文"`
		Author  string   `json:"author" validate:"required" doc:"作者"`
		Tags    []string `json:"tags" doc:"标签"`
	} `json:"body"`
}

// UpdatePostInput — PUT 更新帖子
type UpdatePostInput struct {
	ghttp.Params `json:"-"`

	Body struct {
		Title   *string  `json:"title" doc:"标题"`
		Content *string  `json:"content" doc:"正文"`
		Tags    []string `json:"tags" doc:"标签"`
	} `json:"body"`
}

// CreateCommentInput — 创建评论
type CreateCommentInput struct {
	ghttp.Params `json:"-"`

	Body struct {
		Author   string `json:"author" validate:"required" doc:"评论者"`
		Content  string `json:"content" validate:"required" doc:"评论内容"`
		ParentID string `json:"parentId" doc:"父评论ID"`
	} `json:"body"`
}

// SearchInput — 搜索帖子
type SearchInput struct {
	ghttp.Params `json:"-"`
}

// UploadImageInput — 文件上传
type UploadImageInput struct {
	ghttp.Params `json:"-"`

	Body struct {
		Description string `json:"description" form:"description" doc:"图片描述"`
	} `json:"body"`
}

// ============================================================================
// 输出结构体
// ============================================================================

type ListPostsOutput struct {
	Posts    []Post `json:"posts"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type CreatePostOutput struct {
	Post Post `json:"post"`
}

type UpdatePostOutput struct {
	Post Post `json:"post"`
}

type CreateCommentOutput struct {
	Comment Comment `json:"comment"`
}

type SearchOutput struct {
	Results []Post `json:"results"`
	Keyword string `json:"keyword"`
}

type ErrorOutput struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// DeleteOutput 演示无响应体输出
type DeleteOutput struct{}

// UploadOutput 带有自定义状态码
type UploadOutput struct {
	Status      int    `json:"-"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
}

func (o UploadOutput) StatusCode() int {
	return o.Status
}

// LoginOutput 演示 Cookie 写入
type LoginOutput struct {
	Token string `json:"token"`
}

func (o LoginOutput) Cookies() []*http.Cookie {
	return []*http.Cookie{
		{
			Name:     "session_id",
			Value:    o.Token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		},
	}
}

// ============================================================================
// 内存存储（模拟数据库）
// ============================================================================

type postStore struct {
	posts map[string]Post
}

func newPostStore() *postStore {
	return &postStore{posts: make(map[string]Post)}
}

func (s *postStore) List(page, pageSize int) ([]Post, int) {
	all := make([]Post, 0, len(s.posts))
	for _, p := range s.posts {
		all = append(all, p)
	}
	total := len(all)
	start := (page - 1) * pageSize
	if start >= total {
		return []Post{}, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return all[start:end], total
}

func (s *postStore) Get(id string) (Post, bool) {
	p, ok := s.posts[id]
	return p, ok
}

func (s *postStore) Create(p Post) {
	s.posts[p.ID] = p
}

func (s *postStore) Update(id string, p Post) {
	s.posts[id] = p
}

func (s *postStore) Delete(id string) {
	delete(s.posts, id)
}

// ============================================================================
// 构建服务器
// ============================================================================

func buildServer() *ghttp.Server {
	store := newPostStore()

	// 预填充一些数据
	now := time.Now()
	store.Create(Post{ID: "1", Title: "Hello World", Content: "First post", Author: "admin", CreatedAt: now, UpdatedAt: now})
	store.Create(Post{ID: "2", Title: "Go Generics", Content: "Deep dive", Author: "admin", CreatedAt: now, UpdatedAt: now})

	s := ghttp.New(
		ghttp.WithAddress(":8080"),
		ghttp.WithProduces(ghttp.MIMEJSON),
		ghttp.WithConsumes(ghttp.MIMEJSON),
		ghttp.WithOpenAPI("Blog API", "1.0.0"),
		ghttp.WithMaxBodyBytes(10<<20), // 10MB
	)

	// 全局中间件
	s.Use(ghttp.RequestID())
	s.Use(ghttp.Recoverer())
	s.Use(ghttp.CORS(ghttp.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions},
		AllowHeaders:     []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           3600,
	}))

	// API v1 路由组
	v1 := s.Group("/api/v1")
	v1.Use(ghttp.RequestLogger())

	// --- 帖子 CRUD ---

	// 列出帖子（支持分页 query 参数）
	ghttp.Route[ListPostsInput, ListPostsOutput](v1).
		GET("/posts").
		Doc(ghttp.Summary("获取帖子列表")).
		To(func(ctx context.Context, req ListPostsInput) (ListPostsOutput, error) {
			// 问题1：没有 QueryInt 之类的类型化 query 读取方法
			// 使用者需要手动 strconv.Atoi
			page := 1
			pageSize := 10
			// 只能用 req.Query("page") 获取字符串
			if p := req.Query("page"); p != "" {
				// 手动解析
				var n int
				if _, err := fmt.Sscanf(p, "%d", &n); err == nil && n > 0 {
					page = n
				}
			}
			if ps := req.Query("pageSize"); ps != "" {
				var n int
				if _, err := fmt.Sscanf(ps, "%d", &n); err == nil && n > 0 && n <= 100 {
					pageSize = n
				}
			}

			posts, total := store.List(page, pageSize)
			return ListPostsOutput{
				Posts:    posts,
				Total:    total,
				Page:     page,
				PageSize: pageSize,
			}, nil
		})

	// 获取单个帖子
	ghttp.Route[GetPostInput, Post](v1).
		GET("/posts/{id}").
		Doc(ghttp.Summary("获取单个帖子")).
		To(func(ctx context.Context, req GetPostInput) (Post, error) {
			id := req.Path("id")
			post, ok := store.Get(id)
			if !ok {
				return Post{}, ghttp.NotFound("post not found: " + id)
			}
			return post, nil
		})

	// 创建帖子
	ghttp.Route[CreatePostInput, CreatePostOutput](v1).
		POST("/posts").
		Doc(ghttp.Summary("创建帖子")).
		Consumes(ghttp.MIMEJSON).
		Produces(ghttp.MIMEJSON).
		To(func(ctx context.Context, req CreatePostInput) (CreatePostOutput, error) {
			now := time.Now()
			post := Post{
				ID:        fmt.Sprintf("post-%d", now.UnixNano()),
				Title:     req.Body.Title,
				Content:   req.Body.Content,
				Author:    req.Body.Author,
				Tags:      req.Body.Tags,
				CreatedAt: now,
				UpdatedAt: now,
			}
			store.Create(post)
			return CreatePostOutput{Post: post}, nil
		})

	// 更新帖子
	ghttp.Route[UpdatePostInput, UpdatePostOutput](v1).
		PUT("/posts/{id}").
		Doc(ghttp.Summary("更新帖子")).
		To(func(ctx context.Context, req UpdatePostInput) (UpdatePostOutput, error) {
			id := req.Path("id")
			post, ok := store.Get(id)
			if !ok {
				return UpdatePostOutput{}, ghttp.NotFound("post not found: " + id)
			}
			if req.Body.Title != nil {
				post.Title = *req.Body.Title
			}
			if req.Body.Content != nil {
				post.Content = *req.Body.Content
			}
			if req.Body.Tags != nil {
				post.Tags = req.Body.Tags
			}
			post.UpdatedAt = time.Now()
			store.Update(id, post)
			return UpdatePostOutput{Post: post}, nil
		})

	// 删除帖子
	ghttp.Route[GetPostInput, DeleteOutput](v1).
		DELETE("/posts/{id}").
		Doc(ghttp.Summary("删除帖子")).
		To(func(ctx context.Context, req GetPostInput) (DeleteOutput, error) {
			id := req.Path("id")
			if _, ok := store.Get(id); !ok {
				return DeleteOutput{}, ghttp.NotFound("post not found: " + id)
			}
			store.Delete(id)
			return DeleteOutput{}, nil
		})

	// 搜索帖子
	ghttp.Route[SearchInput, SearchOutput](v1).
		GET("/posts/search").
		Doc(ghttp.Summary("搜索帖子")).
		To(func(ctx context.Context, req SearchInput) (SearchOutput, error) {
			keyword := req.Query("q")
			if keyword == "" {
				return SearchOutput{}, ghttp.BadRequest("query parameter 'q' is required")
			}
			// 简单的内存搜索
			var results []Post
			for _, p := range store.posts {
				if contains(p.Title, keyword) || contains(p.Content, keyword) {
					results = append(results, p)
				}
			}
			return SearchOutput{Results: results, Keyword: keyword}, nil
		})

	// --- 评论 ---

	ghttp.Route[CreateCommentInput, CreateCommentOutput](v1).
		POST("/posts/{id}/comments").
		Doc(ghttp.Summary("创建评论")).
		To(func(ctx context.Context, req CreateCommentInput) (CreateCommentOutput, error) {
			postID := req.Path("id")
			if _, ok := store.Get(postID); !ok {
				return CreateCommentOutput{}, ghttp.NotFound("post not found: " + postID)
			}
			comment := Comment{
				ID:       fmt.Sprintf("comment-%d", time.Now().UnixNano()),
				PostID:   postID,
				Author:   req.Body.Author,
				Content:  req.Body.Content,
				ParentID: req.Body.ParentID,
			}
			return CreateCommentOutput{Comment: comment}, nil
		})

	// --- SSE 推送 ---

	ghttp.Route[ghttp.Params, struct{}](v1).
		GET("/events").
		Doc(ghttp.Summary("SSE 事件推送")).
		ToSSE(func(ctx context.Context, params ghttp.Params, stream *ghttp.SSEWriter) error {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			count := 0
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
					if count >= 5 {
						return nil
					}
				}
			}
		})

	// --- 登录（演示 Cookie 写入）---

	ghttp.Route[struct {
		ghttp.Params `json:"-"`

		Body struct {
			Username string `json:"username" validate:"required"`
			Password string `json:"password" validate:"required"`
		} `json:"body"`
	}, LoginOutput](v1).
		POST("/auth/login").
		Doc(ghttp.Summary("用户登录")).
		To(func(ctx context.Context, req struct {
			ghttp.Params `json:"-"`

			Body struct {
				Username string `json:"username" validate:"required"`
				Password string `json:"password" validate:"required"`
			} `json:"body"`
		}) (LoginOutput, error) {
			if req.Body.Username == "admin" && req.Body.Password == "password" {
				return LoginOutput{Token: "mock-jwt-token"}, nil
			}
			return LoginOutput{}, ghttp.Err(http.StatusUnauthorized, "invalid credentials")
		})

	// --- 静态文件服务 ---

	ghttp.Route[struct{}, struct{}](s).
		GET("/static").
		ToStatic("./public")

	// --- 健康检查 ---

	ghttp.Route[struct{}, struct {
		Status string `json:"status"`
		Time   string `json:"time"`
	}](s).
		GET("/health").
		Doc(ghttp.Summary("健康检查")).
		To(func(ctx context.Context, req struct{}) (struct {
			Status string `json:"status"`
			Time   string `json:"time"`
		}, error) {
			return struct {
				Status string `json:"status"`
				Time   string `json:"time"`
			}{
				Status: "ok",
				Time:   time.Now().Format(time.RFC3339),
			}, nil
		})

	return s
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func main() {
	s := buildServer()
	fmt.Println("Blog API server starting on :8080")
	fmt.Println("OpenAPI docs at http://localhost:8080/openapi.json")
	if err := s.Run(); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}
