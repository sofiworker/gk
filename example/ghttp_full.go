package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

type userInput struct {
	Path struct {
		ID string `path:"id"`
	}
	Query struct {
		Role string `query:"role"`
	}
	Body struct {
		Name string `json:"name"`
	}
}

type userOutput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type healthOutput struct {
	OK bool `json:"ok"`
}

type customOutput struct {
	Method string `json:"method"`
}

type methodOutput struct {
	Verb string `json:"verb"`
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
}

type scenarioValidator struct{}

func (scenarioValidator) Validate(context.Context, interface{}) error {
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	staticDir := mustCreateStaticDir()
	defer os.RemoveAll(staticDir)
	templateDir := mustCreateTemplateDir()
	defer os.RemoveAll(templateDir)

	app := ghttp.New(
		ghttp.WithOpenAPI("ghttp full example", "1.0.0"),
		ghttp.WithValidator(scenarioValidator{}),
		ghttp.WithAddress(":8080"),
		ghttp.WithRenderer(ghttp.NewRenderer(templateDir, ".html", nil, false)),
		ghttp.WithProduces(ghttp.MIMEJSON),
	)

	app.Use(ghttp.RequestID())
	app.Use(ghttp.CORS(ghttp.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodPatch,
			http.MethodHead,
			http.MethodOptions,
			http.MethodTrace,
			"PROPFIND",
		},
		AllowHeaders: []string{"Content-Type", "X-Request-ID", "Accept"},
	}))
	app.Use(ghttp.RequestLogger())
	app.Use(ghttp.Timeout(2 * time.Second))
	app.Use(ghttp.Recoverer())

	api := app.Group("/api").Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Example-Group", "api")
			next.ServeHTTP(w, r)
		})
	})

	mustRoute(ghttp.Route[userInput, userOutput](api).
		POST("/users/{id}").
		Doc("Create or update a user").
		Tags("users").
		OperationID("createUser").
		Reads(userInput{}).
		Responds(http.StatusCreated).With(userOutput{}).Desc("created").End().
		To(func(ctx context.Context, req userInput) (userOutput, error) {
			return userOutput{
				ID:   req.Path.ID,
				Name: req.Body.Name,
				Role: req.Query.Role,
			}, nil
		}))

	mustRoute(ghttp.Route[struct{}, healthOutput](app).
		ANY("/health").
		Doc("health check").
		Responds(http.StatusOK).With(healthOutput{}).Desc("healthy").End().
		To(func(ctx context.Context, req struct{}) (healthOutput, error) {
			return healthOutput{OK: true}, nil
		}))

	mustRoute(ghttp.Route[struct{}, customOutput](app).
		CUSTOM("PROPFIND", "/custom").
		Doc("custom method").
		Responds(http.StatusOK).With(customOutput{}).Desc("custom").End().
		To(func(ctx context.Context, req struct{}) (customOutput, error) {
			return customOutput{Method: "PROPFIND"}, nil
		}))

	registerMethodRoutes(app)

	mustRoute(ghttp.Route[struct{}, struct{}](app).
		GET("/assets").
		ToStatic(staticDir))
	mustRoute(ghttp.Route[struct{}, struct{}](app).
		GET("/pages/home").
		Doc("render template").
		Responds(http.StatusOK).With(struct{}{}).End().
		ToHTML(http.StatusOK, "home", map[string]interface{}{"Title": "ghttp full example"}))
	mustRoute(ghttp.Route[struct{}, struct{}](app).
		GET("/events").
		ToSSE(func(ctx ghttp.Context, stream *ghttp.SSEWriter) error {
			if err := stream.WriteEvent("ready", "ok"); err != nil {
				return err
			}
			return nil
		}))

	serveAndDemo(app)
}

func registerMethodRoutes(app *ghttp.Server) {
	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		GET("/methods/get").
		Doc("GET example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "GET", Path: "/methods/get"}, nil
		}))

	mustRoute(ghttp.Route[userInput, methodOutput](app).
		GET("/methods/get-body").
		Doc("GET with body").
		Reads(userInput{}).
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req userInput) (methodOutput, error) {
			return methodOutput{Verb: "GET", Path: "/methods/get-body", Name: req.Body.Name, Role: req.Query.Role}, nil
		}))

	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		HEAD("/methods/head").
		Doc("HEAD example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "HEAD", Path: "/methods/head"}, nil
		}))

	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		POST("/methods/post").
		Doc("POST example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "POST", Path: "/methods/post"}, nil
		}))

	mustRoute(ghttp.Route[userInput, methodOutput](app).
		POST("/methods/post-empty").
		Doc("POST without body").
		Reads(userInput{}).
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req userInput) (methodOutput, error) {
			return methodOutput{Verb: "POST", Path: "/methods/post-empty", Name: req.Body.Name}, nil
		}))

	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		PUT("/methods/put/{id}").
		Doc("PUT example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "PUT", Path: "/methods/put/{id}"}, nil
		}))

	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		PATCH("/methods/patch/{id}").
		Doc("PATCH example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "PATCH", Path: "/methods/patch/{id}"}, nil
		}))

	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		DELETE("/methods/delete/{id}").
		Doc("DELETE example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "DELETE", Path: "/methods/delete/{id}"}, nil
		}))

	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		OPTIONS("/methods/options").
		Doc("OPTIONS example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "OPTIONS", Path: "/methods/options"}, nil
		}))

	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		TRACE("/methods/trace").
		Doc("TRACE example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "TRACE", Path: "/methods/trace"}, nil
		}))

	mustRoute(ghttp.Route[struct{}, methodOutput](app).
		CONNECT("/methods/connect").
		Doc("CONNECT example").
		Responds(http.StatusOK).With(methodOutput{}).End().
		To(func(ctx context.Context, req struct{}) (methodOutput, error) {
			return methodOutput{Verb: "CONNECT", Path: "/methods/connect"}, nil
		}))
}

