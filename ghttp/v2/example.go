package v2

import (
	"context"
	"mime/multipart"
	"net/http"

	root "github.com/sofiworker/gk/ghttp"
)

// Example 展示 v2 门面的最小注册形态；它只返回路由描述，不启动服务器。
// Example shows the smallest v2 facade shape; it only returns route descriptions.
func Example() ([]Route, error) {
	api := New()

	// 组级 middleware 和前缀由子组继承。
	// Child groups inherit the prefix and middleware.
	users := api.Group("/users").Use(requireExampleAuth)
	admin := users.Group("/admin").Use(requireExampleAdmin)

	getUser, err := GetIn(users, nil, "/{username}", getExampleUser)
	if err != nil {
		return nil, err
	}

	// 路由级输入覆盖组默认值；handler 的输入输出可以独立使用指针。
	// Route input overrides group defaults; input and output pointers are independent.
	updateUser, err := Handle(users, http.MethodPatch, "/{username}", nil, updateExampleUser,
		WithInput(JSONInput[*exampleUpdateInput]()),
		WithOutput(JSONOutput[*exampleUser]()),
	)
	if err != nil {
		return nil, err
	}

	deleteUser, err := Handle(admin, http.MethodDelete, "/{username}", nil, func(ctx context.Context, in examplePathInput) (struct{}, error) {
		return struct{}{}, deleteExampleUser(ctx, in)
	})
	if err != nil {
		return nil, err
	}

	// multipart 文件输入：handler 在请求期间 Open/Close 文件。
	// Multipart input: the handler opens and closes files during the request.
	upload, err := Handle(users, http.MethodPost, "/{username}/attachments", nil, uploadExample,
		WithInput(MultipartInput[*exampleUpload]()),
	)
	if err != nil {
		return nil, err
	}

	// Reply 可携带状态码、Header 和 Cookie，同时保留类型化 body。
	// Reply carries status, headers and cookies while keeping a typed body.
	login := FromFunc(http.MethodPost, "/login", loginExample)
	if err := login.Err(); err != nil {
		return nil, err
	}

	return []Route{getUser, updateUser, deleteUser, upload, login}, nil
}

type exampleUser struct {
	Username string `json:"username"`
}

type exampleUpdateInput struct {
	Username string `path:"username"`
	Name     string `json:"name"`
}

type examplePathInput struct {
	Username string `path:"username"`
}

type exampleUpload struct {
	Username string                `path:"username"`
	File     *multipart.FileHeader `form:"file"`
}

func getExampleUser(_ context.Context, in examplePathInput) (exampleUser, error) {
	return exampleUser{Username: in.Username}, nil
}

func updateExampleUser(_ context.Context, in *exampleUpdateInput) (*exampleUser, error) {
	return &exampleUser{Username: in.Username}, nil
}

func deleteExampleUser(context.Context, examplePathInput) error { return nil }

func uploadExample(_ context.Context, in *exampleUpload) (exampleUser, error) {
	return exampleUser{Username: in.Username}, nil
}

func loginExample(context.Context) (Reply[*exampleUser], error) {
	return Reply[*exampleUser]{
		Body:   &exampleUser{Username: "demo"},
		Status: http.StatusCreated,
		Cookies: []*http.Cookie{{
			Name: "session", Value: "example", HttpOnly: true,
		}},
	}, nil
}

func requireExampleAuth(next root.Handler) root.Handler  { return next }
func requireExampleAdmin(next root.Handler) root.Handler { return next }
