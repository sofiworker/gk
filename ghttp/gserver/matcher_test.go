package gserver

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/exp/rand"
)

// dummy handlers for test
func hA(ctx *Context) {}
func hB(ctx *Context) {}
func hC(ctx *Context) {}

func TestServerMatcher_StaticRoute(t *testing.T) {
	m := newServerMatcher()

	if err := m.AddRoute("GET", "/health", hA); err != nil {
		t.Fatalf("AddRoute /health failed: %v", err)
	}

	res := m.Match("GET", "/health")
	if res == nil {
		t.Fatalf("expected match for /health")
	}
	if res.Path != "/health" {
		t.Errorf("expected Path=/health got %s", res.Path)
	}
	if len(res.Handlers) != 1 || reflect.ValueOf(res.Handlers[0]).Pointer() != reflect.ValueOf(hA).Pointer() {
		t.Errorf("unexpected handlers: %+v", res.Handlers)
	}
	if len(res.Params) != 0 {
		t.Errorf("expected no params, got %+v", res.Params)
	}
	if res.QueryValues != nil && len(res.QueryValues) != 0 {
		t.Errorf("expected no query values, got %+v", res.QueryValues)
	}

	// should not match different method
	if got := m.Match("POST", "/health"); got != nil {
		t.Errorf("expected nil match for POST /health, got %#v", got)
	}
}

func TestServerMatcher_ParamRoute(t *testing.T) {
	m := newServerMatcher()

	if err := m.AddRoute("GET", "/users/:id", hA, hB); err != nil {
		t.Fatalf("AddRoute /users/:id failed: %v", err)
	}

	res := m.Match("GET", "/users/42")
	if res == nil {
		t.Fatalf("expected match for /users/42")
	}
	if res.Path != "/users/:id" {
		t.Errorf("expected pattern path, got %s", res.Path)
	}
	if len(res.Handlers) != 2 {
		t.Errorf("expected 2 handlers, got %d", len(res.Handlers))
	}
	val, ok := res.GetParam("id")
	if !ok || val != "42" {
		t.Errorf("expected param id=42, got (%v,%v)", val, ok)
	}
}

func TestServerMatcher_WildcardRoute(t *testing.T) {
	m := newServerMatcher()

	if err := m.AddRoute("GET", "/files/*path", hC); err != nil {
		t.Fatalf("AddRoute /files/*path failed: %v", err)
	}

	// suppose CompressedRadixTree.search() will match "/files/*path"
	// and fill param "path" -> "img/logo.png"
	// For test here (since tree.search is stubbed returning nil),
	// you'd implement that logic in your real tree. We'll just assert no panic.
	_ = m.Match("GET", "/files/img/logo.png")
}

func TestServerMatcher_QueryParams(t *testing.T) {
	m := newServerMatcher()

	if err := m.AddRoute("GET", "/search", hA); err != nil {
		t.Fatalf("AddRoute /search failed: %v", err)
	}

	// normal query + repeated key + +space + %2F decode
	res := m.Match("GET", "/search?q=hello+world&q=bye&cat=images%2Fpng")
	if res == nil {
		t.Fatalf("expected match for /search with query")
	}

	if len(res.Params) == 0 {
		t.Fatalf("expected Params merged from query")
	}

	gotQ, okQ := res.GetParam("q")
	if !okQ || gotQ != "bye" {
		t.Errorf("expected q=bye (last wins), got %q, ok=%v", gotQ, okQ)
	}

	gotCat, okCat := res.GetParam("cat")
	if !okCat || gotCat != "images/png" {
		t.Errorf("expected cat=images/png got %q ok=%v", gotCat, okCat)
	}

	if res.QueryValues == nil || len(res.QueryValues["q"]) != 2 {
		t.Errorf("expected QueryValues to keep all q values, got %#v", res.QueryValues)
	}
}

