package ghttp

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestJSONCodec_RoundTrip 验证内置 JSONCodec 的解码与编码两侧。
func TestJSONCodec_RoundTrip(t *testing.T) {
	c := JSONCodec()
	if c.ContentType() != "application/json" {
		t.Fatalf("ContentType = %q, want application/json", c.ContentType())
	}

	// 解码
	type in struct {
		Name string `json:"name"`
	}
	req := &Request{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"alice"}`))}
	var got in
	if err := c.Decode(req, &got); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got.Name != "alice" {
		t.Errorf("decoded Name = %q, want alice", got.Name)
	}

	// 编码
	rec := httptest.NewRecorder()
	resp := &Response{ResponseWriter: rec}
	if err := c.Encode(resp, in{Name: "bob"}); err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"name":"bob"`) {
		t.Errorf("encoded body = %q", rec.Body.String())
	}
}

// TestJSONCodec_EmptyBody 验证空 body 返回输入错误。
func TestJSONCodec_EmptyBody(t *testing.T) {
	c := JSONCodec()
	req := &Request{Request: httptest.NewRequest(http.MethodGet, "/", nil)}
	req.Body = nil
	var v map[string]any
	if err := c.Decode(req, &v); err == nil {
		t.Error("expected error for nil body, got nil")
	}
}

// TestXMLCodec_RoundTrip 验证内置 XMLCodec 的解码与编码两侧。
func TestXMLCodec_RoundTrip(t *testing.T) {
	c := XMLCodec()
	if c.ContentType() != "application/xml" {
		t.Fatalf("ContentType = %q, want application/xml", c.ContentType())
	}

	type in struct {
		XMLName xml.Name `xml:"item"`
		Name    string   `xml:"name"`
	}
	req := &Request{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`<item><name>alice</name></item>`))}
	var got in
	if err := c.Decode(req, &got); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got.Name != "alice" {
		t.Errorf("decoded Name = %q, want alice", got.Name)
	}

	rec := httptest.NewRecorder()
	resp := &Response{ResponseWriter: rec}
	if err := c.Encode(resp, in{Name: "bob"}); err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	if !strings.Contains(rec.Body.String(), "<name>bob</name>") {
		t.Errorf("encoded body = %q", rec.Body.String())
	}
}

// customDecoder 是包外自定义 RequestDecoder 能被实现的证明(只实现解码一侧)。
type customDecoder struct{ tag string }

func (customDecoder) ContentType() string { return "text/plain" }
func (d customDecoder) Decode(req *Request, v any) error {
	// 把整段 body 当作字符串塞进 *string,验证单侧接口足以工作。
	if p, ok := v.(*string); ok {
		buf := make([]byte, 64)
		n, _ := req.Body.Read(buf)
		*p = d.tag + string(buf[:n])
	}
	return nil
}

// TestRequestDecoder_SingleSide 验证只实现 RequestDecoder 一侧即可用于 Body。
func TestRequestDecoder_SingleSide(t *testing.T) {
	var dec RequestDecoder = customDecoder{tag: "X:"}
	if dec.ContentType() != "text/plain" {
		t.Fatalf("ContentType = %q", dec.ContentType())
	}
	req := &Request{Request: httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))}
	var s string
	if err := dec.Decode(req, &s); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if s != "X:hello" {
		t.Errorf("decoded = %q, want X:hello", s)
	}
}

// TestCodec_ComposesBothSides 验证 JSONCodec 同时满足 RequestDecoder 与 ResponseEncoder。
func TestCodec_ComposesBothSides(t *testing.T) {
	var c Codec = JSONCodec()
	var _ RequestDecoder = c
	var _ ResponseEncoder = c
	_ = context.Background()
}
