# ghttp Server 侧问题修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复真实使用与 13 框架横评发现的 ghttp server 侧问题，并按“标准 HTTP > WS/SSE > client”的优先级落地。

**Architecture:** 以 `CodecManager.Select` 作为唯一协商实现，收敛 envelope/错误/正常响应的 codec 选择；新增显式开关（错误详情、严格协商、严格 Content-Type、validator）；类型化响应通过 `StatusCoder`/`ResponseHeaderWriter` 接口与 builder 显式声明状态码和 header；路径参数经 request context 提供给 middleware；WS/SSE、能力层、OpenAPI、性能按批次 3 实施。

**Tech Stack:** Go（当前模块内 ghttp 包）、net/http、httptest、encoding/json、gorilla/websocket（已存在依赖）、log/slog（标准库）。

## Global Constraints

- 只实施 server 侧；client（WS6）不做正式实现，仅允许 demo 或延后占位。
- server 侧优先级：标准 HTTP 方法 > WS/SSE > client；WS/SSE 不得挤占标准 HTTP 方法相关改动。
- 显式优于隐式：影响外部可观察行为的特性必须显式开启，默认保守；协议级便利默认值必须在 README 明示。
- 非 200 必须返回真实 HTTP 状态码与内容；envelope 只改 body 表示，不得改写状态码。
- 当前泛型 API 是 Go 1.27 泛型方法落地前的过渡形态；不得把泛型参数固化进 Server/Group，RouteBuilder 方法链必须保持可行。
- matcher 与参数提取热路径不得新增分配；性能改动需 `-benchmem` 证据。
- 影响 HTTP 语义的行为必须有真实 HTTP 请求测试（httptest），不能只靠单测。
- 新能力以“能力接口 + 默认实现”提供；如无必要不得新增第三方依赖。
- 所有破坏性变更需同步更新 README 与迁移说明。
- 提交动作仅当用户明确授权；未授权时跳过 commit 步骤。

---

## 范围与批次

非目标（本计划不实施）：WS6 client 修复、zap 适配子包、限流/审计/指标钩子。zap 适配与限流等留待后续独立计划。

| 批次 | 内容 | 对应 WS | 优先级 |
|------|------|---------|--------|
| 批次 1 | HTTP 规范与显式化：协商闭环、envelope 尊重 Produces、406、严格 Content-Type、错误详情开关、validator 显式化 | WS1、WS2 | P0 |
| 批次 2 | 类型化状态码/header、middleware 路径参数可见性 | WS3、WS4 | P1 |
| 批次 3 | SSE/WS 协议与安全、slog 适配、RBAC 能力、OpenAPI 增强、性能预算 | WS5、WS7、WS8、WS9 | P2 |

每批独立可评审、可交付；批次 3 内 WS5 不得早于标准 HTTP 方法相关改动。

---

## 批次 1：HTTP 规范与显式化（P0）

### Task 1.1: Accept 协商收敛（q=0 / 通配符 / 无匹配判定）

**Files:**
- Modify: `ghttp/codec_manager.go`
- Modify: `ghttp/codec_test.go`
- Modify: `ghttp/builder.go`（删除重复的 `acceptItem`/`sortedAcceptItems`，改 `selectResponseCodec`）

**Interfaces:**
- Consumes: 现有 `Codec`、`CodecManager`、`responseCodec`。
- Produces:
  - `func (m *CodecManager) Select(accept string, candidates []string) (contentType string, codec Codec, ok bool)`
  - `func parseAcceptItems(accept string) []acceptItem`（包内）
  - `func acceptMediaTypeMatches(acceptMedia, candidate string) bool`（包内）
  - 保留 `Negotiate(accept string) Codec` 作为“全部注册 codec 中选默认”的兼容包装。

- [ ] **Step 1: 写失败测试**

在 `ghttp/codec_test.go` 追加：

```go
func TestCodecManagerSelect(t *testing.T) {
	m := NewCodecManager()

	cases := []struct {
		name       string
		accept     string
		candidates []string
		wantCT     string
		wantOK     bool
	}{
		{name: "empty accept picks first", accept: "", candidates: []string{MIMEJSON, MIMEXML}, wantCT: MIMEJSON, wantOK: true},
		{name: "q=0 excludes candidate", accept: "application/json;q=0, application/xml", candidates: []string{MIMEJSON, MIMEXML}, wantCT: MIMEXML, wantOK: true},
		{name: "all q=0 no match", accept: "application/json;q=0", candidates: []string{MIMEJSON}, wantOK: false},
		{name: "wildcard type matches", accept: "application/*", candidates: []string{MIMEJSON, "text/plain"}, wantCT: MIMEJSON, wantOK: true},
		{name: "no acceptable candidate", accept: "text/html", candidates: []string{MIMEJSON}, wantOK: false},
		{name: "higher q wins", accept: "application/xml;q=0.5, application/json;q=0.9", candidates: []string{MIMEJSON, MIMEXML}, wantCT: MIMEJSON, wantOK: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ct, _, ok := m.Select(tc.accept, tc.candidates)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && ct != tc.wantCT {
				t.Fatalf("content type = %q, want %q", ct, tc.wantCT)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./ghttp/ -run TestCodecManagerSelect -count=1`
Expected: 编译失败，`m.Select` 不存在。

- [ ] **Step 3: 实现 Select 与解析助手**

在 `ghttp/codec_manager.go` 追加：

```go
// Select negotiates the best registered codec from candidates using RFC 9110
// Accept semantics: q=0 excludes a media range, wildcards match, equal
// qualities keep declaration order. ok=false means no candidate is acceptable.
func (m *CodecManager) Select(accept string, candidates []string) (string, Codec, bool) {
	if len(candidates) == 0 {
		return "", nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if strings.TrimSpace(accept) == "" || strings.TrimSpace(accept) == "*/*" {
		codec, ok := m.codecs[candidates[0]]
		return candidates[0], codec, ok
	}
	for _, item := range parseAcceptItems(accept) {
		for _, candidate := range candidates {
			if acceptMediaTypeMatches(item.contentType, candidate) {
				codec, ok := m.codecs[candidate]
				return candidate, codec, ok
			}
		}
	}
	return "", nil, false
}

type acceptItem struct {
	contentType string
	quality     float64
	index       int
}

func parseAcceptItems(accept string) []acceptItem {
	parts := strings.Split(accept, ",")
	items := make([]acceptItem, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		mediaType, params, err := mime.ParseMediaType(part)
		if err != nil {
			mediaType = normalizeContentType(part)
		}
		if mediaType == "" {
			continue
		}
		quality := 1.0
		if params != nil {
			if q, ok := params["q"]; ok {
				if parsed, err := strconv.ParseFloat(q, 64); err == nil {
					quality = parsed
				}
			}
		}
		if quality <= 0 {
			continue
		}
		items = append(items, acceptItem{contentType: strings.ToLower(mediaType), quality: quality, index: i})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].quality == items[j].quality {
			return items[i].index < items[j].index
		}
		return items[i].quality > items[j].quality
	})
	return items
}

func acceptMediaTypeMatches(acceptMedia, candidate string) bool {
	if acceptMedia == "*/*" {
		return true
	}
	if strings.HasSuffix(acceptMedia, "/*") {
		return strings.HasPrefix(candidate, strings.TrimSuffix(acceptMedia, "*"))
	}
	return acceptMedia == candidate
}
```

将 `Negotiate`/`negotiate` 内部改为复用 `parseAcceptItems`（删除其内联解析与 `acceptItem` 定义），无匹配时仍返回 `m.codecs[m.defaultCT]` 以保持兼容。

- [ ] **Step 4: 收敛 selectResponseCodec**

删除 `ghttp/builder.go` 中的 `type acceptItem` 与 `sortedAcceptItems`，将 `selectResponseCodec` 改为：

