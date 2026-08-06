package webbench

// Pure route-matching benchmarks, following the methodology of
// julienschmidt/go-http-routing-benchmark: no-op handlers, a reusable
// discarding writer, requests built outside the loop. Nothing is bound,
// validated, or serialized — the numbers isolate route lookup, parameter
// capture, and dispatch.
//
// Scenario groups:
//   - micro table (4 routes): static / 1 param / 5 params / wildcard / miss
//   - githubAPI table (203 routes): the classic GithubStatic, GithubParam,
//     and GithubAll (one op = one sweep over all 203 requests)
//
// The typed layers of ghttp/huma/fuego/go-restful are NOT exercised here for
// param binding; handlers are as close to no-ops as each framework allows.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	restful "github.com/emicklei/go-restful/v3"
	"github.com/gin-gonic/gin"
	"github.com/go-chi/chi/v5"
	"github.com/go-fuego/fuego"
	"github.com/gofiber/fiber/v3"
	"github.com/gorilla/mux"
	"github.com/julienschmidt/httprouter"
	"github.com/labstack/echo/v4"
	"github.com/sofiworker/gk/ghttp"
	"github.com/uptrace/bunrouter"
	"github.com/valyala/fasthttp"
)

// microAPI exercises the fundamental matching shapes in isolation.
var microAPI = []apiRoute{
	{"GET", "/ping"},
	{"GET", "/users/:id"},
	{"GET", "/orgs/:org/teams/:team/members/:member/roles/:role/perms/:perm"},
	{"GET", "/files/*filepath"},
}

type reqSpec struct{ method, path string }

var (
	specStatic   = []reqSpec{{"GET", "/ping"}}
	specParam1   = []reqSpec{{"GET", "/users/12345"}}
	specParam5   = []reqSpec{{"GET", "/orgs/o/teams/t/members/m/roles/r/perms/p"}}
	specWildcard = []reqSpec{{"GET", "/files/assets/css/site.css"}}
	specMiss     = []reqSpec{{"GET", "/definitely/not/registered"}}

	specGithubStatic = []reqSpec{{"GET", "/user/repos"}}
	specGithubParam  = []reqSpec{{"GET", "/repos/julienschmidt/httprouter/stargazers"}}
)

// specGithubAll requests every githubAPI route once, using the pattern path
// itself as the request path (":owner" et al. become literal segment values),
// exactly like the original benchmark.
func specGithubAll() []reqSpec {
	specs := make([]reqSpec, len(githubAPI))
	for i, r := range githubAPI {
		specs[i] = reqSpec{method: r.method, path: r.path}
	}
	return specs
}

// ---------------------------------------------------------------------------
// Path syntax translation from the canonical ":param" / "*name" form
// ---------------------------------------------------------------------------

func translatePath(p string, param func(name string) string, wildcard func(name string) string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		switch {
		case strings.HasPrefix(s, ":"):
			segs[i] = param(s[1:])
		case strings.HasPrefix(s, "*"):
			segs[i] = wildcard(s[1:])
		}
	}
	return strings.Join(segs, "/")
}

func curlyPath(p string) string { // stdmux, ghttp, huma, fuego
	return translatePath(p,
		func(n string) string { return "{" + n + "}" },
		func(n string) string { return "{" + n + "...}" })
}

func chiPath(p string) string {
	return translatePath(p,
		func(n string) string { return "{" + n + "}" },
		func(string) string { return "*" })
}

func gorillaPath(p string) string {
	return translatePath(p,
		func(n string) string { return "{" + n + "}" },
		func(n string) string { return "{" + n + ":.*}" })
}

func restfulPath(p string) string {
	return translatePath(p,
		func(n string) string { return "{" + n + "}" },
		func(n string) string { return "{" + n + ":*}" })
}

func starPath(p string) string { // echo, fiber
	return translatePath(p,
		func(n string) string { return ":" + n },
		func(string) string { return "*" })
}

// gin/hertz/httprouter/bunrouter use the canonical syntax unchanged.

// ---------------------------------------------------------------------------
// Runners
// ---------------------------------------------------------------------------

type matchRunner interface {
	run(b *testing.B, specs []reqSpec)
	probe(spec reqSpec) int
}

