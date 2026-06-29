package ghttp

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestJSONCodecRoundTrip(t *testing.T) {
	codec := &JSONCodec{}

	var buf bytes.Buffer
	if err := codec.Marshal(&buf, map[string]string{"key": "value"}); err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !strings.Contains(buf.String(), `"key"`) {
		t.Fatalf("expected JSON with key, got %s", buf.String())
	}

	var result map[string]string
	if err := codec.Unmarshal(&buf, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if result["key"] != "value" {
		t.Fatalf("expected value, got %s", result["key"])
	}
}

func TestCodecManagerResolve(t *testing.T) {
	mgr := NewCodecManager()

	codec, ok := mgr.Resolve("application/json")
	if !ok {
		t.Fatal("expected JSON codec to be registered")
	}
	if codec.ContentTypes()[0] != "application/json" {
		t.Fatalf("expected application/json, got %s", codec.ContentTypes()[0])
	}
}

func TestCodecManagerNegotiate(t *testing.T) {
	mgr := NewCodecManager()

	codec := mgr.Negotiate("application/json")
	if codec.ContentTypes()[0] != "application/json" {
		t.Fatalf("expected JSON, got %s", codec.ContentTypes()[0])
	}

	codec2 := mgr.Negotiate("text/plain")
	if codec2.ContentTypes()[0] != "text/plain" {
		t.Fatalf("expected text/plain, got %s", codec2.ContentTypes()[0])
	}
}

func TestCodecManagerRegisterCustom(t *testing.T) {
	mgr := NewCodecManager()

	custom := &testCodec{ct: "application/custom"}
	if err := mgr.Register(custom); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	codec, ok := mgr.Resolve("application/custom")
	if !ok {
		t.Fatal("expected custom codec to be found")
	}
	if codec.ContentTypes()[0] != "application/custom" {
		t.Fatalf("expected application/custom, got %s", codec.ContentTypes()[0])
	}
}

type testCodec struct {
	ct string
}

func (c *testCodec) ContentTypes() []string                     { return []string{c.ct} }
func (c *testCodec) Marshal(w io.Writer, v interface{}) error   { return nil }
func (c *testCodec) Unmarshal(r io.Reader, v interface{}) error { return nil }