```go
func selectResponseCodec(s *Server, accept string, produces []string, codecs []responseCodec) (string, Codec) {
	if len(codecs) == 0 {
		if len(produces) == 0 && s != nil {
			produces = s.produces
		}
		codecs = resolveResponseCodecs(s, produces)
	}
	if len(codecs) == 0 {
		return "", nil
	}
	candidates := make([]string, len(codecs))
	for i, c := range codecs {
		candidates[i] = c.contentType
	}
	contentType, codec, ok := s.codecMgr.Select(accept, candidates)
	if !ok {
		return "", nil
	}
	return contentType, codec
}
```

- [ ] **Step 5: 运行测试**

Run: `go test ./ghttp/ -run 'TestCodecManager|TestServer|TestRoute' -count=1`
Expected: 全部通过（`Negotiate` 行为不变）。

- [ ] **Step 6: 提交（仅当授权）**

```bash
git add ghttp/codec_manager.go ghttp/codec_test.go ghttp/builder.go
git commit -m "feat(ghttp): unify accept negotiation with q=0 and wildcard semantics"
```

---

### Task 1.2: Envelope 尊重路由 Produces（签名演进）

**Files:**
- Modify: `ghttp/output.go`
- Modify: `ghttp/builder.go`（所有 `server.envelope(...)` 调用点）
- Modify: `ghttp/output_test.go`、`ghttp/builder_test.go`、`ghttp/server_full_test.go`

**Interfaces:**
- Produces（破坏性变更，README 标注）:
  `type EnvelopeFunc func(w http.ResponseWriter, r *http.Request, statusCode int, resp interface{}, err error, contentType string, codec Codec)`
- Produces（包内）: `func negotiateRouteCodec(w http.ResponseWriter, r *http.Request, s *Server, produces []string, codecs []responseCodec) (string, Codec, bool)`

- [ ] **Step 1: 写失败测试**

在 `ghttp/output_test.go` 追加：

```go
func TestEnvelopeUsesRouteProduces(t *testing.T) {
	type Resp struct {
		Name string `json:"name"`
	}
	app := New(WithEnvelope(DefaultEnvelope), WithProduces(MIMEJSON))
	Route[struct{}, Resp](app).GET("/users/{id}").
		Produces(MIMEXML).
		To(func(context.Context, struct{}) (Resp, error) {
			return Resp{Name: "alice"}, nil
		})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	req.Header.Set("Accept", MIMEXML)
	app.ServeHTTP(w, req)

	if got := w.Header().Get("Content-Type"); got != MIMEXML {
		t.Fatalf("content type = %q, want %q", got, MIMEXML)
	}
}
```

Run: `go test ./ghttp/ -run TestEnvelopeUsesRouteProduces -count=1`
Expected: 失败（当前 envelope 按 `Accept` 从全局 codecMgr 选，忽略路由 Produces）。

- [ ] **Step 2: 改 EnvelopeFunc 签名与 DefaultEnvelope**

```go
type EnvelopeFunc func(w http.ResponseWriter, r *http.Request, statusCode int, resp interface{}, err error, contentType string, codec Codec)

func DefaultEnvelope(w http.ResponseWriter, r *http.Request, statusCode int, resp interface{}, err error, contentType string, codec Codec) {
	if codec == nil {
		codec = &JSONCodec{}
		contentType = MIMEJSON
	}
	w.Header().Set("Content-Type", contentType)

	var envelope struct {
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
		Data interface{} `json:"data,omitempty"`
	}
	if err != nil {
		he := AsError(err)
		if he != nil {
			statusCode = he.Code
			envelope.Code = he.Code
			envelope.Msg = he.Message
		} else {
			envelope.Code = statusCode
			envelope.Msg = http.StatusText(statusCode)
		}
	} else {
		envelope.Msg = "success"
		envelope.Data = resp
	}
	w.WriteHeader(statusCode)
	_ = codec.Marshal(w, &envelope)
}
```

- [ ] **Step 3: 新增协商助手并替换调用点**

在 `ghttp/builder.go` 追加：

```go
func negotiateRouteCodec(w http.ResponseWriter, r *http.Request, s *Server, produces []string, codecs []responseCodec) (string, Codec, bool) {
	contentType, codec := selectResponseCodec(s, r.Header.Get("Accept"), produces, codecs)
	if codec != nil {
		return contentType, codec, true
	}
	if len(codecs) == 0 {
		writeError(w, r, s, http.StatusInternalServerError, ErrRouteProducesUnsupported)
		return "", nil, false
	}
	return codecs[0].contentType, codecs[0].codec, true
}
```

将 `buildHandler`、`buildParamsHandler`、`buildParamsHandlerWithGlobalValidator` 中的：

```go
if server.envelope != nil {
	server.envelope(w, r, http.StatusOK, resp, nil, server.codecMgr)
	return
}
```

统一替换为：

```go
contentType, codec, ok := negotiateRouteCodec(w, r, server, b.produces, b.codecs)
if !ok {
	return
}
if server.envelope != nil {
	server.envelope(w, r, http.StatusOK, resp, nil, contentType, codec)
	return
}
writeResponse(w, r, server, http.StatusOK, b.produces, b.codecs, resp)
```

将 `writeErrorWithCodec` 改为先取 codec、失败回退 JSON，再调用 envelope：

```go
func writeErrorWithCodec(w http.ResponseWriter, r *http.Request, s *Server, defaultCode int, err error, produces []string, codecs []responseCodec) {
	if responseErrorWriteBlocked(r) {
		return
	}
	if s == nil {
		http.Error(w, err.Error(), statusCodeFromError(defaultCode, err))
		return
	}
	code := statusCodeFromError(defaultCode, err)
	body := HTTPError{Code: code, Message: http.StatusText(code), Err: err}
	if code < http.StatusInternalServerError {
		body.Message = err.Error()
	}
	if he := AsError(err); he != nil {
		body = *he
	}
	if strings.TrimSpace(body.Message) == "" {
		body.Message = http.StatusText(code)
	}
	contentType, codec := selectResponseCodec(s, r.Header.Get("Accept"), produces, codecs)
	if codec == nil {
		contentType = MIMEJSON
		codec, _ = s.codecMgr.Resolve(MIMEJSON)
	}
	if s.envelope != nil {
		s.envelope(w, r, code, nil, err, contentType, codec)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(code)
	_ = codec.Marshal(w, &body)
}
```

- [ ] **Step 4: 更新既有调用与测试**

`ghttp/output_test.go` 中直接调用 `app.envelope(...)` 的两处改为传入 `MIMEJSON, &JSONCodec{}`；`ghttp/server_full_test.go` 的 envelope 字面量改为新签名。

Run: `go test ./ghttp/ -run 'TestEnvelope|TestRouteBuilder|TestServer' -count=1`
Expected: 通过。

- [ ] **Step 5: 提交（仅当授权）**

```bash
git add ghttp/output.go ghttp/builder.go ghttp/output_test.go ghttp/builder_test.go ghttp/server_full_test.go
git commit -m "feat(ghttp): make envelope respect route produces"
```

---

### Task 1.3: 严格内容协商开关（406）

**Files:**
- Modify: `ghttp/config.go`、`ghttp/server.go`
- Modify: `ghttp/builder.go`（`writeResponse` 使用 `negotiateRouteCodec`）
- Modify: `ghttp/integration_test.go` 或新增 `ghttp/negotiation_test.go`

**Interfaces:**
- Produces: `func WithStrictContentNegotiation() ServerOption`
- Config 新增字段：`strictContentNegotiation bool`

- [ ] **Step 1: 写失败测试**

新增 `ghttp/negotiation_test.go`：