func TestServerMatcher_QueryEmptyAndFragment(t *testing.T) {
	m := newServerMatcher()
	if err := m.AddRoute("GET", "/x", hA); err != nil {
		t.Fatal(err)
	}

	// path with "?" but empty query
	res := m.Match("GET", "/x?")
	if res == nil {
		t.Fatalf("expected match for /x?")
	}
	if _, ok := res.GetParam("nonexist"); ok {
		t.Errorf("nonexistent param should not be found")
	}

	// path with fragment "#frag", must still match "/x"
	res2 := m.Match("GET", "/x#frag")
	if res2 == nil {
		t.Fatalf("expected match for /x#frag")
	}
}

func TestServerMatcher_BadEncodingFallback(t *testing.T) {
	m := newServerMatcher()
	if err := m.AddRoute("GET", "/foo", hA); err != nil {
		t.Fatal(err)
	}

	// "%ZZ" should trigger fallback to url.ParseQuery,
	// we mainly assert it doesn't panic and still returns match.
	res := m.Match("GET", "/foo?a=%ZZ&b=ok")
	if res == nil {
		t.Fatalf("expected match for /foo with bad-encoded query")
	}
	// We don't assert exact param values here because behavior under fallback
	// is delegated to url.ParseQuery. We just verify no panic and non-nil.
}

func TestServerMatcher_RootAndEmpty(t *testing.T) {
	m := newServerMatcher()

	if err := m.AddRoute("GET", "/", hA); err != nil {
		t.Fatalf("AddRoute / failed: %v", err)
	}

	if got := m.Match("GET", "/"); got == nil {
		t.Errorf("expected match for /")
	}

	if got := m.Match("GET", ""); got != nil {
		t.Errorf("empty path should not match / (unless you want to allow that)")
	}
}

func TestDuplicateRoute(t *testing.T) {
	m := newServerMatcher()

	if err := m.AddRoute("GET", "/dupe", hA); err != nil {
		t.Fatalf("add first /dupe failed: %v", err)
	}
	if err := m.AddRoute("GET", "/dupe", hB); err == nil {
		t.Fatalf("expected duplicate route error")
	}
}

func TestListRoutesAndStats(t *testing.T) {
	m := newServerMatcher()
	_ = m.AddRoute("GET", "/a", hA)
	_ = m.AddRoute("GET", "/b/:id", hB)
	_ = m.AddRoute("POST", "/c", hC)

	routes := m.ListRoutes()
	if len(routes) < 3 {
		t.Errorf("expected >=3 routes, got %d: %#v", len(routes), routes)
	}

	// trigger some matches
	_ = m.Match("GET", "/a")
	_ = m.Match("GET", "/b/123")
	_ = m.Match("GET", "/nope")

	stats := m.Stats()
	if stats.TotalRequests == 0 {
		t.Errorf("expected some requests counted")
	}
	if (stats.MatchHits + stats.MatchMisses) != stats.TotalRequests {
		t.Errorf("hits+misses != total")
	}
	if stats.RoutesCount < 3 {
		t.Errorf("expected >=3 routes in stats, got %d", stats.RoutesCount)
	}
}

// helper to build a matcher with a bunch of static routes
func buildStaticMatcher(b *testing.B, n int) Matcher {
	m := newServerMatcher()
	for i := 0; i < n; i++ {
		p := "/static/" + strconv.Itoa(i)
		_ = m.AddRoute("GET", p, hA)
	}
	return m
}

// helper to build a matcher with parameter routes
func buildParamMatcher(b *testing.B, n int) Matcher {
	m := newServerMatcher()
	for i := 0; i < n; i++ {
		p := "/users/:id" // same pattern reused; in real impl you might want unique patterns
		_ = m.AddRoute("GET", p, hB)
	}
	return m
}

// Benchmark: 静态路由匹配性能
func BenchmarkStaticMatch(b *testing.B) {
	m := buildStaticMatcher(b, 1000)
	target := "/static/500"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := m.Match("GET", target)
		if res == nil {
			b.Fatal("no match")
		}
	}
}

// Benchmark: 参数路由匹配性能 (/users/:id)
func BenchmarkParamMatch(b *testing.B) {
	m := newServerMatcher()
	_ = m.AddRoute("GET", "/users/:id", hB)

	target := "/users/123456"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := m.Match("GET", target)
		_ = res // can't assert too much because CompressedRadixTree is stub
	}
}

