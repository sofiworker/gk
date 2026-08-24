package app

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

const outputETag = `"ghttp-k6-sample-v1"`

var outputModifiedTime = time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

func registerOutput(server *ghttp.Server) {
	sample := loadOutputFixture()

	// JSON 输出,missing 查询参数控制是否返回 404。
	// JSON output with a missing query param to control 404.
	mustRaw(server.RawHandle(http.MethodGet, "/output/json", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		missing, _ := strconv.ParseBool(r.URL.Query().Get("missing"))
		if missing {
			writePublicError(w, http.StatusNotFound)
			return
		}
		writeJSON(w, struct {
			Message string            `json:"message"`
			Tags    []string          `json:"tags"`
			Meta    map[string]string `json:"meta"`
		}{Message: "hello", Tags: []string{"ghttp", "k6"}, Meta: map[string]string{"source": "typed"}})
	}))))
	// HEAD 只回头部,不回 body(与 net/http 线级语义一致,httptest 才能观察到空 body)。
	// HEAD returns headers only (matching net/http wire semantics so httptest can
	// observe the empty body).
	mustRaw(server.RawHandle(http.MethodHead, "/output/json", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))))

	// XML 输出:手写 XML 字符串,不依赖 XML 编解码器。
	// XML output: write a literal XML string, no XML codec dependency.
	mustRaw(server.RawHandle(http.MethodGet, "/output/xml", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<output><message>hello</message></output>`))
	}))))

	// 纯文本输出。
	// Plain text output.
	mustRaw(server.RawHandle(http.MethodGet, "/output/text", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello text"))
	}))))

	// 二进制输出。
	// Binary output.
	mustRaw(server.RawHandle(http.MethodGet, "/output/binary", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 'g', 'h', 't', 't', 'p', 0xff})
	}))))

	// 201 Created + Location。
	mustRaw(server.RawHandle(http.MethodPost, "/output/created", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/output/json")
		w.WriteHeader(http.StatusCreated)
	}))))

	// 202 Accepted。
	mustRaw(server.RawHandle(http.MethodPost, "/output/accepted", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))))

	// 204 No Content。
	mustRaw(server.RawHandle(http.MethodDelete, "/output/empty", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))
	// HEAD /output/empty 期望 405 空体。
	// HEAD /output/empty expects 405 with an empty body.
	mustRaw(server.RawHandle(http.MethodHead, "/output/empty", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))))

	// 307 重定向。
	mustRaw(server.RawHandle(http.MethodGet, "/output/redirect", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/output/json", http.StatusTemporaryRedirect)
	}))))

	// 多值响应头。
	mustRaw(server.RawHandle(http.MethodGet, "/output/headers", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Vary", "Accept")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Add("X-Multi", "one")
		w.Header().Add("X-Multi", "two")
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))))

	// 文件服务(testdata 静态文件)。
	mustRaw(server.RawHandle(http.MethodGet, "/files/sample", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="sample.txt"`)
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	}))))
	mustRaw(server.RawHandle(http.MethodGet, "/files/range", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	}))))
	mustRaw(server.RawHandle(http.MethodGet, "/files/etag", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", outputETag)
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	}))))
}

func loadOutputFixture() []byte {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("resolve output fixture source")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "fixtures", "static", "sample.txt"))
	if err != nil {
		panic(err)
	}
	return data
}