type httpMatchRunner struct{ h http.Handler }

func (r httpMatchRunner) run(b *testing.B, specs []reqSpec) {
	reqs := make([]*http.Request, len(specs))
	for i, s := range specs {
		reqs[i] = httptest.NewRequest(s.method, s.path, nil)
	}
	w := newMockWriter()
	b.ReportAllocs()
	b.ResetTimer()
	if len(reqs) == 1 {
		req := reqs[0]
		for b.Loop() {
			r.h.ServeHTTP(w, req)
		}
		return
	}
	for b.Loop() { // one op = one sweep over all requests (GithubAll style)
		for _, req := range reqs {
			r.h.ServeHTTP(w, req)
		}
	}
}

func (r httpMatchRunner) probe(spec reqSpec) int {
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, httptest.NewRequest(spec.method, spec.path, nil))
	return rec.Code
}

type fiberMatchRunner struct {
	app *fiber.App
	h   fasthttp.RequestHandler
}

func (r *fiberMatchRunner) run(b *testing.B, specs []reqSpec) {
	ctxs := make([]*fasthttp.RequestCtx, len(specs))
	for i, s := range specs {
		var req fasthttp.Request
		req.Header.SetMethod(s.method)
		req.SetRequestURI(s.path)
		ctx := &fasthttp.RequestCtx{}
		ctx.Init(&req, nil, nil)
		ctxs[i] = ctx
	}
	b.ReportAllocs()
	b.ResetTimer()
	if len(ctxs) == 1 {
		ctx := ctxs[0]
		for b.Loop() {
			ctx.Response.Reset()
			r.h(ctx)
		}
		return
	}
	for b.Loop() {
		for _, ctx := range ctxs {
			ctx.Response.Reset()
			r.h(ctx)
		}
	}
}