// Benchmark: 带查询字符串的匹配 + query 解析
func BenchmarkQueryMatch(b *testing.B) {
	m := newServerMatcher()
	_ = m.AddRoute("GET", "/search", hA)
	target := "/search?q=hello+world&q=bye&cat=images%2Fpng"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := m.Match("GET", target)
		if res == nil {
			b.Fatal("no match")
		}
		// optional: check param merge cost
		if _, ok := res.GetParam("q"); !ok {
			b.Fatal("missing q param")
		}
	}
}

// Benchmark: 并行匹配以测 RWMutex.RLock 的扩展性
func BenchmarkParallelMatch(b *testing.B) {
	m := newServerMatcher()
	_ = m.AddRoute("GET", "/health", hA)
	_ = m.AddRoute("GET", "/users/:id", hB)
	_ = m.AddRoute("GET", "/search", hC)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 3 {
			case 0:
				_ = m.Match("GET", "/health")
			case 1:
				_ = m.Match("GET", "/users/999")
			case 2:
				_ = m.Match("GET", "/search?q=x&y=z")
			}
			i++
		}
	})
}

// buildScaledMatcher 构建指定规模的 matcher
func buildScaledMatcher(n int) Matcher {
	m := newServerMatcher()

	// 静态路由
	for i := 0; i < n; i++ {
		p := "/svc" + strconv.Itoa(i) + "/res" + strconv.Itoa(i) + "/detail/k"
		_ = m.AddRoute("GET", p, hA)
	}

	// 参数路由
	for i := 0; i < n/50; i++ { // 2%
		_ = m.AddRoute("GET", "/tenant/:tid/app/:app/version/:ver/item", hB)
	}

	// 通配符
	for i := 0; i < n/50; i++ {
		if i%2 == 0 {
			_ = m.AddRoute("GET", "/blob/*rest", hC)
		} else {
			_ = m.AddRoute("GET", "/dump/*all", hC)
		}
	}

	// 不同方法
	_ = m.AddRoute("POST", "/admin/reset", hA)
	_ = m.AddRoute("PUT", "/devices/:dev/config", hB)
	_ = m.AddRoute("DELETE", "/kill/:target", hC)
	_ = m.AddRoute("GET", "/health", hA)
	_ = m.AddRoute("GET", "/metrics", hB)
	_ = m.AddRoute("GET", "/", hA)

	return m
}

type benchReq struct {
	method string
	path   string
}

// genScaledSamples 生成 n 条样本（命中/未命中混合）
func genScaledSamples(n int) []benchReq {
	out := make([]benchReq, n)
	for i := 0; i < n; i++ {
		switch i % 5 {
		case 0:
			out[i] = benchReq{"GET", fmt.Sprintf("/svc%d/res%d/detail/k?q=%d", i, i, i)}
		case 1:
			out[i] = benchReq{"GET", fmt.Sprintf("/svc%d/XXX%d/detail/k", i, i)} // miss
		case 2:
			out[i] = benchReq{"GET", fmt.Sprintf("/svc%d/res%d/detail/k/extra", i, i)} // miss
		case 3:
			out[i] = benchReq{"GET", fmt.Sprintf("/metrics?type=node&n=%d", i)}
		default:
			out[i] = benchReq{"GET", fmt.Sprintf("/non/existing/%d", i)} // 404
		}
	}
	return out
}

// genEvilScaledSamples 构造超长URL样本
func genEvilScaledSamples(n int) []benchReq {
	out := make([]benchReq, n)
	longSeg := strings.Repeat("A", 512)
	longQ := "?" + strings.Repeat("q=1&", 100) + "x=end"
	for i := 0; i < n; i++ {
		switch i % 3 {
		case 0:
			out[i] = benchReq{"GET", "/dump/" + longSeg + "/" + strconv.Itoa(i)}
		case 1:
			out[i] = benchReq{"GET", "/metrics" + longQ}
		default:
			out[i] = benchReq{"GET", "/health?a=%ZZ%ZZ" + longQ}
		}
	}
	return out
}