```go
func TestStrictContentNegotiationReturns406(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithStrictContentNegotiation())
	Route[Params, struct{ Name string `json:"name"` }](app).GET("/ping").To(func(context.Context, Params) (struct{ Name string `json:"name"` }, error) {
		return struct{ Name string `json:"name"` }{Name: "pong"}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "text/html")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNotAcceptable, w.Body.String())
	}
}

func TestLenientContentNegotiationFallsBack(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Params, map[string]string](app).GET("/ping").To(func(context.Context, Params) (map[string]string, error) {
		return map[string]string{"name": "pong"}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept", "text/html")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Header().Get("Content-Type"); got != MIMEJSON {
		t.Fatalf("content type = %q, want %q", got, MIMEJSON)
	}
}
```

Run: `go test ./ghttp/ -run 'TestStrictContentNegotiation|TestLenientContentNegotiation' -count=1`
Expected: 编译失败，`WithStrictContentNegotiation` 不存在。

- [ ] **Step 2: 实现开关**

`ghttp/config.go`：Config 增加 `strictContentNegotiation bool`；追加：

```go
// WithStrictContentNegotiation returns 406 when no route Produces candidate
// is acceptable per the request Accept header. Default: fall back to the
// first declared Produces type (documented default).
func WithStrictContentNegotiation() ServerOption {
	return func(c *Config) {
		c.strictContentNegotiation = true
	}
}
```

`ghttp/builder.go` 的 `writeResponse` 改为：

```go
func writeResponse(w http.ResponseWriter, r *http.Request, s *Server, statusCode int, produces []string, codecs []responseCodec, resp interface{}) {
	contentType, codec, ok := negotiateRouteCodec(w, r, s, produces, codecs)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	_ = codec.Marshal(w, resp)
}
```

同时给 `negotiateRouteCodec` 增加严格分支：

```go
	if s.config.strictContentNegotiation {
		writeError(w, r, s, http.StatusNotAcceptable, Err(http.StatusNotAcceptable, http.StatusText(http.StatusNotAcceptable)))
		return "", nil, false
	}
```

插在 `if codec != nil` 之后、`len(codecs) == 0` 判断之前。

- [ ] **Step 3: 运行测试**

Run: `go test ./ghttp/ -run 'TestStrictContentNegotiation|TestLenientContentNegotiation|TestEnvelope' -count=1`
Expected: 通过。

- [ ] **Step 4: README 明示默认值**

在 `ghttp/README.md` 的响应协商章节写明：默认无 Accept 匹配时回退第一个 Produces；`WithStrictContentNegotiation()` 开启后返回 406。

- [ ] **Step 5: 提交（仅当授权）**

```bash
git add ghttp/config.go ghttp/builder.go ghttp/negotiation_test.go ghttp/README.md
git commit -m "feat(ghttp): add explicit 406 content negotiation mode"
```

---

### Task 1.4: 请求侧解析走 CodecManager + FormCodec.Unmarshal + 严格 Content-Type

**Files:**
- Modify: `ghttp/codec_form.go`、`ghttp/codec_test.go`
- Modify: `ghttp/input.go`、`ghttp/builder.go`
- Modify: `ghttp/config.go`、`ghttp/README.md`

**Interfaces:**
- Produces: `func WithStrictContentType() ServerOption`
- 修改签名（包内）:
  - `func parseBody(r *http.Request, bodyField reflect.Value, c *Config, codecMgr *CodecManager) error`
  - `func parseInputWithConfigAndPathParams(r *http.Request, input interface{}, c *Config, codecMgr *CodecManager, routeParams pathParamList) error`

- [ ] **Step 1: 写失败测试**

`ghttp/codec_test.go` 追加：

```go
func TestFormCodecUnmarshal(t *testing.T) {
	codec := &FormCodec{}
	var values url.Values
	if err := codec.Unmarshal(strings.NewReader("name=alice&age=18"), &values); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if values.Get("name") != "alice" || values.Get("age") != "18" {
		t.Fatalf("values = %#v", values)
	}
}
```

`ghttp/negotiation_test.go` 追加：

```go
func TestStrictContentTypeReturns415(t *testing.T) {
	type input struct {
		Params
		Body struct {
			Name string `json:"name"`
		}
	}
	app := New(WithProduces(MIMEJSON), WithStrictContentType())
	Route[input, struct{}](app).POST("/users").To(func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("Content-Type", "application/octet-stream")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusUnsupportedMediaType, w.Body.String())
	}
}
```

Run: `go test ./ghttp/ -run 'TestFormCodecUnmarshal|TestStrictContentTypeReturns415' -count=1`
Expected: 两个测试均失败（Unmarshal 返回 nil；415 未实现）。

- [ ] **Step 2: 实现 FormCodec.Unmarshal**

```go
func (c *FormCodec) Unmarshal(r io.Reader, v interface{}) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}
	switch target := v.(type) {
	case *url.Values:
		*target = values
		return nil
	case *map[string]string:
		out := make(map[string]string, len(values))
		for k, vs := range values {
			out[k] = vs[0]
		}
		*target = out
		return nil
	case *string:
		*target = string(data)
		return nil
	case *[]byte:
		*target = data
		return nil
	default:
		return fmt.Errorf("form codec: unsupported target %T", v)
	}
}
```

- [ ] **Step 3: parseBody 走 CodecManager**

`ghttp/input.go` 改签名并替换 switch：

```go
func parseBody(r *http.Request, bodyField reflect.Value, c *Config, codecMgr *CodecManager) error {
	if bodyField.Kind() != reflect.Struct || bodyField.Type().NumField() == 0 {
		return nil
	}
	ct := r.Header.Get("Content-Type")
	if c != nil && c.bodyDecoder != nil {
		return c.bodyDecoder(r.Body, ct, bodyField.Addr().Interface())
	}
	mediaType := normalizeContentType(ct)
	codec, ok := codecMgr.Resolve(mediaType)
	if !ok {
		if c != nil && c.strictContentType {
			return Err(http.StatusUnsupportedMediaType, fmt.Sprintf("unsupported media type %q", ct), WithCause(ErrUnsupportedMediaType))
		}
		codec, ok = codecMgr.Resolve(MIMEJSON)
		if !ok {
			return fmt.Errorf("ghttp: json codec not registered")
		}
	}
	return codec.Unmarshal(r.Body, bodyField.Addr().Interface())
}
```

`ghttp/input.go` 中 `parseInputWithConfigAndPathParams` 增加 `codecMgr *CodecManager` 参数并传给 `parseBody`；`ParseInput`/`parseInput`/`parseInputWithConfig` 三个包装调用传 `nil`，`parseBody` 开头加：

```go
if codecMgr == nil {
	codecMgr = NewCodecManager()
}
```

`ghttp/builder.go` 中 `parseAndValidateInputWithPathParams` 调用改为 `parseInputWithConfigAndPathParams(r, target, server.config, server.codecMgr, params)`；其解析错误分支改为尊重 HTTPError 状态码：

```go
if err := parseInputWithConfigAndPathParams(r, target, server.config, params); err != nil {
	code := http.StatusBadRequest
	if he := AsError(err); he != nil {
		code = he.Code
	}
	if isRequestBodyTooLarge(err) {
		code = http.StatusRequestEntityTooLarge
		err = Err(code, ErrRequestBodyTooLarge.Error(), WithCause(err))
	}
	b.writeError(w, r, code, err)
	var zero Req
	return zero, false
}
```

- [ ] **Step 4: 新增开关**

`ghttp/config.go`：

```go
strictContentType bool

// WithStrictContentType returns 415 when a request Content-Type is not
// registered with the CodecManager and no route/server Consumes matched.
// Default: unknown types are decoded as JSON (documented default).
func WithStrictContentType() ServerOption {
	return func(c *Config) {
		c.strictContentType = true
	}
}
```

- [ ] **Step 5: 运行测试**

Run: `go test ./ghttp/ -run 'TestFormCodec|TestStrictContentType|TestRouteBuilderWithPOST|TestBadRequest' -count=1`
Expected: 通过。

- [ ] **Step 6: 提交（仅当授权）**

