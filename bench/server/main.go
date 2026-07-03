// Package main -HTTP framework benchmark (ghttp vs gin/fiber/chi/echo/go-restful/default/fasthttp)
//
// Each framework registers GET /hello ->"hello world", respecting sleepTime/cpuBound globals.
// Usage: gowebbenchmark <framework> [sleep_ms] [port]
//
//	sleep_ms=0 (default): runtime.Gosched(),  sleep_ms>0: time.Sleep(), sleep_ms=-1: CPU-bound (pow)
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	// ghttp
	"github.com/sofiworker/gk/ghttp"

	"github.com/abemedia/go-don"
	_ "github.com/abemedia/go-don/encoding/text"
	"github.com/emicklei/go-restful"
	// Comparison frameworks
	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/gofiber/fiber/v2"
	"github.com/labstack/echo/v4"
	"github.com/valyala/fasthttp"
)

var (
	port              = 8080
	sleepTime         = 0
	cpuBound          bool
	target            = 15
	sleepTimeDuration time.Duration
	message           = []byte("hello world")
	messageStr        = "hello world"
)

func main() {
	//args := os.Args
	//if len(args) < 2 {
	//	fmt.Println("Usage: gowebbenchmark <framework> [sleep_ms] [port]")
	//	fmt.Println("Frameworks: default, gin, chi, echo, fiber, gorestful, fasthttp, don, ghttp")
	//	os.Exit(1)
	//}
	//
	//webFramework := args[1]
	//if len(args) > 2 {
	//	sleepTime, _ = strconv.Atoi(args[2])
	//	if sleepTime == -1 {
	//		cpuBound = true
	//		sleepTime = 0
	//	}
	//}
	//if len(args) > 3 {
	//	port, _ = strconv.Atoi(args[3])
	//}
	//sleepTimeDuration = time.Duration(sleepTime) * time.Millisecond

	webFramework := "ghttp"
	switch strings.ToLower(webFramework) {
	case "default":
		startDefault()
	case "gin":
		startGin()
	case "chi":
		startChi()
	case "echo":
		startEcho()
	case "fiber":
		startFiber()
	case "gorestful":
		startGoRestful()
	case "fasthttp":
		startFasthttp()
	case "don":
		startDon()
	case "ghttp":
		startGhttp()
	default:
		fmt.Printf("Unknown framework: %s\n", webFramework)
		os.Exit(1)
	}
}

// ---- Common handler body ----
func handleRequest() {
	if cpuBound {
		pow(target)
	} else {
		if sleepTime > 0 {
			time.Sleep(sleepTimeDuration)
		} else {
			runtime.Gosched()
		}
	}
}

// ---- default (net/http) ----
func helloHandler(w http.ResponseWriter, r *http.Request) {
	handleRequest()
	w.Write(message)
}

func startDefault() {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", helloHandler)
	http.ListenAndServe(":"+strconv.Itoa(port), mux)
}

// ---- gin ----
func startGin() {
	gin.SetMode(gin.ReleaseMode)
	mux := gin.New()
	mux.GET("/hello", func(c *gin.Context) {
		handleRequest()
		c.Writer.Write(message)
		c.String(200, "hello world")
	})
	mux.Run(":" + strconv.Itoa(port))
}

// ---- chi ----
func startChi() {
	r := chi.NewRouter()
	r.Get("/hello", helloHandler)
	http.ListenAndServe(":"+strconv.Itoa(port), r)
}

// ---- echo ----
func startEcho() {
	e := echo.New()
	e.GET("/hello", func(c echo.Context) error {
		handleRequest()
		c.Response().Write(message)
		return nil
	})
	e.Start(":" + strconv.Itoa(port))
}

// ---- fiber ----
func startFiber() {
	app := fiber.New(fiber.Config{
		DisableDefaultDate:        true,
		DisableHeaderNormalizing:  true,
		DisableDefaultContentType: true,
	})
	app.Get("/hello", func(c *fiber.Ctx) error {
		handleRequest()
		return c.SendString(messageStr)
	})
	log.Fatal(app.Listen(":" + strconv.Itoa(port)))
}

// ---- go-restful ----
func startGoRestful() {
	wsContainer := restful.NewContainer()
	ws := new(restful.WebService)
	ws.Route(ws.GET("/hello").To(func(r *restful.Request, w *restful.Response) {
		handleRequest()
		w.Write(message)
	}))
	wsContainer.Add(ws)
	http.ListenAndServe(":"+strconv.Itoa(port), wsContainer)
}

// ---- fasthttp ----
func startFasthttp() {
	s := &fasthttp.Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			handleRequest()
			ctx.Write(message)
		},
		GetOnly:                       true,
		NoDefaultDate:                 true,
		NoDefaultContentType:          true,
		DisableHeaderNamesNormalizing: true,
	}
	log.Fatal(s.ListenAndServe(":" + strconv.Itoa(port)))
}

// ---- don ----
func startDon() {
	api := don.New(nil)
	api.Get("/hello", don.H(func(context.Context, any) ([]byte, error) {
		handleRequest()
		return message, nil
	}))
	api.ListenAndServe(":" + strconv.Itoa(port))
}

// ---- ghttp ----
func ghttpHandler(w http.ResponseWriter, r *http.Request) {
	handleRequest()
	w.Write(message)
}

func startGhttp() {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEPlain))
	defer s.Shutdown(context.Background())

	ghttp.Route[struct{}, struct{}](s).
		GET("/hello").
		ToHTTP(http.HandlerFunc(ghttpHandler))

	group := s.Group("/test", func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r)
		})
	})
	group.Use(func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r)
		})
	})

	type uploadInput struct {
		Body struct {
			Name   string              `form:"name"`
			Age    int                 `form:"age"`
			Tags   []string            `form:"tag"`
			Avatar *ghttp.FileHeader   `form:"avatar"`
			Files  []*ghttp.FileHeader `form:"files"`
		}
	}

	ghttp.Route[uploadInput, struct{}](group).
		POST("/upload").
		ToHTTPFunc(func(w http.ResponseWriter, r *http.Request, in uploadInput) error {
			avatar := ""
			if in.Body.Avatar != nil {
				avatar = in.Body.Avatar.Filename
			}

			fileNames := make([]string, 0, len(in.Body.Files))
			for _, file := range in.Body.Files {
				fileNames = append(fileNames, file.Filename)
			}

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = fmt.Fprintf(w, "name=%s age=%d tags=%s avatar=%s files=%s",
				in.Body.Name,
				in.Body.Age,
				strings.Join(in.Body.Tags, ","),
				avatar,
				strings.Join(fileNames, ","),
			)
			return nil
		})

	ghttp.Route[struct{}, struct{}](group).
		POST("/form").
		ToHTTPFunc(func(w http.ResponseWriter, r *http.Request, in struct{}) error {
			if err := r.ParseForm(); err != nil {
				return err
			}

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = fmt.Fprintf(w, "name=%s age=%s tags=%s",
				r.FormValue("name"),
				r.FormValue("age"),
				strings.Join(r.Form["tag"], ","),
			)
			return nil
		})

	ghttp.Route[struct{}, string](s).GET("/ping").To(func(ctx context.Context, input struct{}) (string, error) {
		return "pong", nil
	})

	http.ListenAndServe(":"+strconv.Itoa(port), s)
}