func (r *fiberMatchRunner) probe(spec reqSpec) int {
	resp, err := r.app.Test(httptest.NewRequest(spec.method, spec.path, nil))
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

type hertzMatchRunner struct{ engine *route.Engine }

func (r *hertzMatchRunner) run(b *testing.B, specs []reqSpec) {
	b.ReportAllocs()
	b.ResetTimer()
	if len(specs) == 1 {
		s := specs[0]
		for b.Loop() {
			ut.PerformRequest(r.engine, s.method, s.path, nil)
		}
		return
	}
	for b.Loop() {
		for _, s := range specs {
			ut.PerformRequest(r.engine, s.method, s.path, nil)
		}
	}
}

func (r *hertzMatchRunner) probe(spec reqSpec) int {
	return ut.PerformRequest(r.engine, spec.method, spec.path, nil).Result().StatusCode()
}

// ---------------------------------------------------------------------------
// Per-framework router builders (no-op handlers)
// ---------------------------------------------------------------------------

var noopHTTP = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

func buildGhttpMatch(routes []apiRoute) matchRunner {
	s := ghttp.New(ghttp.WithProduces(ghttp.MIMEJSON))
	handler := func(context.Context, ghttp.Params) (struct{}, error) {
		return struct{}{}, nil
	}
	for _, rt := range routes {
		path := curlyPath(rt.path)
		switch rt.method {
		case http.MethodGet:
			ghttp.Route[ghttp.Params, struct{}](s).GET(path).To(handler)
		case http.MethodPost:
			ghttp.Route[ghttp.Params, struct{}](s).POST(path).To(handler)
		case http.MethodPut:
			ghttp.Route[ghttp.Params, struct{}](s).PUT(path).To(handler)
		case http.MethodDelete:
			ghttp.Route[ghttp.Params, struct{}](s).DELETE(path).To(handler)
		case http.MethodPatch:
			ghttp.Route[ghttp.Params, struct{}](s).PATCH(path).To(handler)
		default:
			panic(fmt.Sprintf("ghttp: unsupported benchmark method %s", rt.method))
		}
	}
	return httpMatchRunner{h: s}
}

func buildStdMuxMatch(routes []apiRoute) matchRunner {
	m := http.NewServeMux()
	for _, rt := range routes {
		m.Handle(rt.method+" "+curlyPath(rt.path), noopHTTP)
	}
	return httpMatchRunner{h: m}
}

func buildGinMatch(routes []apiRoute) matchRunner {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	h := func(*gin.Context) {}
	for _, rt := range routes {
		r.Handle(rt.method, rt.path, h)
	}
	return httpMatchRunner{h: r}
}

func buildEchoMatch(routes []apiRoute) matchRunner {
	e := echo.New()
	h := func(echo.Context) error { return nil }
	for _, rt := range routes {
		e.Add(rt.method, starPath(rt.path), h)
	}
	return httpMatchRunner{h: e}
}

func buildChiMatch(routes []apiRoute) matchRunner {
	r := chi.NewRouter()
	for _, rt := range routes {
		r.Method(rt.method, chiPath(rt.path), noopHTTP)
	}
	return httpMatchRunner{h: r}
}

func buildGorillaMatch(routes []apiRoute) matchRunner {
	r := mux.NewRouter()
	for _, rt := range routes {
		r.Handle(gorillaPath(rt.path), noopHTTP).Methods(rt.method)
	}
	return httpMatchRunner{h: r}
}

func buildHTTPRouterMatch(routes []apiRoute) matchRunner {
	r := httprouter.New()
	h := func(http.ResponseWriter, *http.Request, httprouter.Params) {}
	for _, rt := range routes {
		r.Handle(rt.method, rt.path, h)
	}
	return httpMatchRunner{h: r}
}

func buildBunRouterMatch(routes []apiRoute) matchRunner {
	r := bunrouter.New()
	h := func(http.ResponseWriter, bunrouter.Request) error { return nil }
	for _, rt := range routes {
		r.Handle(rt.method, rt.path, h)
	}
	return httpMatchRunner{h: r}
}

func buildGoRestfulMatch(routes []apiRoute) matchRunner {
	c := restful.NewContainer()
	ws := new(restful.WebService)
	h := func(*restful.Request, *restful.Response) {}
	for _, rt := range routes {
		builder := ws.Method(rt.method).Path(restfulPath(rt.path))
		ws.Route(builder.To(h))
	}
	c.Add(ws)
	return httpMatchRunner{h: c}
}

func buildHumaMatch(routes []apiRoute) matchRunner {
	m := http.NewServeMux()
	api := humago.New(m, huma.DefaultConfig("routing", "1.0.0"))
	for i, rt := range routes {
		huma.Register(api, huma.Operation{
			OperationID: fmt.Sprintf("r%d", i),
			Method:      rt.method,
			Path:        curlyPath(rt.path),
		}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			return nil, nil
		})
	}
	return httpMatchRunner{h: m}
}

func buildFuegoMatch(routes []apiRoute) matchRunner {
	s := fuego.NewServer(
		fuego.WithoutLogger(),
		fuego.WithoutStartupMessages(),
		fuego.WithLoggingMiddleware(fuego.LoggingConfig{
			DisableRequest:  true,
			DisableResponse: true,
		}),
	)
	h := func(fuego.ContextNoBody) (string, error) { return "", nil }
	for _, rt := range routes {
		path := curlyPath(rt.path)
		switch rt.method {
		case "GET":
			fuego.Get(s, path, h)
		case "POST":
			fuego.Post(s, path, h)
		case "PUT":
			fuego.Put(s, path, h)
		case "PATCH":
			fuego.Patch(s, path, h)
		case "DELETE":
			fuego.Delete(s, path, h)
		}
	}
	return httpMatchRunner{h: s.Mux}
}

func buildFiberMatch(routes []apiRoute) matchRunner {
	appl := fiber.New()
	h := func(fiber.Ctx) error { return nil }
	for _, rt := range routes {
		appl.Add([]string{rt.method}, starPath(rt.path), h)
	}
	return &fiberMatchRunner{app: appl, h: appl.Handler()}
}

