// Package main — HTTP framework benchmark (ghttp vs gin/fiber/chi/echo/go-restful/default/fasthttp)
//
// Each framework registers GET /hello → "hello world", respecting sleepTime/cpuBound globals.
// Usage: gowebbenchmark <framework> [sleep_ms] [port]
//   sleep_ms=0 (default): runtime.Gosched(),  sleep_ms>0: time.Sleep(), sleep_ms=-1: CPU-bound (pow)
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

	// Comparison frameworks
	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/labstack/echo/v4"
	"github.com/emicklei/go-restful"
	"github.com/valyala/fasthttp"
	"github.com/gofiber/fiber/v2"
	"github.com/abemedia/go-don"
	_ "github.com/abemedia/go-don/encoding/text"
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
	args := os.Args
	if len(args) < 2 {
		fmt.Println("Usage: gowebbenchmark <framework> [sleep_ms] [port]")
		fmt.Println("Frameworks: default, gin, chi, echo, fiber, gorestful, fasthttp, don, ghttp")
		os.Exit(1)
	}

	webFramework := args[1]
	if len(args) > 2 {
		sleepTime, _ = strconv.Atoi(args[2])
		if sleepTime == -1 {
			cpuBound = true
			sleepTime = 0
		}
	}
	if len(args) > 3 {
		port, _ = strconv.Atoi(args[3])
	}
	sleepTimeDuration = time.Duration(sleepTime) * time.Millisecond

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
	s := ghttp.New("gk", "1.0.0")
	s.Router().Register(http.MethodGet, "/hello", http.HandlerFunc(ghttpHandler))
	http.ListenAndServe(":"+strconv.Itoa(port), s)
}