// runScaledBench 统一执行
func runScaledBench(b *testing.B, m Matcher, samples []benchReq) {
	n := len(samples)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := samples[i%n]
		_ = m.Match(req.method, req.path)
	}
}

// 并行执行
func runScaledBenchParallel(b *testing.B, m Matcher, samples []benchReq) {
	n := len(samples)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := samples[i%n]
			_ = m.Match(req.method, req.path)
			i++
		}
	})
}

// ---- Benchmark for different scales ----

func BenchmarkRouter_Scale_100(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(100), genScaledSamples(100))
}
func BenchmarkRouter_Scale_500(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(500), genScaledSamples(500))
}
func BenchmarkRouter_Scale_1000(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(1000), genScaledSamples(1000))
}
func BenchmarkRouter_Scale_5000(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(5000), genScaledSamples(5000))
}
func BenchmarkRouter_Scale_10000(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(10000), genScaledSamples(10000))
}
func BenchmarkRouter_Scale_50000(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(50000), genScaledSamples(50000))
}

func BenchmarkRouter_Scale_100_Evil(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(100), genEvilScaledSamples(100))
}
func BenchmarkRouter_Scale_1000_Evil(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(1000), genEvilScaledSamples(1000))
}
func BenchmarkRouter_Scale_10000_Evil(b *testing.B) {
	runScaledBench(b, buildScaledMatcher(10000), genEvilScaledSamples(10000))
}

func BenchmarkRouter_Scale_10000_Parallel(b *testing.B) {
	m := buildScaledMatcher(10000)
	runScaledBenchParallel(b, m, genScaledSamples(10000))
}
func BenchmarkRouter_Scale_50000_Parallel(b *testing.B) {
	m := buildScaledMatcher(50000)
	runScaledBenchParallel(b, m, genScaledSamples(50000))
}

// ----------------------------
// data generation helpers
// ----------------------------

// buildHugeMatcher 构建一个极度饱和的 Matcher，包含：
// - 上万条静态路由
// - 一批参数路由
// - 一批通配符路由
// - 不同 HTTP 方法
//
// 注意：你的 MethodMatcher 实现需要可以承受这么多 AddRoute() 调用。
// 如果你的实现目前还没完全填上 radixTree/segmentIndex.search，
// 这些基准可以先聚焦静态路由命中/未命中成本，等实现完整后再全开。
func buildHugeMatcher(numStatic int, numParam int, numWild int) Matcher {
	m := newServerMatcher()

	// 1. 超大量静态路由 (GET /svc{i}/res{j}/detail/k)
	for i := 0; i < numStatic; i++ {
		p := "/svc" + strconv.Itoa(i) + "/res" + strconv.Itoa(i) + "/detail/k"
		_ = m.AddRoute("GET", p, hA)
	}

	// 2. 参数路由 (GET /tenant/:tid/app/:app/version/:ver/item)
	//    用固定pattern重复AddRoute是允许的（但实际效果是同一个路由）。
	//    如果你以后要禁止重复AddRoute，可以在这里稍微扰动pattern，比如引入索引位进pattern。
	for i := 0; i < numParam; i++ {
		p := "/tenant/:tid/app/:app/version/:ver/item"
		_ = m.AddRoute("GET", p, hB)
	}

	// 3. 通配符路由 (GET /blob/*rest, /dump/*all) 多条
	for i := 0; i < numWild; i++ {
		if i%2 == 0 {
			_ = m.AddRoute("GET", "/blob/*rest", hC)
		} else {
			_ = m.AddRoute("GET", "/dump/*all", hC)
		}
	}

	// 4. 不同方法
	_ = m.AddRoute("POST", "/admin/reset", hA)
	_ = m.AddRoute("PUT", "/devices/:dev/config", hB)
	_ = m.AddRoute("DELETE", "/kill/:target", hC)
	_ = m.AddRoute("GET", "/health", hA)
	_ = m.AddRoute("GET", "/metrics", hB)
	_ = m.AddRoute("GET", "/", hA)

	return m
}