func buildHertzMatch(routes []apiRoute) matchRunner {
	engine := route.NewEngine(hertzconfig.NewOptions([]hertzconfig.Option{}))
	h := func(context.Context, *app.RequestContext) {}
	for _, rt := range routes {
		engine.Handle(rt.method, rt.path, h)
	}
	return &hertzMatchRunner{engine: engine}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

type routingAdapter struct {
	name  string
	build func([]apiRoute) matchRunner
}

var routingAdapters = []routingAdapter{
	{"bunrouter", buildBunRouterMatch},
	{"chi", buildChiMatch},
	{"echo", buildEchoMatch},
	{"fiber", buildFiberMatch},
	{"fuego", buildFuegoMatch},
	{"ghttp", buildGhttpMatch},
	{"gin", buildGinMatch},
	{"go-restful", buildGoRestfulMatch},
	{"gorilla-mux", buildGorillaMatch},
	{"hertz", buildHertzMatch},
	{"httprouter", buildHTTPRouterMatch},
	{"huma", buildHumaMatch},
	{"stdmux", buildStdMuxMatch},
}

// Routers are built once per (adapter, table) and reused by every benchmark.
var (
	microRouters  = map[string]matchRunner{}
	githubRouters = map[string]matchRunner{}
	routersOnce   sync.Once
)

func routingRunners() (map[string]matchRunner, map[string]matchRunner) {
	routersOnce.Do(func() {
		for _, a := range routingAdapters {
			microRouters[a.name] = a.build(microAPI)
			githubRouters[a.name] = a.build(githubAPI)
		}
	})
	return microRouters, githubRouters
}

// ---------------------------------------------------------------------------
// Sanity + benchmarks
// ---------------------------------------------------------------------------

func TestRoutingSanity(t *testing.T) {
	micro, github := routingRunners()
	for _, a := range routingAdapters {
		t.Run(a.name, func(t *testing.T) {
			checks := []struct {
				runner  matchRunner
				spec    reqSpec
				matched bool
			}{
				{micro[a.name], specStatic[0], true},
				{micro[a.name], specParam1[0], true},
				{micro[a.name], specParam5[0], true},
				{micro[a.name], specWildcard[0], true},
				{micro[a.name], specMiss[0], false},
				{github[a.name], specGithubStatic[0], true},
				{github[a.name], specGithubParam[0], true},
			}
			for _, c := range checks {
				status := c.runner.probe(c.spec)
				if matched := status < 400; matched != c.matched {
					t.Errorf("%s %s: status %d, want matched=%v", c.spec.method, c.spec.path, status, c.matched)
				}
			}
		})
	}
}

func runRoutingBench(b *testing.B, table string, specs []reqSpec) {
	micro, github := routingRunners()
	runners := micro
	if table == "github" {
		runners = github
	}
	for _, a := range routingAdapters {
		b.Run(a.name, func(b *testing.B) {
			runners[a.name].run(b, specs)
		})
	}
}

// BenchmarkRoutingStatic measures matching a single static route.
func BenchmarkRoutingStatic(b *testing.B) { runRoutingBench(b, "micro", specStatic) }

// BenchmarkRoutingParam1 measures matching one path parameter.
func BenchmarkRoutingParam1(b *testing.B) { runRoutingBench(b, "micro", specParam1) }

// BenchmarkRoutingParam5 measures matching five path parameters.
func BenchmarkRoutingParam5(b *testing.B) { runRoutingBench(b, "micro", specParam5) }

// BenchmarkRoutingWildcard measures catch-all matching.
func BenchmarkRoutingWildcard(b *testing.B) { runRoutingBench(b, "micro", specWildcard) }

// BenchmarkRoutingMiss measures the not-found path.
func BenchmarkRoutingMiss(b *testing.B) { runRoutingBench(b, "micro", specMiss) }

// BenchmarkRoutingGithubStatic measures one static hit on the 203-route table.
func BenchmarkRoutingGithubStatic(b *testing.B) { runRoutingBench(b, "github", specGithubStatic) }

// BenchmarkRoutingGithubParam measures one 2-param hit on the 203-route table.
func BenchmarkRoutingGithubParam(b *testing.B) { runRoutingBench(b, "github", specGithubParam) }

// BenchmarkRoutingGithubAll sweeps all 203 GitHub API routes per op
// (ns/op therefore covers 203 matches, as in go-http-routing-benchmark).
func BenchmarkRoutingGithubAll(b *testing.B) { runRoutingBench(b, "github", specGithubAll()) }
