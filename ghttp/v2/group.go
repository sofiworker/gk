package v2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	root "github.com/sofiworker/gk/ghttp"
)

// API 是未匹配路由的 v2 注册器，可把路由挂载到现有 ghttp Server 或 Group。
// API is the v2 registrar; it can mount routes on an existing ghttp Server or Group.
type API struct {
	group *Group
}

// Group 保存路径前缀和继承配置。
// Group stores a path prefix and inherited configuration.
type Group struct {
	prefix string
	parent *Group
	opts   []GroupOption
	target Registrar
}

// GroupOption 配置组级默认值。
// GroupOption configures group defaults.
type GroupOption func(*routeOptions)

// New 创建根注册器。
// New creates a root registrar.
func New(target ...Registrar) *API {
	g := &Group{}
	if len(target) > 0 {
		g.target = target[0]
	}
	return &API{group: g}
}

// Registrar 是挂载端点所需的最小接口。
// Registrar is the minimal interface needed to mount endpoints.
type Registrar interface {
	RawHandle(string, string, root.RawHandlerFunc) error
}

// Register 将根级路由挂载到创建 API 时指定的目标。
// Register mounts root routes on the target supplied to New.
func (a *API) Register(routes ...Route) error { return a.group.Register(routes...) }

// With 追加根级默认配置。
// With appends root defaults.
func (a *API) With(opts ...Option) *API { a.group.With(opts...); return a }

// With 追加组默认配置，仅影响后续注册。
// With appends group defaults affecting subsequent registrations only.
func (g *Group) With(opts ...Option) *Group {
	for _, opt := range opts {
		g.opts = append(g.opts, GroupOption(opt))
	}
	return g
}

// Register 先校验批次再逐条安装；底层安装失败不回滚已安装端点。
// Register validates the batch before installation; backend failures do not roll back installed routes.
func (g *Group) Register(routes ...Route) error {
	if g == nil || nilCodec(g.target) {
		return errors.New("ghttp/v2: missing registration target")
	}
	_, err := Routes(g, g.target, routes...)
	return err
}

// Group 创建继承当前配置的子组。
// Group creates a child group inheriting the current configuration.
func (a *API) Group(prefix string, opts ...GroupOption) *Group {
	return a.group.Group(prefix, opts...)
}

// Group 创建嵌套组；子组路径相对于父组。
// Group creates a nested group whose path is relative to its parent.
func (g *Group) Group(prefix string, opts ...GroupOption) *Group {
	return &Group{prefix: joinPrefix(g.prefix, prefix), parent: g, opts: cloneGroupOptions(opts), target: g.target}
}

// Prefix 返回累积后的路径前缀。
// Prefix returns the accumulated path prefix.
func (g *Group) Prefix() string {
	if g == nil {
		return ""
	}
	return g.prefix
}

// Routes 使用组配置构造多条路由；可选 target 非空时逐条挂载。
// Routes builds routes with group options and optionally mounts them to target.
func Routes(g *Group, target any, routes ...Route) ([]Route, error) {
	if g == nil {
		return nil, errors.New("ghttp/v2: nil group")
	}
	result := make([]Route, 0, len(routes))
	seen := map[string]bool{}
	for _, r := range routes {
		fullPath := joinPrefix(g.prefix, r.Path)
		if r.compile == nil {
			return nil, fmt.Errorf("%s %s: uninitialized route", r.Method, fullPath)
		}
		compiled := r.compile(fullPath, groupOptions(g))
		if err := compiled.Err(); err != nil {
			return nil, fmt.Errorf("%s %s: %w", r.Method, fullPath, err)
		}
		key := r.Method + " " + fullPath
		if seen[key] {
			return nil, fmt.Errorf("%s: %w", key, root.ErrDuplicateRoute)
		}
		seen[key] = true
		result = append(result, compiled)
	}
	if target != nil {
		for i, r := range result {
			if err := r.Mount(target); err != nil {
				return result[:i], fmt.Errorf("%s %s: %w", r.Method, r.Path, err)
			}
		}
	}
	return result, nil
}