// genHitSamples 生成 num 个“命中样本”路径，
// 基于我们上面注册过的超大量静态路由："/svc{i}/res{i}/detail/k"
func genHitSamples(num int) []benchReq {
	out := make([]benchReq, num)
	for i := 0; i < num; i++ {
		path := "/svc" + strconv.Itoa(i) + "/res" + strconv.Itoa(i) + "/detail/k"
		// 带点轻量 query，模拟常规请求
		if i%4 == 0 {
			path += "?q=req" + strconv.Itoa(i)
		} else if i%4 == 1 {
			path += "?q=req" + strconv.Itoa(i) + "&cat=images%2Fpng"
		} else if i%4 == 2 {
			path += "?flag&empty="
		} else {
			// 有坏编码，触发 fallback
			path += "?a=%ZZ&b=ok"
		}
		out[i] = benchReq{
			method: "GET",
			path:   path,
		}
	}
	return out
}

// genMissSamples 生成 num 个“未命中样本”路径：
// - 相似但不完全匹配的路由
// - 段数不对
// - 错误方法
// - 缺少必需段
func genMissSamples(num int) []benchReq {
	out := make([]benchReq, num)
	for i := 0; i < num; i++ {
		switch i % 5 {
		case 0:
			// 改一个段名，应该 miss
			out[i] = benchReq{"GET", "/svc" + strconv.Itoa(i) + "/XXX" + strconv.Itoa(i) + "/detail/k"}
		case 1:
			// 少一段
			out[i] = benchReq{"GET", "/svc" + strconv.Itoa(i) + "/res" + strconv.Itoa(i)}
		case 2:
			// 多一段
			out[i] = benchReq{"GET", "/svc" + strconv.Itoa(i) + "/res" + strconv.Itoa(i) + "/detail/k/extra"}
		case 3:
			// 正确路径但错误方法
			out[i] = benchReq{"POST", "/svc" + strconv.Itoa(i) + "/res" + strconv.Itoa(i) + "/detail/k"}
		default:
			// 完全不存在的路径
			out[i] = benchReq{"GET", "/non/existing/" + strconv.Itoa(i)}
		}
	}
	return out
}

// genMixedSamples 生成命中+未命中混合（真实流量风格）
func genMixedSamples(hits, misses []benchReq) []benchReq {
	out := make([]benchReq, 0, len(hits)+len(misses))
	// 交错插入：hit -> miss -> hit -> miss...
	maxN := len(hits)
	if len(misses) > maxN {
		maxN = len(misses)
	}
	for i := 0; i < maxN; i++ {
		if i < len(hits) {
			out = append(out, hits[i])
		}
		if i < len(misses) {
			out = append(out, misses[i])
		}
	}
	return out
}

// genEvilSamples 生成“恶意/超限”类请求：
// - 非常长的路径
// - 非常长的 query
// - 大量重复的 query key
// - 看起来像攻击扫描
// - 包含 fragment
func genEvilSamples(num int) []benchReq {
	out := make([]benchReq, num)

	longSegment := strings.Repeat("X", 512) // 单段 512 字符
	longTail := "/" + longSegment + "/" + longSegment + "/" + longSegment

	// 构建超大 query
	var bigQueryBuilder strings.Builder
	for i := 0; i < 200; i++ {
		if i > 0 {
			bigQueryBuilder.WriteByte('&')
		}
		fmt.Fprintf(&bigQueryBuilder, "k%d=%s", i, longSegment)
	}
	bigQuery := bigQueryBuilder.String()

	rand.Seed(uint64(time.Now().UnixNano()))

	for i := 0; i < num; i++ {
		switch i % 4 {
		case 0:
			// path 巨长 + 正常方法
			out[i] = benchReq{
				method: "GET",
				path:   "/dump/" + longSegment + longTail,
			}
		case 1:
			// path 正常但 query 巨大
			out[i] = benchReq{
				method: "GET",
				path:   "/metrics?" + bigQuery,
			}
		case 2:
			// 无法解析的奇怪编码，想逼 fallback
			out[i] = benchReq{
				method: "GET",
				path:   "/health?a=%ZZ%ZZ%ZZ%ZZ%ZZ&x=1#frag",
			}
		default:
			// 看起来是扫描/探针，多层不存在段
			out[i] = benchReq{
				method: "GET",
				path:   "/../../../../etc/passwd?try=1&rand=" + strconv.Itoa(rand.Intn(1_000_000)),
			}
		}
	}
	return out
}

