package ghttp

import (
	"bytes"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

// BenchmarkFileReply_ServeFile 基准测试文件响应性能。
// BenchmarkFileReply_ServeFile benchmarks file response performance.
func BenchmarkFileReply_ServeFile(b *testing.B) {
	content := bytes.Repeat([]byte("test content "), 1024) // ~12KB
	reader := bytes.NewReader(content)

	fileReply := FileReply{
		Reader:      bytes.NewReader(content),
		Name:        "test.txt",
		ContentType: "text/plain",
		Size:        int64(len(content)),
		ModTime:     time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader.Seek(0, 0)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/file", nil)

		resp := &Response{Writer: rec}
		ghttpReq := &Request{Raw: req}

		_ = fileReply.ServeFile(resp, ghttpReq)
	}
}

// BenchmarkStreamReply_Stream 基准测试流式响应性能。
// BenchmarkStreamReply_Stream benchmarks streaming response performance.
func BenchmarkStreamReply_Stream(b *testing.B) {
	data := []byte("data: test event\n\n")

	streamReply := StreamReply{
		ContentType: "text/event-stream",
		Writer: func(w io.Writer) error {
			_, err := w.Write(data)
			return err
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		resp := &Response{Writer: rec}

		_ = streamReply.Stream(resp)
	}
}

// BenchmarkRedirectReply_Redirect 基准测试重定向性能。
// BenchmarkRedirectReply_Redirect benchmarks redirect performance.
func BenchmarkRedirectReply_Redirect(b *testing.B) {
	redirectReply := RedirectReply{
		URL:    "/new-location",
		Status: 302,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/old", nil)

		resp := &Response{Writer: rec}
		ghttpReq := &Request{Raw: req}

		redirectReply.Redirect(resp, ghttpReq)
	}
}
