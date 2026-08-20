package webbench

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"github.com/julienschmidt/httprouter"
)

// ghttp-C 原型:验证「方案 C typed 执行器 + 零反射 codec + 池化上下文」相对
// gin/echo 的真实位置。路由层复用 httprouter(与 gin 同源 radix,隔离出
// 执行器/codec 这个唯一变量),不代表最终实现;最终版复用 ghttp 自有的、已
// 对齐 gin 的压缩前缀树。
//
// ghttp-C prototype: measures where "scheme-C typed executor + zero-reflection
// codec + pooled context" actually sits versus gin/echo. Routing reuses
// httprouter (same radix lineage as gin) to isolate the executor/codec as the
// only variable; the real implementation reuses ghttp's own tree.

// ---- 池化的每请求上下文 ----
type ghcCtx struct {
	r      *http.Request
	params httprouter.Params
	w      http.ResponseWriter
	buf    []byte // 复用的编码缓冲
}

var ghcPool = sync.Pool{New: func() any { return &ghcCtx{buf: make([]byte, 0, 512)} }}

// ---- 方案 C typed 执行器 ----
type ghcInput[P, Q, B any] struct {
	Path  P
	Query Q
	Body  B
}
type ghcNoQuery struct{}
type ghcNoBody struct{}

type ghcHandler interface {
	serve(*ghcCtx) error
}

type ghcCompiled[P, Q, B, O any] struct {
	decodeP func(*ghcCtx) (P, error)
	decodeQ func(*ghcCtx) (Q, error)
	decodeB func(*ghcCtx) (B, error)
	h       func(context.Context, ghcInput[P, Q, B]) (O, error)
	encode  func(*ghcCtx, O) error
}

func (c *ghcCompiled[P, Q, B, O]) serve(cx *ghcCtx) error {
	var in ghcInput[P, Q, B]
	var err error
	if in.Path, err = c.decodeP(cx); err != nil {
		return err
	}
	if in.Query, err = c.decodeQ(cx); err != nil {
		return err
	}
	if in.Body, err = c.decodeB(cx); err != nil {
		return err
	}
	out, err := c.h(cx.r.Context(), in)
	if err != nil {
		return err
	}
	return c.encode(cx, out)
}

// ---- 零反射编码器(注册期为具体输出类型固定;此处手写模拟生成产物)----
func ghcWriteJSON(cx *ghcCtx, buf []byte) error {
	h := cx.w.Header()
	h["Content-Type"] = ghcJSONCT
	cx.w.WriteHeader(http.StatusOK)
	_, err := cx.w.Write(buf)
	return err
}

var ghcJSONCT = []string{"application/json"}

func ghcEncUserOut(cx *ghcCtx, o userOut) error {
	b := cx.buf[:0]
	b = append(b, `{"id":`...)
	b = strconv.AppendInt(b, o.ID, 10)
	b = append(b, `,"name":`...)
	b = strconv.AppendQuote(b, o.Name)
	b = append(b, `,"email":`...)
	b = strconv.AppendQuote(b, o.Email)
	b = append(b, `,"age":`...)
	b = strconv.AppendInt(b, int64(o.Age), 10)
	b = append(b, `,"active":`...)
	b = strconv.AppendBool(b, o.Active)
	b = append(b, `,"tags":[`...)
	for i, t := range o.Tags {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendQuote(b, t)
	}
	b = append(b, `]}`...)
	cx.buf = b
	return ghcWriteJSON(cx, b)
}

func ghcEncProfile(cx *ghcCtx, p profile) error {
	// 复杂类型:注册期可回退 encoding/json;此处走 stdlib,证明「回退路径」存在。
	// complex types may fall back to encoding/json at registration; use stdlib
	// here to prove the fallback path exists.
	buf, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return ghcWriteJSON(cx, buf)
}

func ghcEncOrderOut(cx *ghcCtx, o orderOut) error {
	b := cx.buf[:0]
	b = append(b, `{"order_id":`...)
	b = strconv.AppendQuote(b, o.OrderID)
	b = append(b, `,"user_id":`...)
	b = strconv.AppendQuote(b, o.UserID)
	b = append(b, `,"currency":`...)
	b = strconv.AppendQuote(b, o.Currency)
	b = append(b, `,"expand":`...)
	b = strconv.AppendQuote(b, o.Expand)
	b = append(b, `,"request_id":`...)
	b = strconv.AppendQuote(b, o.RequestID)
	b = append(b, `,"item_count":`...)
	b = strconv.AppendInt(b, int64(o.ItemCount), 10)
	b = append(b, `,"total":`...)
	b = strconv.AppendFloat(b, o.Total, 'g', -1, 64)
	b = append(b, `,"note":`...)
	b = strconv.AppendQuote(b, o.Note)
	b = append(b, '}')
	cx.buf = b
	return ghcWriteJSON(cx, b)
}

// ---- string 输出(对齐 echo/gin 的 c.String)----
func ghcWriteString(cx *ghcCtx, s string) error {
	cx.w.WriteHeader(http.StatusOK)
	_, err := cx.w.Write([]byte(s))
	return err
}

// ---- mux:httprouter 匹配 + 池化 + typed 分派 ----
type ghcMux struct{ r *httprouter.Router }

func (m *ghcMux) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.r.ServeHTTP(w, r) }

// wrap 把 ghcHandler 适配成 httprouter.Handle,负责取还池化上下文。
func ghcWrap(h ghcHandler) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		cx := ghcPool.Get().(*ghcCtx)
		cx.r, cx.params, cx.w = r, ps, w
		_ = h.serve(cx)
		cx.r, cx.params, cx.w = nil, nil, nil
		ghcPool.Put(cx)
	}
}

