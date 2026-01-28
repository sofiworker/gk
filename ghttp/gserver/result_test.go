package gserver

import (
	"bytes"
	"net"
	"net/http"
	"testing"

	"github.com/valyala/fasthttp"
)

var benchAddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}

// ==================== AutoResult Tests ====================

func TestAutoResult_JSON(t *testing.T) {
	type Msg struct {
		Message string `json:"message"`
	}

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	req.Header.Set("Accept", "application/json")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := Auto(Msg{Message: "hello"})
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, gctx.StatusCode())
	}

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "application/json" {
		t.Errorf("expected content-type application/json, got %s", contentType)
	}

	body := gctx.Response().Body()
	if !bytes.Contains(body, []byte(`"message":"hello"`)) {
		t.Errorf("expected body to contain 'message:hello', got %s", string(body))
	}
}

func TestAutoResult_XML(t *testing.T) {
	type Msg struct {
		Message string `xml:"message"`
	}

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	req.Header.Set("Accept", "application/xml")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := Auto(Msg{Message: "hello"})
	result.Execute(gctx)

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "application/xml" {
		t.Errorf("expected content-type application/xml, got %s", contentType)
	}
}

func TestAutoResult_DefaultJSON(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := Auto(map[string]string{"message": "hello"})
	result.Execute(gctx)

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "application/json" {
		t.Errorf("expected content-type application/json, got %s", contentType)
	}
}

func TestAutoResult_WithCode(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := AutoCode(map[string]string{"message": "hello"}, http.StatusCreated)
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, gctx.StatusCode())
	}
}

func TestAutoResult_WithHeader(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := NewAutoResult(map[string]string{"message": "hello"}).WithHeader("X-Custom", "value")
	result.Execute(gctx)

	header := string(gctx.Response().Header.Peek("X-Custom"))
	if header != "value" {
		t.Errorf("expected header X-Custom=value, got %s", header)
	}
}

func TestAutoResult_WithHeaders(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := NewAutoResult(map[string]string{"message": "hello"}).WithHeaders(map[string]string{
		"X-Custom1": "value1",
		"X-Custom2": "value2",
	})
	result.Execute(gctx)

	header1 := string(gctx.Response().Header.Peek("X-Custom1"))
	if header1 != "value1" {
		t.Errorf("expected header X-Custom1=value1, got %s", header1)
	}

	header2 := string(gctx.Response().Header.Peek("X-Custom2"))
	if header2 != "value2" {
		t.Errorf("expected header X-Custom2=value2, got %s", header2)
	}
}

func TestAutoResult_WithMarshal(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := NewAutoResult(map[string]string{"message": "hello"}).WithMarshal(func(data interface{}) ([]byte, string, error) {
		return []byte("custom:"), "text/plain", nil
	})
	result.Execute(gctx)

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "text/plain" {
		t.Errorf("expected content-type text/plain, got %s", contentType)
	}

	body := gctx.Response().Body()
	if string(body) != "custom:" {
		t.Errorf("expected body custom:, got %s", string(body))
	}
}

func TestAutoResult_NilContext(t *testing.T) {
	result := Auto(map[string]string{"message": "hello"})
	result.Execute(nil) // 不应该panic
}

// ==================== JSONResult Tests ====================

func TestJSONResult_Execute(t *testing.T) {
	type Msg struct {
		Message string `json:"message"`
	}

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := JSON(Msg{Message: "hello"})
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, gctx.StatusCode())
	}

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "application/json" {
		t.Errorf("expected content-type application/json, got %s", contentType)
	}

	body := gctx.Response().Body()
	if !bytes.Contains(body, []byte(`"message":"hello"`)) {
		t.Errorf("expected body to contain 'message:hello', got %s", string(body))
	}
}

func TestJSONCode_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := JSONCode(map[string]string{"message": "hello"}, http.StatusCreated)
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, gctx.StatusCode())
	}
}

// ==================== XMLResult Tests ====================

func TestXMLResult_Execute(t *testing.T) {
	type Msg struct {
		Message string `xml:"message"`
	}

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := XML(Msg{Message: "hello"})
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, gctx.StatusCode())
	}

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "application/xml" {
		t.Errorf("expected content-type application/xml, got %s", contentType)
	}
}

// ==================== StringResult Tests ====================

func TestStringResult_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := String("hello %s", "world")
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, gctx.StatusCode())
	}

	body := gctx.Response().Body()
	expected := "hello world"
	if string(body) != expected {
		t.Logf("expected body (len %d): %q", len(expected), expected)
		t.Logf("got body (len %d): %q", len(body), string(body))
		t.Errorf("expected body %s, got %s", expected, string(body))
	}
}

func TestStringCode_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := StringCode("hello", http.StatusCreated)
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, gctx.StatusCode())
	}
}

// ==================== DataResult Tests ====================

func TestDataResult_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := Data("application/octet-stream", []byte("binary data"))
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, gctx.StatusCode())
	}

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "application/octet-stream" {
		t.Errorf("expected content-type application/octet-stream, got %s", contentType)
	}

	body := gctx.Response().Body()
	expected := "binary data"
	if string(body) != expected {
		t.Errorf("expected body %s, got %s", expected, string(body))
	}
}

// ==================== RedirectResult Tests ====================