func joinPrefix(base, prefix string) string {
	if base == "" || base == "/" {
		if prefix == "" {
			return "/"
		}
		return "/" + strings.Trim(prefix, "/")
	}
	if prefix == "" || prefix == "/" {
		return base
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(prefix, "/")
}

func cloneGroupOptions(opts []GroupOption) []GroupOption { return append([]GroupOption(nil), opts...) }

// Use 为该组及后代路由追加 middleware，调用顺序为父到子再到路由级。
// Use appends middleware inherited by this group and descendants, from parent to child.
func (g *Group) Use(middleware ...root.Middleware) *Group {
	snapshot := append([]root.Middleware(nil), middleware...)
	g.opts = append(g.opts, func(c *routeOptions) { c.middleware = append(c.middleware, snapshot...) })
	return g
}

// GroupBodyLimit 设置该组继承的请求体大小上限。
// GroupBodyLimit sets the inherited request body size limit.
func GroupBodyLimit(bytes int64) GroupOption {
	return func(c *routeOptions) { c.maxBodyBytes = bytes }
}

// GroupInput 设置组级默认输入；路由 handler 的输入类型必须与其匹配。
// GroupInput sets a group default input; route handler input types must match.
func GroupInput[I any](in Input[I]) GroupOption {
	return func(c *routeOptions) { c.input = in; c.inputSet = true }
}

// GroupOutput 设置组级默认输出；路由 handler 的输出类型必须与其匹配。
// GroupOutput sets a group default output; route handler output types must match.
func GroupOutput[O any](out Output[O]) GroupOption {
	return func(c *routeOptions) { c.output = out; c.outputSet = true }
}

// Handle 用父子组配置创建、挂载并返回端点。
// Handle creates, mounts and returns an endpoint using inherited group options.
func Handle[I, O any](g *Group, method, routePath string, target any, h func(context.Context, I) (O, error), opts ...Option) (Route, error) {
	if g == nil {
		return Route{}, errors.New("ghttp/v2: nil group")
	}
	all := groupOptions(g)
	all = append(all, opts...)
	r := Method(method, joinPrefix(g.prefix, routePath), h, all...)
	if err := r.Err(); err != nil {
		return r, fmt.Errorf("%s %s: %w", r.Method, r.Path, err)
	}
	if target != nil {
		if err := r.Mount(target); err != nil {
			return r, fmt.Errorf("%s %s: %w", r.Method, r.Path, err)
		}
	}
	return r, nil
}

func groupOptions(g *Group) []Option {
	var chain []*Group
	for current := g; current != nil; current = current.parent {
		chain = append(chain, current)
	}
	var opts []Option
	for i := len(chain) - 1; i >= 0; i-- {
		for _, opt := range chain[i].opts {
			if opt != nil {
				opts = append(opts, Option(opt))
			}
		}
	}
	return opts
}

// Get 注册并挂载 GET 路由。
// Get registers and mounts a GET route.
func GetIn[I, O any](g *Group, target any, routePath string, h func(context.Context, I) (O, error), opts ...Option) (Route, error) {
	return Handle(g, http.MethodGet, routePath, target, h, opts...)
}

// Post 注册并挂载 POST 路由。
// Post registers and mounts a POST route.
func PostIn[I, O any](g *Group, target any, routePath string, h func(context.Context, I) (O, error), opts ...Option) (Route, error) {
	return Handle(g, http.MethodPost, routePath, target, h, opts...)
}

// PutIn 注册并挂载 PUT 路由。
// PutIn registers and mounts a PUT route.
func PutIn[I, O any](g *Group, target any, routePath string, h func(context.Context, I) (O, error), opts ...Option) (Route, error) {
	return Handle(g, http.MethodPut, routePath, target, h, opts...)
}

// PatchIn 注册并挂载 PATCH 路由。
// PatchIn registers and mounts a PATCH route.
func PatchIn[I, O any](g *Group, target any, routePath string, h func(context.Context, I) (O, error), opts ...Option) (Route, error) {
	return Handle(g, http.MethodPatch, routePath, target, h, opts...)
}

// DeleteIn 注册并挂载 DELETE 路由。
// DeleteIn registers and mounts a DELETE route.
func DeleteIn[I, O any](g *Group, target any, routePath string, h func(context.Context, I) (O, error), opts ...Option) (Route, error) {
	return Handle(g, http.MethodDelete, routePath, target, h, opts...)
}

// HeadIn 注册并挂载 HEAD 路由。
// HeadIn registers and mounts a HEAD route.
func HeadIn[I, O any](g *Group, target any, routePath string, h func(context.Context, I) (O, error), opts ...Option) (Route, error) {
	return Handle(g, http.MethodHead, routePath, target, h, opts...)
}

// OptionsIn 注册并挂载 OPTIONS 路由。
// OptionsIn registers and mounts a OPTIONS route.
func OptionsIn[I, O any](g *Group, target any, routePath string, h func(context.Context, I) (O, error), opts ...Option) (Route, error) {
	return Handle(g, http.MethodOptions, routePath, target, h, opts...)
}

// Mount 将已构造路由挂载到现有 ghttp Server/Group，并应用组前缀及 middleware。
// Mount attaches an existing route to ghttp Server/Group with this group's prefix and middleware.
func (g *Group) Mount(target any, r Route) error {
	_, err := Routes(g, target, r)
	return err
}