// ----------------------------------
// bench runners
// ----------------------------------

func runBenchCases(b *testing.B, m Matcher, cases []benchReq) {
	n := len(cases)
	if n == 0 {
		b.Fatal("no cases to run")
	}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c := cases[i%n]
		res := m.Match(c.method, c.path)
		_ = res // don't assert here to avoid skewing timing
	}
}

func runBenchCasesParallel(b *testing.B, m Matcher, cases []benchReq) {
	n := len(cases)
	if n == 0 {
		b.Fatal("no cases to run")
	}
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c := cases[i%n]
			_ = m.Match(c.method, c.path)
			i++
		}
	})
}

// ----------------------------------
// actual Benchmarks
// ----------------------------------

// 我们在这里定义 1e4 (=10000) 级别的数据集，
// 这基本能模拟“上千~上万路由，海量不同请求URL”的生产压测场景。
const (
	numStaticRoutes = 10_000 // 静态路由数量
	// 参数&通配符路由我们少一点，通常实际服务这些是热路由但不会上万
	numParamRoutes = 100
	numWildRoutes  = 100
	numSamplesEach = 10_000 // 生成的样本请求数
)

// BenchmarkRouter_HitOnly_1e4
// 纯命中请求：测最佳情况（路由和请求都高度匹配），
// 用于观察最优吞吐 / 最低 allocs。
func BenchmarkRouter_HitOnly_1e4(b *testing.B) {
	m := buildHugeMatcher(numStaticRoutes, numParamRoutes, numWildRoutes)
	hitSamples := genHitSamples(numSamplesEach)
	runBenchCases(b, m, hitSamples)
}

// BenchmarkRouter_MissOnly_1e4
// 全未命中：测“扫描/探测/攻击流量”压力下的最坏分支。
// 这里我们可以观测路由树 miss 成本、allocs、GC。
func BenchmarkRouter_MissOnly_1e4(b *testing.B) {
	m := buildHugeMatcher(numStaticRoutes, numParamRoutes, numWildRoutes)
	missSamples := genMissSamples(numSamplesEach)
	runBenchCases(b, m, missSamples)
}

// BenchmarkRouter_MixedHitMiss_1e4
// 命中 + 未命中交错：模拟真实线上。
// 通常线上并不是100%命中，很多是404、method不对、路径不完整等。
func BenchmarkRouter_MixedHitMiss_1e4(b *testing.B) {
	m := buildHugeMatcher(numStaticRoutes, numParamRoutes, numWildRoutes)
	hitSamples := genHitSamples(numSamplesEach)
	missSamples := genMissSamples(numSamplesEach)
	mixed := genMixedSamples(hitSamples, missSamples)
	runBenchCases(b, m, mixed)
}

// BenchmarkRouter_Evil_1e4
// 各种长URL、超长query、非法编码、路径遍历尝试。
// 这个场景用于压 query 解析 fallback、极长 path slice、
// 同时逼内存分配（strings.Builder in ParseQueryParams 等）。
func BenchmarkRouter_Evil_1e4(b *testing.B) {
	m := buildHugeMatcher(numStaticRoutes, numParamRoutes, numWildRoutes)
	evilSamples := genEvilSamples(numSamplesEach)
	runBenchCases(b, m, evilSamples)
}

// BenchmarkRouter_MixedParallel_1e4
// 并发跑 Mixed 流量（命中+未命中），用 RunParallel 来模拟高并发 server。
// 主要评估：
// - RWMutex.RLock 路径在高并发下的可扩展性
// - GC/alloc 行为在并行下是否恶化
func BenchmarkRouter_MixedParallel_1e4(b *testing.B) {
	m := buildHugeMatcher(numStaticRoutes, numParamRoutes, numWildRoutes)
	hitSamples := genHitSamples(numSamplesEach)
	missSamples := genMissSamples(numSamplesEach)
	mixed := genMixedSamples(hitSamples, missSamples)
	runBenchCasesParallel(b, m, mixed)
}
