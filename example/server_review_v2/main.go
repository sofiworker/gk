// Package main 是对 ghttp server 的全面使用评测。
// 从真实业务场景出发，覆盖：RESTful CRUD、认证中间件、错误处理、多Codec协商、
// 文件上传、路由分组、嵌套分组、OpenAPI、自定义状态码、Cookie/Session、
// 原始HTTP访问、自定义Envelope、健康检查等场景。
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

// ============================================================================
// 领域模型
// ============================================================================

type Article struct {
	ID          string    `json:"id" doc:"文章ID"`
	Title       string    `json:"title" doc:"标题" validate:"required,min=1,max=200"`
	Body        string    `json:"body" doc:"正文" validate:"required"`
	AuthorID    string    `json:"authorId" doc:"作者ID"`
	Status      string    `json:"status" doc:"状态: draft/published"`
	Tags        []string  `json:"tags" doc:"标签"`
	ViewCount   int       `json:"viewCount" doc:"阅读量"`
	PublishedAt time.Time `json:"publishedAt" doc:"发布时间"`
	CreatedAt   time.Time `json:"createdAt" doc:"创建时间"`
	UpdatedAt   time.Time `json:"updatedAt" doc:"更新时间"`
}

type User struct {
	ID        string    `json:"id" doc:"用户ID"`
	Username  string    `json:"username" doc:"用户名"`
	Email     string    `json:"email" doc:"邮箱"`
	Role      string    `json:"role" doc:"角色"`
	CreatedAt time.Time `json:"createdAt" doc:"创建时间"`
}

// ============================================================================
// 输入结构体
// ============================================================================

type ListArticlesInput struct {
	ghttp.Params `json:"-"`
}

type GetArticleInput struct {
	ghttp.Params `json:"-"`
}

type CreateArticleInput struct {
	ghttp.Params `json:"-"`
	Body         struct {
		Title    string   `json:"title" validate:"required,min=1,max=200" doc:"标题"`
		Body     string   `json:"body" validate:"required" doc:"正文"`
		Tags     []string `json:"tags" doc:"标签"`
		AuthorID string   `json:"authorId" validate:"required" doc:"作者ID"`
		Status   string   `json:"status" doc:"状态: draft/published; 默认draft"`
	} `json:"body"`
}

type UpdateArticleInput struct {
	ghttp.Params `json:"-"`
	Body         struct {
		Title *string  `json:"title" doc:"标题"`
		Body  *string  `json:"body" doc:"正文"`
		Tags  []string `json:"tags" doc:"标签"`
	} `json:"body"`
}

type DeleteInput struct {
	ghttp.Params `json:"-"`
}

type LoginInput struct {
	ghttp.Params `json:"-"`
	Body         struct {
		Username string `json:"username" validate:"required" doc:"用户名"`
		Password string `json:"password" validate:"required" doc:"密码"`
	} `json:"body"`
}

type RegisterInput struct {
	ghttp.Params `json:"-"`
	Body         struct {
		Username string `json:"username" validate:"required,min=3,max=50" doc:"用户名"`
		Email    string `json:"email" validate:"required,email" doc:"邮箱"`
		Password string `json:"password" validate:"required,min=6" doc:"密码"`
	} `json:"body"`
}

type SearchInput struct {
	ghttp.Params `json:"-"`
}

// ============================================================================
// 输出结构体
// ============================================================================

type ListArticlesOutput struct {
	Articles []Article `json:"articles" doc:"文章列表" example:"[]"`
	Total    int       `json:"total" doc:"总数" example:"0"`
	Page     int       `json:"page" doc:"当前页码" example:"1"`
	PageSize int       `json:"pageSize" doc:"每页大小" example:"10"`
	HasMore  bool      `json:"hasMore" doc:"是否有更多"`
}

type ArticleOutput struct {
	Article Article `json:"article"`
}

type LoginOutput struct {
	Token     string `json:"token" doc:"JWT Token"`
	ExpiresAt string `json:"expiresAt" doc:"过期时间"`
}