func TestRedirectResult_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := Redirect("/new-location")
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusFound {
		t.Errorf("expected status %d, got %d", http.StatusFound, gctx.StatusCode())
	}

	location := string(gctx.Response().Header.Peek("Location"))
	if location != "/new-location" {
		t.Errorf("expected location /new-location, got %s", location)
	}
}

func TestRedirectCode_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := RedirectCode("/new-location", http.StatusMovedPermanently)
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusMovedPermanently {
		t.Errorf("expected status %d, got %d", http.StatusMovedPermanently, gctx.StatusCode())
	}
}

// ==================== ErrorResult Tests ====================

func TestErrorResult_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := ErrorMsg("error message")
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, gctx.StatusCode())
	}

	body := gctx.Response().Body()
	expected := "error message"
	if string(body) != expected {
		t.Errorf("expected body %s, got %s", expected, string(body))
	}
}

func TestErrorCode_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := ErrorStatusCode(http.StatusNotFound, "not found")
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, gctx.StatusCode())
	}

	body := gctx.Response().Body()
	expected := "not found"
	if string(body) != expected {
		t.Errorf("expected body %s, got %s", expected, string(body))
	}
}

func TestErrorStatusCode_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := ErrorStatusCode(http.StatusNotFound, "not found")
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, gctx.StatusCode())
	}
}

// ==================== NoContentResult Tests ====================

func TestNoContentResult_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	result := NoContent()
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, gctx.StatusCode())
	}
}

// ==================== StreamResult Tests ====================

func TestStreamResult_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	data := []byte("stream data")
	reader := bytes.NewReader(data)
	result := Stream(reader)
	result.Execute(gctx)

	if gctx.StatusCode() != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, gctx.StatusCode())
	}

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "application/octet-stream" {
		t.Errorf("expected content-type application/octet-stream, got %s", contentType)
	}

	body := gctx.Response().Body()
	expected := "stream data"
	if string(body) != expected {
		t.Errorf("expected body %s, got %s", expected, string(body))
	}
}

func TestStreamWithContentType_Execute(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	data := []byte("stream data")
	reader := bytes.NewReader(data)
	result := StreamWithContentType(reader, "text/plain")
	result.Execute(gctx)

	contentType := string(gctx.Response().Header.Peek("Content-Type"))
	if contentType != "text/plain" {
		t.Errorf("expected content-type text/plain, got %s", contentType)
	}

	body := gctx.Response().Body()
	expected := "stream data"
	if string(body) != expected {
		t.Errorf("expected body %s, got %s", expected, string(body))
	}
}

// ==================== Wrap Tests ====================

func TestWrap(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	type Msg struct {
		Message string `json:"message"`
	}

	handler := Wrap(func(ctx *Context) Result {
		return Auto(Msg{Message: "hello"})
	})
	handler(gctx)

	body := gctx.Response().Body()
	if !bytes.Contains(body, []byte(`"message":"hello"`)) {
		t.Errorf("expected body to contain 'message:hello', got %s", string(body))
	}
}

func TestWrap_NilResult(t *testing.T) {
	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("GET")
	req.SetRequestURI("/test")
	var ctx fasthttp.RequestCtx
	ctx.Init(req, benchAddr, nil)
	gctx := &Context{
		fastCtx: &ctx,
		Writer:  &respWriter{ctx: &ctx},
	}

	handler := Wrap(func(ctx *Context) Result {
		return nil
	})
	handler(gctx) // 不应该panic

	if len(gctx.Response().Body()) > 0 {
		t.Errorf("expected empty body, got %s", string(gctx.Response().Body()))
	}
}

func TestWraps(t *testing.T) {
	handlers := Wraps(
		func(ctx *Context) Result {
			return Auto(map[string]string{"handler": "1"})
		},
		func(ctx *Context) Result {
			return Auto(map[string]string{"handler": "2"})
		},
	)

	if len(handlers) != 2 {
		t.Errorf("expected 2 handlers, got %d", len(handlers))
	}
}

// ==================== 基准测试 ====================

func BenchmarkAutoResult(b *testing.B) {
	benchAddr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	result := Auto(map[string]string{"message": "hello"})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := fasthttp.AcquireRequest()
		req.Header.SetMethod("GET")
		req.SetRequestURI("/test")
		var ctx fasthttp.RequestCtx
		ctx.Init(req, benchAddr, nil)
		gctx := &Context{
			fastCtx: &ctx,
		}
		result.Execute(gctx)
		fasthttp.ReleaseRequest(req)
	}
}

func BenchmarkJSONResult(b *testing.B) {
	benchAddr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	result := JSON(map[string]string{"message": "hello"})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := fasthttp.AcquireRequest()
		req.Header.SetMethod("GET")
		req.SetRequestURI("/test")
		var ctx fasthttp.RequestCtx
		ctx.Init(req, benchAddr, nil)
		gctx := &Context{
			fastCtx: &ctx,
		}
		result.Execute(gctx)
		fasthttp.ReleaseRequest(req)
	}
}

func BenchmarkWrap(b *testing.B) {
	handler := Wrap(func(ctx *Context) Result {
		return Auto(map[string]string{"message": "hello"})
	})
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := fasthttp.AcquireRequest()
		req.Header.SetMethod("GET")
		req.SetRequestURI("/test")
		var ctx fasthttp.RequestCtx
		ctx.Init(req, benchAddr, nil)
		gctx := &Context{
			fastCtx: &ctx,
		}

		handler(gctx)
		fasthttp.ReleaseRequest(req)
	}
}