```bash
git add ghttp/codec_form.go ghttp/codec_test.go ghttp/input.go ghttp/builder.go ghttp/config.go ghttp/negotiation_test.go
git commit -m "feat(ghttp): route request decoding through codec manager with strict content type"
```

---

### Task 2.1: 错误详情显式开关（WithExposeErrorDetails）

**Files:**
- Modify: `ghttp/config.go`、`ghttp/builder.go`
- Modify: `ghttp/error_handler_test.go` 或新增 `ghttp/error_details_test.go`

**Interfaces:**
- Produces: `func WithExposeErrorDetails() ServerOption`
- Config 新增字段：`exposeErrorDetails bool`

- [ ] **Step 1: 写失败测试**

新增 `ghttp/error_details_test.go`：

```go
func TestErrorDetailsHiddenByDefault(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[Params, struct{}](app).GET("/boom").To(func(context.Context, Params) (struct{}, error) {
		return struct{}{}, Err(http.StatusBadRequest, "internal secret detail")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "internal secret detail") {
		t.Fatalf("error details leaked: %s", w.Body.String())
	}
}

func TestErrorDetailsExposedWhenEnabled(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithExposeErrorDetails())
	Route[Params, struct{}](app).GET("/boom").To(func(context.Context, Params) (struct{}, error) {
		return struct{}{}, Err(http.StatusBadRequest, "bad thing: secret")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if !strings.Contains(w.Body.String(), "bad thing: secret") {
		t.Fatalf("error details missing: %s", w.Body.String())
	}
}
```

Run: `go test ./ghttp/ -run 'TestErrorDetails' -count=1`
Expected: `TestErrorDetailsHiddenByDefault` 失败（当前 4xx 会写入 `err.Error()`，即泄露）。

- [ ] **Step 2: 实现开关与写逻辑**

`ghttp/config.go`：

```go
exposeErrorDetails bool

// WithExposeErrorDetails enables returning internal error messages in error
// response bodies. Default: non-HTTPError failures return only the HTTP
// status text; explicit HTTPError messages are always returned.
func WithExposeErrorDetails() ServerOption {
	return func(c *Config) {
		c.exposeErrorDetails = true
	}
}
```

`ghttp/builder.go` 的 `writeErrorWithCodec` 中 body 构造改为：

```go
func writeErrorWithCodec(w http.ResponseWriter, r *http.Request, s *Server, defaultCode int, err error, produces []string, codecs []responseCodec) {
	if responseErrorWriteBlocked(r) {
		return
	}
	if s == nil {
		http.Error(w, err.Error(), statusCodeFromError(defaultCode, err))
		return
	}
	code := statusCodeFromError(defaultCode, err)
	body := HTTPError{Code: code, Message: http.StatusText(code), Err: err}
	if he := AsError(err); he != nil {
		body = *he
	} else if s.config.exposeErrorDetails {
		body.Message = err.Error()
	}
	if strings.TrimSpace(body.Message) == "" {
		body.Message = http.StatusText(code)
	}
	contentType, codec := selectResponseCodec(s, r.Header.Get("Accept"), produces, codecs)
	if codec == nil {
		contentType = MIMEJSON
		codec, _ = s.codecMgr.Resolve(MIMEJSON)
	}
	if s.envelope != nil {
		s.envelope(w, r, code, nil, err, contentType, codec)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(code)
	_ = codec.Marshal(w, &body)
}
```

即：显式 `HTTPError.Message` 恒保留；非显式错误的原始 message 只在 `WithExposeErrorDetails()` 开启后进入 body。

- [ ] **Step 3: 修正测试断言并运行**

Run: `go test ./ghttp/ -run 'TestErrorDetails' -count=1`
Expected: 通过。

- [ ] **Step 4: 提交（仅当授权）**

```bash
git add ghttp/config.go ghttp/builder.go ghttp/error_details_test.go
git commit -m "feat(ghttp): explicit opt-in for exposing error details"
```

---

### Task 2.2: 服务器级 validator 默认关闭

**Files:**
- Modify: `ghttp/server.go`、`ghttp/builder.go`
- Modify: `ghttp/README.md`

**Interfaces:**
- 行为变更（破坏性）：`New()` 不再默认安装 playground validator；需 `WithValidator(v)` 显式启用。
- 保留：`WithValidator`、`RouteBuilder.Validate`、`SkipValidation`。

- [ ] **Step 1: 写失败测试**

新增到 `ghttp/builder_test.go`：

```go
func TestServerValidatorOffByDefault(t *testing.T) {
	type input struct {
		Params
		Body struct {
			Name string `json:"name" validate:"required"`
		}
	}
	app := New(WithProduces(MIMEJSON))
	Route[input, struct{}](app).POST("/users").To(func(context.Context, input) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (validator must be off by default)", w.Code)
	}
}

func TestServerValidatorExplicitOn(t *testing.T) {
	type validatedInput struct {
		Params
		Body struct {
			Name string `json:"name" validate:"required"`
		}
	}
	app := New(WithProduces(MIMEJSON), WithValidator(newDefaultValidator()))
	Route[validatedInput, struct{}](app).POST("/users").To(func(context.Context, validatedInput) (struct{}, error) {
		return struct{}{}, nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"name":""}`))
	req.Header.Set("Content-Type", MIMEJSON)
	app.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
}
```

Run: `go test ./ghttp/ -run 'TestServerValidator' -count=1`
Expected: 第一个失败（当前默认 validator 会 422）。

- [ ] **Step 2: 改默认值**

`ghttp/server.go` 的 `New()`：

```go
validator:    nil,
```

`ghttp/builder.go` 的 `directParamsGlobalValidator` 简化为：

```go
func (b *RouteBuilder[Req, Resp]) directParamsGlobalValidator() Validator {
	server := b.target.owner()
	if b.skipValidation || server.validator == nil {
		return nil
	}
	return server.validator
}
```

同时 `parseAndValidateInputWithPathParams` 中 `server.validator != nil` 分支保持不动。

- [ ] **Step 3: 修复既有测试并运行全量**

Run: `go test ./ghttp/ -count=1`
Expected: 若既有测试依赖默认 validator，修正为显式 `WithValidator(newDefaultValidator())` 后全部通过。

- [ ] **Step 4: README 标注破坏性变更**

写明：v 起 server 级 validator 默认关闭，需要 `WithValidator` 显式启用；路由级 `.Validate()` 不受影响。

- [ ] **Step 5: 提交（仅当授权）**

```bash
git add ghttp/server.go ghttp/builder.go ghttp/builder_test.go ghttp/README.md
git commit -m "feat(ghttp): server validator is explicit opt-in"
```

---

## 批次 2：类型化能力与 middleware（P1）

### Task 3.1: 类型化状态码与 header（显式接口 + builder 选项）

**Files:**
- Modify: `ghttp/handler.go`（新增接口定义）
- Modify: `ghttp/builder.go`
- Modify: `ghttp/route_definition.go`
- Modify: `ghttp/builder_test.go`、`ghttp/openapi_test.go`

**Interfaces:**
- Produces:
  - `type StatusCoder interface { StatusCode() int }`
  - `type ResponseHeaderWriter interface { WriteResponseHeaders(http.Header) }`
  - `func (b *RouteBuilder[Req, Resp]) Status(code int) *RouteBuilder[Req, Resp]`
  - `func (b *RouteBuilder[Req, Resp]) ResponseHeader(name, value string) *RouteBuilder[Req, Resp]`
  - `func responseHasBody(status int) bool`（包内）

设计说明：不用响应结构体 tag/字段名隐式推断状态；状态码与 header 必须是显式声明。接口方式零热路径反射；builder 固定值供 OpenAPI 静态推断。

- [ ] **Step 1: 写失败测试**

`ghttp/builder_test.go` 追加：

```go
type createdResp struct {
	ID string `json:"id"`
}

func (r createdResp) StatusCode() int { return http.StatusCreated }

func (r createdResp) WriteResponseHeaders(h http.Header) {
	h.Set("X-Resource-ID", r.ID)
}

