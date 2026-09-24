package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	v2 "github.com/sofiworker/gk/ghttp/v2"
)

type user struct {
	ID   int    `json:"id" xml:"id"`
	Name string `json:"name" xml:"name"`
}
type userPath struct {
	ID int `path:"id"`
}
type updateInput struct {
	ID   int `path:"id"`
	Body struct {
		Name string `json:"name"`
	} `body:"json"`
}
type formInput struct {
	Name string   `form:"name"`
	Tags []string `form:"tag"`
}
type uploadInput struct {
	Title string                `form:"title"`
	File  *multipart.FileHeader `form:"file"`
}
type uploadResult struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

func auth(next v2.Handler) v2.Handler {
	return func(ctx context.Context, r *v2.Request, w *v2.Response) error {
		// 固定令牌仅用于演示组中间件；实际系统应接入自己的认证服务。
		// A fixed token only demonstrates group middleware; real applications should use their authentication service.
		if r.Header.Get("Authorization") != "Bearer demo" {
			return v2.HTTPError{Status: 401, Cause: errors.New("invalid demo token")}
		}
		return next(ctx, r, w)
	}
}
func readUser(_ context.Context, in userPath) (user, error) {
	return user{ID: in.ID, Name: "demo"}, nil
}
func updateUser(_ context.Context, in *updateInput) (*user, error) {
	return &user{ID: in.ID, Name: in.Body.Name}, nil
}
func validateUpdate(_ context.Context, in *updateInput) error {
	if strings.TrimSpace(in.Body.Name) == "" {
		return errors.New("name is required")
	}
	return nil
}
func inspect(_ context.Context, in v2.RequestInput) (map[string]string, error) {
	src := in.Sources()
	q, _ := src.QueryFirst("q")
	return map[string]string{"q": q, "trace": src.Header("X-Trace"), "session": src.Cookie("session")}, nil
}
func upload(_ context.Context, in *uploadInput) (uploadResult, error) {
	if in.File == nil {
		return uploadResult{}, v2.HTTPError{Status: 400, Cause: errors.New("file is required")}
	}
	f, err := in.File.Open()
	if err != nil {
		return uploadResult{}, err
	}
	defer f.Close()
	n, err := io.Copy(io.Discard, f)
	return uploadResult{Name: in.File.Filename, Bytes: n}, err
}
func streamUpload(_ context.Context, reader *multipart.Reader) ([]uploadResult, error) {
	result := []uploadResult{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				return nil, err
			}
			return nil, v2.HTTPError{Status: 400, Cause: err}
		}
		n, err := io.Copy(io.Discard, part)
		closeErr := part.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		result = append(result, uploadResult{Name: part.FormName(), Bytes: n})
	}
}
func download(_ context.Context, in *v2.RequestInput) (v2.FileReply, error) {
	return v2.FileReply{Request: in.Request.Request, Name: "hello.txt", DownloadName: "hello.txt", Content: strings.NewReader("hello from v2\n"), ModTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
}
func decodePath(_ context.Context, r *v2.Request) (userPath, error) {
	id, err := strconv.Atoi(r.Params.Get("id"))
	return userPath{ID: id}, err
}
func newServer() (*v2.Server, error) {
	s := v2.NewServer(v2.WithReadHeaderTimeout(5*time.Second), v2.WithIdleTimeout(time.Minute)).Use(v2.Recovery())
	if err := s.Register(
		v2.FromFunc("GET", "/health", func(context.Context) (string, error) { return "ok", nil }),
		v2.FromFunc("GET", "/", func(context.Context) (v2.RedirectReply, error) { return v2.RedirectReply{Location: "/health"}, nil }),
	); err != nil {
		return nil, err
	}
	api := s.Group("/api").Use(auth).With(v2.WithBodyLimit(2 << 20))
	if err := api.Register(
		v2.Get("/users/{id}", readUser, v2.WithNegotiation(v2.JSONOutput[user](), v2.XMLOutput[user]())),
		v2.Patch("/users/{id}", updateUser, v2.WithValidator(validateUpdate)),
		v2.Post("/users", func(_ context.Context, in *user) (v2.Reply[*user], error) {
			return v2.Reply[*user]{Body: in, Status: 201, Headers: http.Header{"X-Example": []string{"created"}}}, nil
		}, v2.WithInput(v2.StrictJSONInput[*user]())),
		v2.FromAction("DELETE", "/users/{id}", func(context.Context, userPath) error { return nil }),
		v2.Get("/inspect", inspect),
		v2.Raw(http.MethodPost, "/raw", func(_ context.Context, req *v2.Request, resp *v2.Response) error {
			data, err := io.ReadAll(req.Body)
			if err != nil {
				return err
			}
			resp.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, err = resp.Write(data)
			return err
		}),
		v2.Get("/decoded/{id}", readUser, v2.WithInput(v2.DecodeWith(decodePath))),
		v2.Post("/form", func(_ context.Context, in formInput) (formInput, error) { return in, nil }, v2.WithInput(v2.FormInput[formInput]())),
		v2.Post("/upload", upload, v2.WithInput(v2.MultipartInput[*uploadInput](v2.MultipartLimits{MaxBytes: 2 << 20, MemoryBytes: 64 << 10}))),
		v2.Post("/upload-stream", streamUpload, v2.WithInput(v2.MultipartStreamInput())),
		v2.Get("/download", download),
		v2.Get("/stream", func(_ context.Context, in v2.RequestInput) (v2.StreamReply, error) {
			return v2.StreamReply{Request: in.Request.Request, Reader: strings.NewReader("first\nsecond\n"), ContentType: "text/plain; charset=utf-8"}, nil
		}),
	); err != nil {
		return nil, err
	}
	// 文档在注册后生成一次，避免每个请求重新反射 schema。
	// Generate the document once after registration rather than reflecting schemas per request.
	spec, err := s.OpenAPI("v2 HTTP example", "1.0.0")
	if err != nil {
		return nil, err
	}
	if err = s.Register(v2.FromFunc("GET", "/openapi.json", func(context.Context) (v2.StreamReply, error) {
		return v2.StreamReply{Reader: strings.NewReader(string(spec)), ContentType: "application/json"}, nil
	})); err != nil {
		return nil, err
	}
	return s, nil
}
func run(addr string) error {
	s, err := newServer()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- s.Run(addr) }()
	log.Printf("HTTP example listening on %s", addr)
	select {
	case err := <-done:
		if errors.Is(err, v2.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdown); err != nil {
			_ = s.Close()
			return err
		}
		err := <-done
		if errors.Is(err, v2.ErrServerClosed) {
			return nil
		}
		return err
	}
}
func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	flag.Parse()
	if err := run(*addr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