func serveAndDemo(app *ghttp.Server) {
	ts := httptest.NewServer(app)
	fmt.Printf("ghttp full example is running at %s\n", ts.URL)

	postResp := mustHTTPPost(ts.URL+"/api/users/42?role=admin", `{"name":"alice"}`, "application/json")
	defer postResp.Body.Close()
	printResponse("POST /api/users/42", postResp)

	anyResp := mustHTTPRequest(http.MethodGet, ts.URL+"/health", nil)
	defer anyResp.Body.Close()
	printResponse("GET /health", anyResp)

	customReq, _ := http.NewRequest("PROPFIND", ts.URL+"/custom", nil)
	customResp := mustHTTPDo(customReq)
	defer customResp.Body.Close()
	printResponse("PROPFIND /custom", customResp)

	getBodyReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/methods/get-body?role=reader", strings.NewReader(`{"name":"get-body"}`))
	getBodyReq.Header.Set("Content-Type", "application/json")
	getBodyResp := mustHTTPDo(getBodyReq)
	defer getBodyResp.Body.Close()
	printResponse("GET /methods/get-body (with body)", getBodyResp)

	postEmptyReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/methods/post-empty", nil)
	postEmptyResp := mustHTTPDo(postEmptyReq)
	defer postEmptyResp.Body.Close()
	printResponse("POST /methods/post-empty (no body)", postEmptyResp)

	putReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/methods/put/42", nil)
	putResp := mustHTTPDo(putReq)
	defer putResp.Body.Close()
	printResponse("PUT /methods/put/42", putResp)

	patchReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/methods/patch/99", nil)
	patchResp := mustHTTPDo(patchReq)
	defer patchResp.Body.Close()
	printResponse("PATCH /methods/patch/99", patchResp)

	deleteReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/methods/delete/77", nil)
	deleteResp := mustHTTPDo(deleteReq)
	defer deleteResp.Body.Close()
	printResponse("DELETE /methods/delete/77", deleteResp)

	headResp := mustHTTPRequest(http.MethodHead, ts.URL+"/methods/head", nil)
	defer headResp.Body.Close()
	printResponse("HEAD /methods/head", headResp)

	optionsReq, _ := http.NewRequest(http.MethodOptions, ts.URL+"/methods/options", nil)
	optionsResp := mustHTTPDo(optionsReq)
	defer optionsResp.Body.Close()
	printResponse("OPTIONS /methods/options", optionsResp)

	traceReq, _ := http.NewRequest(http.MethodTrace, ts.URL+"/methods/trace", nil)
	traceResp := mustHTTPDo(traceReq)
	defer traceResp.Body.Close()
	printResponse("TRACE /methods/trace", traceResp)

	connectReq, _ := http.NewRequest(http.MethodConnect, ts.URL+"/methods/connect", nil)
	connectResp := mustHTTPDo(connectReq)
	defer connectResp.Body.Close()
	printResponse("CONNECT /methods/connect", connectResp)

	staticResp := mustHTTPRequest(http.MethodGet, ts.URL+"/assets/hello.txt", nil)
	defer staticResp.Body.Close()
	printResponse("GET /assets/hello.txt", staticResp)

	templateReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/pages/home", nil)
	templateResp := mustHTTPDo(templateReq)
	defer templateResp.Body.Close()
	printResponse("GET /pages/home", templateResp)

	sseResp := mustHTTPRequest(http.MethodGet, ts.URL+"/events", nil)
	defer sseResp.Body.Close()
	sseBody, _ := io.ReadAll(sseResp.Body)
	fmt.Printf("SSE /events:\n%s\n", string(sseBody))

	openAPIResp := mustHTTPRequest(http.MethodGet, ts.URL+"/openapi.json", nil)
	defer openAPIResp.Body.Close()
	printResponse("GET /openapi.json", openAPIResp)

	fmt.Printf("server is still running at %s; press Ctrl+C to stop\n", ts.URL)
	select {}
}

func mustCreateStaticDir() string {
	dir, err := os.MkdirTemp("", "ghttp-example-static-*")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello from ghttp full example\n"), 0o600); err != nil {
		panic(err)
	}
	return dir
}

func mustCreateTemplateDir() string {
	dir, err := os.MkdirTemp("", "ghttp-example-template-*")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "home.html"), []byte("<html><body><h1>{{.Title}}</h1></body></html>"), 0o600); err != nil {
		panic(err)
	}
	return dir
}

func mustRoute(err error) {
	if err != nil {
		panic(err)
	}
}

func mustHTTPRequest(method, url string, body io.Reader) *http.Response {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		panic(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	return resp
}

func mustHTTPPost(url, body, contentType string) *http.Response {
	resp, err := http.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	return resp
}

func mustHTTPDo(req *http.Request) *http.Response {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	return resp
}

func printResponse(label string, resp *http.Response) {
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("%s\n", label)
	fmt.Printf("  status: %s\n", resp.Status)
	fmt.Printf("  headers: X-Request-ID=%s X-Example-Group=%s Content-Type=%s\n",
		resp.Header.Get("X-Request-ID"),
		resp.Header.Get("X-Example-Group"),
		resp.Header.Get("Content-Type"),
	)
	fmt.Printf("  body: %s\n", strings.TrimSpace(string(body)))
}