func TestTypedRouteExplicitStatusAndHeaders(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	Route[struct{}, createdResp](app).POST("/users").To(func(context.Context, struct{}) (createdResp, error) {
		return createdResp{ID: "u-1"}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users", nil))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if got := w.Header().Get("X-Resource-ID"); got != "u-1" {
		t.Fatalf("header = %q, want u-1", got)
	}
}

func TestTypedRouteNoBodyStatus(t *testing.T) {
	type noContentResp struct{}
	func (noContentResp) StatusCode() int { return http.StatusNoContent }

	app := New(WithProduces(MIMEJSON))
	Route[struct{}, noContentResp](app).DELETE("/users/{id}").To(func(context.Context, struct{}) (noContentResp, error) {
		return noContentResp{}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/1", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", w.Body.String())
	}
}
```

Run: `go test ./ghttp/ -run 'TestTypedRouteExplicitStatusAndHeaders|TestTypedRouteNoBodyStatus' -count=1`
Expected: 两个均失败（当前恒 200 且写 body）。

- [ ] **Step 2: 定义接口与 builder 字段**

`ghttp/handler.go`：

```go
// StatusCoder lets a typed response explicitly declare its HTTP status.
type StatusCoder interface {
	StatusCode() int
}

// ResponseHeaderWriter lets a typed response explicitly add response headers.
type ResponseHeaderWriter interface {
	WriteResponseHeaders(http.Header)
}
```

`ghttp/builder.go` 的 RouteBuilder 增加：

```go
responseStatus int
responseHeaders []responseHeader

type responseHeader struct {
	name  string
	value string
}

// Status declares the fixed success status for To handlers.
func (b *RouteBuilder[Req, Resp]) Status(code int) *RouteBuilder[Req, Resp] {
	b.ensureMutable()
	b.responseStatus = code
	return b
}

// ResponseHeader declares a fixed response header for To handlers.
func (b *RouteBuilder[Req, Resp]) ResponseHeader(name, value string) *RouteBuilder[Req, Resp] {
	b.ensureMutable()
	b.responseHeaders = append(b.responseHeaders, responseHeader{name: name, value: value})
	return b
}
```

`ghttp/route_definition.go` 增加：

```go
responseStatus  int
responseHeaders []responseHeader
```

并在 `clone()` 中深拷贝 `responseHeaders`。

`registerHandler` 的 definition 构造追加：

```go
definitions = append(definitions, routeDefinition{
	method:          method,
	pattern:         pattern,
	handler:         handler,
	middlewares:     append([]Middleware(nil), b.middlewares...),
	group:           b.target.routeGroup(),
	needsExtractor:  needsExtractor,
	terminal:        terminal,
	responseStatus:  b.responseStatus,
	responseHeaders: append([]responseHeader(nil), b.responseHeaders...),
	doc:             b.doc.clone(),
	reqType:         reflect.TypeFor[Req](),
	respType:        reflect.TypeFor[Resp](),
	consumes:        append([]string(nil), b.consumes...),
	produces:        append([]string(nil), b.produces...),
})
```

`routeDefinition.clone()` 增加：

```go
cloned.responseHeaders = append([]responseHeader(nil), d.responseHeaders...)
```

- [ ] **Step 3: 写响应逻辑**

在 `ghttp/builder.go` 抽一个共享函数：

```go
func responseHasBody(status int) bool {
	return status >= http.StatusOK && status != http.StatusNoContent && status != http.StatusNotModified
}

func (b *RouteBuilder[Req, Resp]) writeTypedResponse(w http.ResponseWriter, r *http.Request, server *Server, resp interface{}) {
	status := b.responseStatus
	if status == 0 {
		status = http.StatusOK
	}
	if sc, ok := resp.(StatusCoder); ok {
		if code := sc.StatusCode(); code != 0 {
			status = code
		}
	}
	if status < http.StatusContinue || status > 599 {
		b.writeError(w, r, http.StatusInternalServerError, Err(http.StatusInternalServerError, "invalid response status"))
		return
	}
	if hw, ok := resp.(ResponseHeaderWriter); ok {
		hw.WriteResponseHeaders(w.Header())
	}
	for _, h := range b.responseHeaders {
		w.Header().Set(h.name, h.value)
	}
	contentType, codec, ok := negotiateRouteCodec(w, r, server, b.produces, b.codecs)
	if !ok {
		return
	}
	if !responseHasBody(status) {
		w.WriteHeader(status)
		return
	}
	if server.envelope != nil {
		server.envelope(w, r, status, resp, nil, contentType, codec)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = codec.Marshal(w, resp)
}
```

`buildHandler`/`buildParamsHandler`/`buildParamsHandlerWithGlobalValidator` 三处的成功分支统一替换为：

```go
writeResponseCookies(w, resp)
b.writeTypedResponse(w, r, server, resp)
```

注意：envelope 路径统一收敛在 `writeTypedResponse` 内；无 body 状态码（204/304/1xx）直接 `WriteHeader`，不调用 envelope，避免自定义 envelope 写出 body。

- [ ] **Step 4: 更新 OpenAPI 状态码**

`ghttp/openapi_compiler.go` 的 typed 分支：

```go
case routeTerminalTyped:
	status := definition.responseStatus
	if status == 0 {
		status = http.StatusOK
	}
	description := http.StatusText(status)
	if definition.doc.Success != nil && definition.doc.Success.Message != "" {
		description = definition.doc.Success.Message
	}
	response := map[string]any{"description": description}
	if definition.respType != nil && len(definition.produces) > 0 {
		content := make(map[string]any, len(definition.produces))
		for _, contentType := range definition.produces {
			content[contentType] = map[string]any{"schema": generateSchema(definition.respType)}
		}
		response["content"] = content
	}
	return map[string]any{strconv.Itoa(status): response}
```

（`StatusCoder` 动态状态无法静态推断，文档标注为“运行时状态，OpenAPI 以 builder `Status` 为准”。）

- [ ] **Step 5: 运行测试**

Run: `go test ./ghttp/ -run 'TestTypedRoute|TestEnvelope|TestOpenAPI' -count=1`
Expected: 通过。

- [ ] **Step 6: 提交（仅当授权）**

```bash
git add ghttp/handler.go ghttp/builder.go ghttp/route_definition.go ghttp/openapi_compiler.go ghttp/builder_test.go ghttp/openapi_test.go
git commit -m "feat(ghttp): typed responses support explicit status and headers"
```

---

### Task 4.1: middleware 路径参数可见性（MatchedParams）

**Files:**
- Modify: `ghttp/server.go`
- Modify: `ghttp/route_mux.go`（如需要保持 extract 单一职责则不动）
- Modify: `ghttp/middleware_test.go`

**Interfaces:**
- Produces: `func (s *Server) MatchedParams(r *http.Request) Params`
- 新增 context key：`matchedParamsContextKey`

- [ ] **Step 1: 写失败测试**

`ghttp/middleware_test.go` 追加：

```go
func TestMiddlewareSeesMatchedPathParams(t *testing.T) {
	app := New(WithProduces(MIMEJSON))
	var got string
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = app.MatchedParams(r).Path("id")
			next.ServeHTTP(w, r)
		})
	})
	Route[Params, struct{ ID string `json:"id"` }](app).GET("/users/{id}").To(func(context.Context, Params) (struct{ ID string `json:"id"` }, error) {
		return struct{ ID string `json:"id"` }{ID: "ok"}, nil
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if got != "42" {
		t.Fatalf("matched id = %q, want 42", got)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
```

Run: `go test ./ghttp/ -run TestMiddlewareSeesMatchedPathParams -count=1`
Expected: 失败（当前 middleware 拿不到路径参数）。

- [ ] **Step 2: 匹配时提取并注入 context**

`ghttp/server.go` 增加：

```go
type matchedParamsContextKey struct{}

func (s *compiledState) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestPath, err := parseRequestPath(r.URL.EscapedPath(), s.strict)
	if err != nil {
		s.badRequest.ServeHTTP(w, r)
		return
	}
	result := s.mux.match(r.Method, requestPath)
	switch result.kind {
	case routeMatchFound:
		if result.route.definition.needsExtractor {
			params, err := result.route.extract(requestPath)
			if err != nil {
				s.badRequest.ServeHTTP(w, r)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), matchedParamsContextKey{}, params))
		}
		result.route.handler.ServeHTTP(w, r)
	case routeMatchMethodNotAllowed:
		w.Header().Set("Allow", strings.Join(result.allow, ", "))
		s.notAllowed.ServeHTTP(w, r)
	default:
		s.notFound.ServeHTTP(w, r)
	}
}
```

`extractorTerminal` 改为优先读 context：

```go
func extractorTerminal(route *compiledRoute) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params, ok := r.Context().Value(matchedParamsContextKey{}).(pathParamList)
		if !ok {
			requestPath, err := parseRequestPath(r.URL.EscapedPath(), route.definition.pattern.strict)
			if err != nil {
				writeError(w, r, serverFromRequest(r), http.StatusBadRequest, err)
				return
			}
			params, err = route.extract(requestPath)
			if err != nil {
				writeError(w, r, serverFromRequest(r), http.StatusBadRequest, err)
				return
			}
		}
		handler, ok := route.definition.handler.(pathParamHandler)
		if !ok {
			writeError(w, r, serverFromRequest(r), http.StatusInternalServerError, Err(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)))
			return
		}
		handler.ServeHTTPWithPathParams(w, r, params)
	})
}
```

- [ ] **Step 3: 实现访问器**

```go
// MatchedParams returns the request's path params plus the lazy request view.
// It is safe to call from any middleware or handler on the request path.
func (s *Server) MatchedParams(r *http.Request) Params {
	if r == nil {
		return Params{}
	}
	if list, ok := r.Context().Value(matchedParamsContextKey{}).(pathParamList); ok {
		return paramsFromRequestWithPathParams(r, s.config, list)
	}
	return paramsFromRequest(r, s.config)
}
```

- [ ] **Step 4: 运行测试与 benchmark 回归**

Run: `go test ./ghttp/ -run 'TestMiddlewareSeesMatchedPathParams|TestRouteBuilder' -count=1`
Run: `go test ./ghttp/ -run '^$' -bench 'BenchmarkProbe' -benchmem -count=3 | tee /tmp/ghttp-bench-task41.txt`
Expected: 测试通过；参数路由的 allocs 增幅 ≤ 1 alloc/op（上下文注入）。

- [ ] **Step 5: 提交（仅当授权）**

```bash
git add ghttp/server.go ghttp/middleware_test.go
git commit -m "feat(ghttp): expose matched path params to middleware"
```

---

## 批次 3：WS/SSE、能力层、OpenAPI、性能（P2）

### Task 5.1: SSE 服务端事件写入规范（多行 data / id / retry / 注释）

**Files:**
- Modify: `ghttp/sse.go`
- Modify: `ghttp/sse_test.go`

**Interfaces:**
- Produces:
  - `func (s *SSEWriter) WriteEventWithID(event, id, data string) error`
  - `func (s *SSEWriter) WriteComment(text string) error`
  - `func (s *SSEWriter) Retry(millis int) error`
  - `WriteEvent` 行为变更：data 含换行时按规范拆成多行 `data:`，event 含 CR/LF 返回错误。

- [ ] **Step 1: 写失败测试**

```go
func TestSSEWriterSplitsMultilineData(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := &SSEWriter{w: rec, flusher: rec}
	if err := writer.WriteEvent("message", "line1\nline2"); err != nil {
		t.Fatal(err)
	}
	got := rec.Body.String()
	want := "event: message\ndata: line1\ndata: line2\n\n"
	if got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestSSEWriterRejectsNewlineInEventName(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := &SSEWriter{w: rec, flusher: rec}
	if err := writer.WriteEvent("bad\nevent", "x"); err == nil {
		t.Fatal("expected error for newline in event name")
	}
}
```

Run: `go test ./ghttp/ -run 'TestSSEWriter' -count=1`
Expected: 第一个失败（当前直接写入含换行的单行 data）。

- [ ] **Step 2: 实现**

```go
func (s *SSEWriter) WriteEvent(event, data string) error {
	if strings.ContainsAny(event, "\r\n") {
		return fmt.Errorf("sse: event name contains newline")
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\n", event); err != nil {
		return err
	}
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if _, err := fmt.Fprintf(s.w, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(s.w, "\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *SSEWriter) WriteEventWithID(event, id, data string) error {
	if strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("sse: event id contains newline")
	}
	if _, err := fmt.Fprintf(s.w, "id: %s\n", id); err != nil {
		return err
	}
	return s.WriteEvent(event, data)
}

func (s *SSEWriter) WriteComment(text string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n", strings.ReplaceAll(text, "\n", " ")); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *SSEWriter) Retry(millis int) error {
	if millis < 0 {
		return fmt.Errorf("sse: retry must be non-negative")
	}
	_, err := fmt.Fprintf(s.w, "retry: %d\n\n", millis)
	if err == nil {
		s.flusher.Flush()
	}
	return err
}
```

- [ ] **Step 3: 运行测试**

Run: `go test ./ghttp/ -run 'TestSSEWriter|TestSSE' -count=1`
Expected: 通过。

- [ ] **Step 4: 提交（仅当授权）**

```bash
git add ghttp/sse.go ghttp/sse_test.go
git commit -m "fix(ghttp): server SSE writes spec-compliant multi-line events"
```

---

### Task 5.2: WebSocket Origin 默认同源 + 显式配置

**Files:**
- Modify: `ghttp/config.go`、`ghttp/websocket.go`
- Modify: `ghttp/websocket_test.go`

**Interfaces:**
- Produces: `func WithWebSocketOriginChecker(check func(*http.Request) bool) ServerOption`
- 默认行为：同源放行、缺 Origin 放行（非浏览器客户端）、跨源拒绝（gorilla 写 403）。

- [ ] **Step 1: 写失败测试**

```go
func TestWebSocketRejectsCrossOriginByDefault(t *testing.T) {
	app := New()
	Route[Params, struct{}](app).GET("/ws").ToWebSocket(func(context.Context, Params, *WebSocketConn) error {
		return nil
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "https://evil.example")
	app.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}
```

Run: `go test ./ghttp/ -run TestWebSocketRejectsCrossOriginByDefault -count=1`
Expected: 失败（当前 CheckOrigin 恒 true，会尝试升级并返回 101 或握手错误而非 403）。

- [ ] **Step 2: 实现**

`ghttp/config.go`：

```go
webSocketCheckOrigin func(*http.Request) bool

// WithWebSocketOriginChecker replaces the default same-origin WebSocket
// origin check. Default: same-origin or missing Origin is allowed.
func WithWebSocketOriginChecker(check func(*http.Request) bool) ServerOption {
	return func(c *Config) {
		c.webSocketCheckOrigin = check
	}
}
```

`ghttp/websocket.go`：

```go
func webSocketSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host
}

func buildWebSocketHandler(s *Server, handler WebSocketHandler) http.Handler {
	return pathParamHandlerFunc(func(w http.ResponseWriter, r *http.Request, routeParams pathParamList) {
		checkOrigin := s.config.webSocketCheckOrigin
		if checkOrigin == nil {
			checkOrigin = webSocketSameOrigin
		}
		upgrader := gwebsocket.Upgrader{CheckOrigin: checkOrigin}
		params := paramsFromRequestWithPathParams(r, s.config, routeParams)
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn := &WebSocketConn{conn: ws}
		defer conn.Close()
		_ = handler(r.Context(), params, conn)
	})
}
```

`WithWebSocketOriginChecker(func(*http.Request) bool { return false })` 可用于完全关闭跨源；README 示例给出。

- [ ] **Step 3: 运行测试**

Run: `go test ./ghttp/ -run 'TestWebSocket' -count=1`
Expected: 通过；同源测试若已有则保持通过。

- [ ] **Step 4: 提交（仅当授权）**

```bash
git add ghttp/config.go ghttp/websocket.go ghttp/websocket_test.go
git commit -m "fix(ghttp): websocket default same-origin check"
```

---

### Task 7.1: slog 适配器（能力接口 + 标准库适配）

**Files:**
- Add: `ghttp/slog.go`、`ghttp/slog_test.go`

**Interfaces:**
- Produces: `func NewSlogLogger(logger *slog.Logger) Logger`
- 不引入 zap；zap 适配留待独立子包。

- [ ] **Step 1: 写失败测试**

```go
func TestSlogLoggerAdapter(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewTextHandler(&buf, nil))
	logger := NewSlogLogger(base)
	logger.InfoContext(context.Background(), "hello", "key", "value")
	if !strings.Contains(buf.String(), "hello") || !strings.Contains(buf.String(), "value") {
		t.Fatalf("log output = %q", buf.String())
	}
}
```

- [ ] **Step 2: 实现**

```go
type SlogLogger struct {
	logger *slog.Logger
}

func NewSlogLogger(logger *slog.Logger) Logger {
	if logger == nil {
		return nil
	}
	return &SlogLogger{logger: logger}
}

func (l *SlogLogger) DebugContext(ctx context.Context, msg string, args ...interface{}) {
	l.logger.DebugContext(ctx, msg, args...)
}
func (l *SlogLogger) InfoContext(ctx context.Context, msg string, args ...interface{}) {
	l.logger.InfoContext(ctx, msg, args...)
}
func (l *SlogLogger) WarnContext(ctx context.Context, msg string, args ...interface{}) {
	l.logger.WarnContext(ctx, msg, args...)
}
func (l *SlogLogger) ErrorContext(ctx context.Context, msg string, args ...interface{}) {
	l.logger.ErrorContext(ctx, msg, args...)
}
```

- [ ] **Step 3: 运行测试**

Run: `go test ./ghttp/ -run TestSlogLoggerAdapter -count=1`
Expected: 通过。

- [ ] **Step 4: 提交（仅当授权）**

```bash
git add ghttp/slog.go ghttp/slog_test.go
git commit -m "feat(ghttp): slog logger adapter"
```

---

### Task 7.2: RBAC 能力接口 + 默认实现

**Files:**
- Add: `ghttp/authz.go`、`ghttp/authz_test.go`

**Interfaces:**
- Produces:
  - `type Authorizer interface { Authorize(ctx context.Context, subject, action, resource string) error }`
  - `type RoleResolver interface { RolesFor(ctx context.Context, subject string) ([]string, error) }`
  - `type PolicyStore interface { Allowed(ctx context.Context, role, action, resource string) (bool, error) }`
  - `func NewRBAC(roles RoleResolver, policies PolicyStore) *RBAC`
  - `func RBACMiddleware(a Authorizer, subject, action, resource func(*http.Request) string) Middleware`

- [ ] **Step 1: 写失败测试**

```go
type staticRoles map[string][]string
func (r staticRoles) RolesFor(_ context.Context, subject string) ([]string, error) {
	return r[subject], nil
}

type staticPolicies map[string]bool
func (p staticPolicies) Allowed(_ context.Context, role, action, resource string) (bool, error) {
	return p[role+"|"+action+"|"+resource], nil
}

func TestRBACAuthorize(t *testing.T) {
	authz := NewRBAC(
		staticRoles{"alice": {"admin"}},
		staticPolicies{"admin|users:read|/users": true},
	)
	if err := authz.Authorize(context.Background(), "alice", "users:read", "/users"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := authz.Authorize(context.Background(), "bob", "users:read", "/users"); err == nil {
		t.Fatal("expected denial for unknown subject")
	}
}
```

- [ ] **Step 2: 实现**

```go
type RBAC struct {
	roles    RoleResolver
	policies PolicyStore
}

func NewRBAC(roles RoleResolver, policies PolicyStore) *RBAC {
	return &RBAC{roles: roles, policies: policies}
}

func (r *RBAC) Authorize(ctx context.Context, subject, action, resource string) error {
	if r == nil || r.roles == nil || r.policies == nil {
		return ErrRBACNotConfigured
	}
	roles, err := r.roles.RolesFor(ctx, subject)
	if err != nil {
		return err
	}
	for _, role := range roles {
		allowed, err := r.policies.Allowed(ctx, role, action, resource)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	return ErrForbidden
}

func RBACMiddleware(a Authorizer, subject, action, resource func(*http.Request) string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := a.Authorize(r.Context(), subject(r), action(r), resource(r)); err != nil {
				writeError(w, r, serverFromRequest(r), http.StatusForbidden, Err(http.StatusForbidden, http.StatusText(http.StatusForbidden)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

`ghttp/error.go` 增加：

```go
ErrRBACNotConfigured = errors.New("rbac not configured")
ErrForbidden         = errors.New("forbidden")
```

- [ ] **Step 3: 运行测试**

Run: `go test ./ghttp/ -run 'TestRBAC' -count=1`
Expected: 通过。

- [ ] **Step 4: 提交（仅当授权）**

```bash
git add ghttp/authz.go ghttp/authz_test.go ghttp/error.go
git commit -m "feat(ghttp): rbac capability interface with default implementation"
```

---

### Task 8.1: OpenAPI 增强（状态码、错误响应、envelope schema、servers/security）

**Files:**
- Modify: `ghttp/openapi_compiler.go`、`ghttp/config.go`
- Modify: `ghttp/openapi_compiler_test.go`

**Interfaces:**
- Produces:
  - `func WithOpenAPIServers(urls ...string) ServerOption`
  - `func WithOpenAPISecurity(requirements ...map[string][]string) ServerOption`
  - `compileOpenAPI(definitions []routeDefinition, title, version string, envelope bool, servers []string, security []map[string][]string)`（签名变更，包内）
  - `openAPIOperationForDefinition(definition routeDefinition, envelope bool)`、`openAPIResponsesForDefinition(definition routeDefinition, envelope bool)`、`openAPIResponsesForTerminal(definition routeDefinition, envelope bool)`（签名变更，包内）

- [ ] **Step 1: 写失败测试**

```go
type openAPIResp struct {
	ID string `json:"id"`
}

func (r openAPIResp) StatusCode() int { return http.StatusCreated }

func TestOpenAPIEnvelopeAndErrorResponses(t *testing.T) {
	app := New(WithProduces(MIMEJSON), WithOpenAPI("t", "v1"), WithEnvelope(DefaultEnvelope))
	Route[Params, openAPIResp](app).POST("/users").Status(http.StatusCreated).To(func(context.Context, Params) (openAPIResp, error) {
		return openAPIResp{ID: "u-1"}, nil
	})
	doc, err := app.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	if !strings.Contains(text, `"201"`) {
		t.Fatalf("missing 201 response: %s", text)
	}
	if !strings.Contains(text, `"data"`) || !strings.Contains(text, `"code"`) {
		t.Fatalf("envelope schema missing: %s", text)
	}
	if !strings.Contains(text, `"404"`) || !strings.Contains(text, `"405"`) {
		t.Fatalf("error responses missing: %s", text)
	}
}
```

- [ ] **Step 2: 实现**

`ghttp/openapi_compiler.go`：

```go
func (s *Server) OpenAPI() ([]byte, error) {
	if !s.config.openAPIEnabled {
		return nil, ErrOpenAPIDisabled
	}
	return compileOpenAPI(
		s.registry.snapshot(),
		s.config.openAPITitle,
		s.config.openAPIVersion,
		s.envelope != nil,
		s.config.openAPIServers,
		s.config.openAPISecurity,
	)
}
```

`compileOpenAPI` 增加 `envelope bool` 参数，函数体改为：

```go
operation := openAPIOperationForDefinition(definition, envelope)
```

并将签名链改为：

```go
func openAPIOperationForDefinition(definition routeDefinition, envelope bool) map[string]any {
	op := make(map[string]any)
	// summary/description/operationId/tags/parameters/requestBody 等原有填充保持不动
	responses := openAPIResponsesForDefinition(definition, envelope)
	op["responses"] = responses
	if definition.terminal == routeTerminalWebSocket {
		op["x-ghttp-websocket"] = true
	}
	return op
}

func openAPIResponsesForDefinition(definition routeDefinition, envelope bool) map[string]any {
	if definition.method == http.MethodHead {
		return openAPIHeadResponses(openAPIResponsesForTerminal(definition, envelope))
	}
	return openAPIResponsesForTerminal(definition, envelope)
}
```

文档根增加：

```go
if len(servers) > 0 {
	serverItems := make([]any, 0, len(servers))
	for _, url := range servers {
		serverItems = append(serverItems, map[string]any{"url": url})
	}
	document["servers"] = serverItems
}
if len(security) > 0 {
	document["security"] = security
}
```

typed 分支的 schema 包装：

```go
schema := generateSchema(definition.respType)
if envelope {
	schema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{"type": "integer"},
			"msg":  map[string]any{"type": "string"},
			"data": schema,
		},
	}
}
```

typed 分支增加 404/405：

```go
responses := map[string]any{
	strconv.Itoa(status): response,
	"404": openAPIErrorResponse("Not Found"),
	"405": openAPIErrorResponse("Method Not Allowed"),
}
```

新增助手：

```go
func openAPIErrorResponse(description string) map[string]any {
	return map[string]any{
		"description": description,
		"content": map[string]any{
			MIMEJSON: map[string]any{
				"schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"code":    map[string]any{"type": "integer"},
						"message": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}
```

`ghttp/config.go` 增加字段与选项：

```go
openAPIServers  []string
openAPISecurity []map[string][]string

// WithOpenAPIServers sets the OpenAPI servers list.
func WithOpenAPIServers(urls ...string) ServerOption {
	return func(c *Config) {
		c.openAPIServers = append([]string(nil), urls...)
	}
}

// WithOpenAPISecurity sets the OpenAPI top-level security requirements.
func WithOpenAPISecurity(requirements ...map[string][]string) ServerOption {
	return func(c *Config) {
		c.openAPISecurity = append([]map[string][]string(nil), requirements...)
	}
}
```

`compileOpenAPI` 的 servers/security 数据由 `Server.OpenAPI` 经参数传入（签名改为 `compileOpenAPI(definitions, title, version string, envelope bool, servers []string, security []map[string][]string)`），避免在 compiler 中访问 Config。

- [ ] **Step 3: 运行测试**

Run: `go test ./ghttp/ -run 'TestOpenAPI' -count=1`
Expected: 既有测试适配新签名后全部通过。

- [ ] **Step 4: 提交（仅当授权）**

```bash
git add ghttp/openapi_compiler.go ghttp/config.go ghttp/openapi_compiler_test.go
git commit -m "feat(ghttp): openapi status, errors, envelope schema, servers and security"
```

---

### Task 9.1: 性能预算与首批已知优化

**Files:**
- Modify: `ghttp/writer.go`
- Modify: `ghttp/server.go`
- Modify: `ghttp/perf_probe_test.go`

**Interfaces:**
- 行为不变；`newResponseWriteState` 返回单一类型，内部按需探测 Flush/Hijack/Push。

- [ ] **Step 1: 记录基线**

Run: `go test ./ghttp/ -run '^$' -bench 'BenchmarkProbe' -benchmem -count=5 | tee /tmp/ghttp-bench-baseline.txt`
Expected: 记录静态/参数全链路的 ns/op、B/op、allocs/op（当前约 26/10 allocs）。

- [ ] **Step 2: 写分配预算回归测试**

`ghttp/perf_probe_test.go` 追加：

```go
func TestFullChainAllocBudget(t *testing.T) {
	server := New(WithProduces(MIMEJSON))
	Route[Params, struct{ ID string `json:"id"` }](server).GET("/users/{id}").To(func(context.Context, Params) (struct{ ID string `json:"id"` }, error) {
		return struct{ ID string `json:"id"` }{ID: "42"}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	allocs := testing.AllocsPerRun(1000, func() {
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
	})
	if allocs > 15 {
		t.Fatalf("allocs/op = %.1f, want <= 15 (final budget 8 after WS9 iterations)", allocs)
	}
}
```

- [ ] **Step 3: 合并 request context**

`ghttp/server.go` 的 `ServeHTTP`：

```go
ctx := context.WithValue(r.Context(), serverContextKey{}, s)
ctx = context.WithValue(ctx, responseStateContextKey{}, responseState)
r = r.WithContext(ctx)
```

替换原来的两次 `WithContext`，省一次 context 分配。

- [ ] **Step 4: 单一 ResponseWriter 包装**

`ghttp/writer.go` 删除 7 个组合包装类型，`newResponseWriteState` 恒返回 `state`，并在 `responseWriteState` 上直接实现三个可选接口：

```go
func (w *responseWriteState) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *responseWriteState) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, rw, err := h.Hijack()
	if err == nil {
		w.mu.Lock()
		w.hijacked = true
		w.mu.Unlock()
	}
	return conn, rw, err
}

func (w *responseWriteState) Push(target string, options *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, options)
	}
	return http.ErrNotSupported
}
```

`flush()` 内部改为直接调用 `w.Flush()`。

- [ ] **Step 5: 重跑基线**

Run: `go test ./ghttp/ -run '^$' -bench 'BenchmarkProbe' -benchmem -count=5 | tee /tmp/ghttp-bench-after.txt`
Expected: 静态链路 allocs 从 26 下降；若仍 > 12，`TestFullChainAllocBudget` 会失败并列出剩余分配点，下一轮据此继续消除（保留 profile 证据）。

- [ ] **Step 6: 提交（仅当授权）**

```bash
git add ghttp/writer.go ghttp/server.go ghttp/perf_probe_test.go
git commit -m "perf(ghttp): reduce full-chain allocations with single response writer"
```

---

## 验证与门禁

- 每批完成后：`gofmt -l ghttp/` 无输出；`go test ./ghttp/ -count=1` 全绿。
- 全部完成后：`make check`、`go test ./...`、`go test -race ./ghttp/...`。
- HTTP 语义任务必须有 httptest 真实请求断言（已内置于各 Task Step 1）。
- 性能任务必须保留 `/tmp/ghttp-bench-*.txt` 对比；未达到预算时不得声称完成。
- 破坏性变更（EnvelopeFunc 签名、validator 默认值）需在 README 与迁移说明中标注。

## 已确认决策（2026-08-06 review 确认）

1. 406 默认行为：**已被 `docs/superpowers/specs/2026-08-06-ghttp-negotiation-form-problem-design.md` 取代**——默认 406，`WithLenientContentNegotiation()` 显式宽松；`WithStrictContentNegotiation()` 保留为兼容别名。
2. 请求 Content-Type 默认：**已被上述设计取代**——显式未知类型默认 415，缺失类型按 JSON；`WithLenientContentType()` 显式宽松，`WithStrictContentType()` 保留为兼容别名。
3. server 级 validator：**默认关闭**，需 `WithValidator` 显式启用，按 Task 2.2 实施（标注破坏性）。
4. 类型化状态码：**接口（`StatusCoder`/`ResponseHeaderWriter`）+ builder 固定值（`.Status()`/`.ResponseHeader()`）**，不引入 tag 反射，按 Task 3.1 实施。
5. WebSocket 默认策略：**同源放行、缺 Origin 放行、跨源 403**，用户未提出异议，按 Task 5.2 实施。