func newGhttpC() http.Handler {
	r := httprouter.New()

	// static /ping → "pong"(对齐 echo/gin)
	r.GET("/ping", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		cx := ghcPool.Get().(*ghcCtx)
		cx.w = w
		_ = ghcWriteString(cx, "pong")
		cx.w = nil
		ghcPool.Put(cx)
	})

	// param1 /users/:id → id
	r.GET("/users/:id", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		cx := ghcPool.Get().(*ghcCtx)
		cx.w = w
		_ = ghcWriteString(cx, ps.ByName("id"))
		cx.w = nil
		ghcPool.Put(cx)
	})

	// param5
	r.GET("/orgs/:org/teams/:team/members/:member/roles/:role/perms/:perm",
		func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
			cx := ghcPool.Get().(*ghcCtx)
			cx.w = w
			_ = ghcWriteString(cx, ps.ByName("org")+ps.ByName("team")+ps.ByName("member")+ps.ByName("role")+ps.ByName("perm"))
			cx.w = nil
			ghcPool.Put(cx)
		})

	// wildcard /files/*path
	r.GET("/files/*path", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		cx := ghcPool.Get().(*ghcCtx)
		cx.w = w
		_ = ghcWriteString(cx, ps.ByName("path"))
		cx.w = nil
		ghcPool.Put(cx)
	})

	// query /search
	r.GET("/search", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		cx := ghcPool.Get().(*ghcCtx)
		cx.w = w
		q := req.URL.Query()
		_ = ghcWriteString(cx, q.Get("q")+q.Get("page")+q.Get("limit"))
		cx.w = nil
		ghcPool.Put(cx)
	})

	// json_bind: POST /users — typed 执行器 + 零反射编码
	usersPost := &ghcCompiled[ghcNoBody, ghcNoQuery, userIn, userOut]{
		decodeP: func(*ghcCtx) (ghcNoBody, error) { return ghcNoBody{}, nil },
		decodeQ: func(*ghcCtx) (ghcNoQuery, error) { return ghcNoQuery{}, nil },
		decodeB: func(cx *ghcCtx) (userIn, error) {
			var in userIn
			return in, json.NewDecoder(cx.r.Body).Decode(&in)
		},
		h: func(_ context.Context, in ghcInput[ghcNoBody, ghcNoQuery, userIn]) (userOut, error) {
			return makeUserOut(in.Body), nil
		},
		encode: ghcEncUserOut,
	}
	r.POST("/users", ghcWrap(usersPost))

	// json_resp: GET /profile — 复杂类型走 stdlib 回退
	profileGet := &ghcCompiled[ghcNoBody, ghcNoQuery, ghcNoBody, profile]{
		decodeP: func(*ghcCtx) (ghcNoBody, error) { return ghcNoBody{}, nil },
		decodeQ: func(*ghcCtx) (ghcNoQuery, error) { return ghcNoQuery{}, nil },
		decodeB: func(*ghcCtx) (ghcNoBody, error) { return ghcNoBody{}, nil },
		h: func(_ context.Context, _ ghcInput[ghcNoBody, ghcNoQuery, ghcNoBody]) (profile, error) {
			return profileFixture, nil
		},
		encode: ghcEncProfile,
	}
	r.GET("/profile", ghcWrap(profileGet))

	// middleware5
	var mwPing httprouter.Handle = func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		cx := ghcPool.Get().(*ghcCtx)
		cx.w = w
		_ = ghcWriteString(cx, "pong")
		cx.w = nil
		ghcPool.Put(cx)
	}
	for i := 0; i < middlewareCount; i++ {
		next := mwPing
		mwPing = func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) { next(w, req, ps) }
	}
	r.GET("/mw/ping", mwPing)

	// full_chain: PUT /api/v1/users/:id/orders — path+query+header+body → JSON
	type chainMeta struct {
		userID, expand, currency, requestID string
	}
	ordersPut := &ghcCompiled[string, chainMeta, orderIn, orderOut]{
		decodeP: func(cx *ghcCtx) (string, error) { return cx.params.ByName("id"), nil },
		decodeQ: func(cx *ghcCtx) (chainMeta, error) {
			q := cx.r.URL.Query()
			return chainMeta{
				expand:    q.Get("expand"),
				currency:  q.Get("currency"),
				requestID: cx.r.Header.Get("X-Request-ID"),
			}, nil
		},
		decodeB: func(cx *ghcCtx) (orderIn, error) {
			var in orderIn
			return in, json.NewDecoder(cx.r.Body).Decode(&in)
		},
		h: func(_ context.Context, in ghcInput[string, chainMeta, orderIn]) (orderOut, error) {
			return makeOrderOut(in.Path, in.Query.expand, in.Query.currency, in.Query.requestID, in.Body), nil
		},
		encode: ghcEncOrderOut,
	}
	r.PUT("/api/v1/users/:id/orders", ghcWrap(ordersPut))

	// route_scale_200
	idH := func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		cx := ghcPool.Get().(*ghcCtx)
		cx.w = w
		_ = ghcWriteString(cx, ps.ByName("id"))
		cx.w = nil
		ghcPool.Put(cx)
	}
	okH := func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		cx := ghcPool.Get().(*ghcCtx)
		cx.w = w
		_ = ghcWriteString(cx, "ok")
		cx.w = nil
		ghcPool.Put(cx)
	}
	for _, rt := range scaleRoutes() {
		if rt.hasID {
			pattern := rt.pattern[:len(rt.pattern)-len("{id}")] + ":id"
			r.Handle(rt.method, pattern, idH)
		} else {
			r.Handle(rt.method, rt.pattern, okH)
		}
	}

	return &ghcMux{r: r}
}

func init() {
	register(&httpTarget{n: "ghttp-C", h: newGhttpC()}, nil)
}