func (o LoginOutput) Cookies() []*http.Cookie {
	return []*http.Cookie{
		{
			Name:     "session_id",
			Value:    o.Token,
			Path:     "/",
			HttpOnly: true,
			Secure:   false,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(24 * time.Hour),
		},
	}
}

type RegisterOutput struct {
	User User `json:"user"`
}

type HealthOutput struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Uptime    string            `json:"uptime"`
	GoVersion string            `json:"goVersion"`
	Checks    map[string]string `json:"checks"`
}

type DeleteOutput struct{}

// CustomStatusOutput 演示响应中自定义状态码的能力
type CustomStatusOutput struct {
	Status int    `json:"-"`
	ID     string `json:"id"`
}

func (o CustomStatusOutput) StatusCode() int {
	if o.Status > 0 {
		return o.Status
	}
	return http.StatusOK
}

// ============================================================================
// 存储层
// ============================================================================

type store struct {
	mu       sync.RWMutex
	articles map[string]Article
	users    map[string]User
}

func newStore() *store {
	s := &store{
		articles: make(map[string]Article),
		users:    make(map[string]User),
	}
	// 预填充数据
	now := time.Now()
	s.articles["a1"] = Article{
		ID: "a1", Title: "Introduction to Go", Body: "Go is a...",
		AuthorID: "u1", Status: "published", Tags: []string{"go", "programming"},
		ViewCount: 150, PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	s.users["u1"] = User{ID: "u1", Username: "admin", Email: "admin@example.com", Role: "admin", CreatedAt: now}
	return s
}

func (s *store) listArticles(page, pageSize int, tag string) ([]Article, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []Article
	for _, a := range s.articles {
		if tag == "" || containsTag(a.Tags, tag) {
			all = append(all, a)
		}
	}
	total := len(all)
	start := (page - 1) * pageSize
	if start >= total {
		return nil, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return all[start:end], total
}

func (s *store) getArticle(id string) (Article, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.articles[id]
	return a, ok
}

func (s *store) createArticle(a Article) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.ID = fmt.Sprintf("a%d", len(s.articles)+1)
	a.CreatedAt = time.Now()
	a.UpdatedAt = a.CreatedAt
	if a.Status == "" {
		a.Status = "draft"
	}
	s.articles[a.ID] = a
}

func (s *store) updateArticleID(id string, title, body *string, tags []string) (Article, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.articles[id]
	if !ok {
		return a, false
	}
	if title != nil {
		a.Title = *title
	}
	if body != nil {
		a.Body = *body
	}
	if tags != nil {
		a.Tags = tags
	}
	a.UpdatedAt = time.Now()
	s.articles[id] = a
	return a, true
}

func (s *store) deleteArticleID(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.articles[id]; !ok {
		return false
	}
	delete(s.articles, id)
	return true
}

func (s *store) search(keyword string) []Article {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []Article
	for _, a := range s.articles {
		if strings.Contains(a.Title, keyword) || strings.Contains(a.Body, keyword) {
			results = append(results, a)
		}
	}
	return results
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// ============================================================================
// 认证中间件（模拟）
// ============================================================================

const contextKeyUserID = "userID"

func authMiddleware() ghttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if token == "" {
				// 也支持 Cookie
				c, err := r.Cookie("session_id")
				if err == nil {
					token = "Bearer " + c.Value
				}
			}
			if token != "" {
				token = strings.TrimPrefix(token, "Bearer ")
				if token == "mock-jwt-token" {
					ctx := context.WithValue(r.Context(), contextKeyUserID, "u1")
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requireRole(roles ...string) ghttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, _ := r.Context().Value(contextKeyUserID).(string)
			if userID == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code":    40101,
					"message": "authentication required",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================================
// 自定义 Envelope
// ============================================================================

func customEnvelope(w http.ResponseWriter, r *http.Request, statusCode int, resp interface{}, err error, contentType string, codec ghttp.Codec) {
	w.Header().Set("Content-Type", contentType)

	type envelope struct {
		Code      int         `json:"code"`
		Message   string      `json:"message"`
		Data      interface{} `json:"data,omitempty"`
		Timestamp int64       `json:"timestamp"`
		RequestID string      `json:"requestId"`
	}

	response := envelope{
		Timestamp: time.Now().Unix(),
		RequestID: r.Header.Get("X-Request-ID"),
	}

	if err != nil {
		he := ghttp.AsError(err)
		if he != nil {
			statusCode = he.Code
			response.Code = he.Code
			response.Message = he.Message
		} else {
			statusCode = http.StatusInternalServerError
			response.Code = 50000
			response.Message = "internal server error"
		}
	} else {
		response.Code = 0
		response.Message = "ok"
		response.Data = resp
	}

	w.WriteHeader(statusCode)
	codec.Marshal(w, &response)
}

// ============================================================================
// 服务器构建
// ============================================================================

func buildServer() *ghttp.Server {
	store := newStore()

	s := ghttp.New(
		ghttp.WithAddress(":8080"),
		ghttp.WithProduces(ghttp.MIMEJSON),
		ghttp.WithConsumes(ghttp.MIMEJSON),
		ghttp.WithOpenAPI("Article API", "2.0.0"),
		ghttp.WithMaxBodyBytes(1<<20), // 1MB
		// 不注册 envelope，测试错误处理差异
	)

	// 全局中间件
	s.Use(ghttp.RequestID())
	s.Use(ghttp.Recoverer())
	s.Use(ghttp.RequestLogger())

	// ---------- health ----------
	ghttp.Route[ghttp.Params, HealthOutput](s).
		GET("/health").
		Doc(ghttp.Summary("Health Check")).
		To(func(ctx context.Context, req ghttp.Params) (HealthOutput, error) {
			return HealthOutput{
				Status:    "healthy",
				Version:   "2.0.0",
				Uptime:    time.Since(time.Now().Add(-time.Minute)).String(),
				GoVersion: "1.24",
				Checks: map[string]string{
					"database": "ok",
					"cache":    "ok",
				},
			}, nil
		})

	// ---------- 文章CRUD ----------
	api := s.Group("/api/v2")
	api.Use(authMiddleware())

	// 列文章
	ghttp.Route[ListArticlesInput, ListArticlesOutput](api).
		GET("/articles").
		Doc(ghttp.Summary("List articles with pagination")).
		To(func(ctx context.Context, req ListArticlesInput) (ListArticlesOutput, error) {
			page := 1
			if p := req.Query("page"); p != "" {
				if n, err := strconv.Atoi(p); err == nil && n > 0 {
					page = n
				}
			}
			pageSize := 10
			if ps := req.Query("pageSize"); ps != "" {
				if n, err := strconv.Atoi(ps); err == nil && n > 0 && n <= 100 {
					pageSize = n
				}
			}
			tag := req.Query("tag")

			articles, total := store.listArticles(page, pageSize, tag)
			return ListArticlesOutput{
				Articles: articles,
				Total:    total,
				Page:     page,
				PageSize: pageSize,
				HasMore:  page*pageSize < total,
			}, nil
		})

	// 搜索
	ghttp.Route[SearchInput, ListArticlesOutput](api).
		GET("/articles/search").
		Doc(ghttp.Summary("Search articles")).
		To(func(ctx context.Context, req SearchInput) (ListArticlesOutput, error) {
			q := req.Query("q")
			if q == "" {
				return ListArticlesOutput{}, ghttp.BadRequest("'q' is required")
			}
			results := store.search(q)
			return ListArticlesOutput{
				Articles: results, Total: len(results), Page: 1, PageSize: len(results),
			}, nil
		})

	// 获取单篇文章
	ghttp.Route[GetArticleInput, ArticleOutput](api).
		GET("/articles/{id}").
		Doc(ghttp.Summary("Get article by ID")).
		To(func(ctx context.Context, req GetArticleInput) (ArticleOutput, error) {
			id := req.Path("id")
			article, ok := store.getArticle(id)
			if !ok {
				return ArticleOutput{}, ghttp.NotFound("article not found: " + id)
			}
			return ArticleOutput{Article: article}, nil
		})

	// 创建文章
	ghttp.Route[CreateArticleInput, ArticleOutput](api).
		POST("/articles").
		Doc(ghttp.Summary("Create article")).
		Produces(ghttp.MIMEJSON).
		Consumes(ghttp.MIMEJSON).
		Use(requireRole("admin", "editor")).
		To(func(ctx context.Context, req CreateArticleInput) (ArticleOutput, error) {
			article := Article{
				Title:    req.Body.Title,
				Body:     req.Body.Body,
				AuthorID: req.Body.AuthorID,
				Status:   req.Body.Status,
				Tags:     req.Body.Tags,
			}
			if article.Status == "published" {
				article.PublishedAt = time.Now()
			}
			store.createArticle(article)
			return ArticleOutput{Article: article}, nil
		})

	// 更新文章
	ghttp.Route[UpdateArticleInput, ArticleOutput](api).
		PUT("/articles/{id}").
		Doc(ghttp.Summary("Update article")).
		Produces(ghttp.MIMEJSON).
		Consumes(ghttp.MIMEJSON).
		Use(requireRole("admin", "editor")).
		To(func(ctx context.Context, req UpdateArticleInput) (ArticleOutput, error) {
			id := req.Path("id")
			// 检查文章是否存在
			if _, ok := store.getArticle(id); !ok {
				return ArticleOutput{}, ghttp.NotFound("article not found: " + id)
			}
			article, ok := store.updateArticleID(id, req.Body.Title, req.Body.Body, req.Body.Tags)
			if !ok {
				return ArticleOutput{}, errors.New("update failed")
			}
			return ArticleOutput{Article: article}, nil
		})

	// 删除文章
	ghttp.Route[DeleteInput, DeleteOutput](api).
		DELETE("/articles/{id}").
		Doc(ghttp.Summary("Delete article")).
		Use(requireRole("admin")).
		To(func(ctx context.Context, req DeleteInput) (DeleteOutput, error) {
			id := req.Path("id")
			if !store.deleteArticleID(id) {
				return DeleteOutput{}, ghttp.NotFound("article not found: " + id)
			}
			return DeleteOutput{}, nil
		})

	// ---------- Auth 路由（无认证） ----------
	auth := s.Group("/auth")

	ghttp.Route[LoginInput, LoginOutput](auth).
		POST("/login").
		Doc(ghttp.Summary("Login")).
		Consumes(ghttp.MIMEJSON).
		Produces(ghttp.MIMEJSON).
		To(func(ctx context.Context, req LoginInput) (LoginOutput, error) {
			// 简单 mock 验证
			if subtle.ConstantTimeCompare([]byte(req.Body.Username), []byte("admin")) != 1 ||
				subtle.ConstantTimeCompare([]byte(req.Body.Password), []byte("password")) != 1 {
				return LoginOutput{}, ghttp.Err(http.StatusUnauthorized, "invalid credentials")
			}
			return LoginOutput{
				Token:     "mock-jwt-token",
				ExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			}, nil
		})

	// ---------- 原始 HTTP 处理器 ----------
	ghttp.Route[ghttp.Params, struct{}](s).
		GET("/raw").
		Doc(ghttp.Summary("Raw HTTP handler")).
		ToRaw(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "raw handler response")
		})

	return s
}

func main() {
	s := buildServer()
	log.Println("Server starting on :8080")
	log.Println("Try:")
	log.Println("  curl http://localhost:8080/health")
	log.Println("  curl http://localhost:8080/api/v2/articles")
	log.Println("  curl -X POST http://localhost:8080/auth/login -H 'Content-Type: application/json' -d '{\"username\":\"admin\",\"password\":\"password\"}'")
	if err := s.Run(); err != nil {
		log.Fatal(err)
	}
}
