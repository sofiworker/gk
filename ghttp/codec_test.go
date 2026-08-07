package ghttp

import (
	"bytes"
	"io"
	"net/url"
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

func TestFormCodecUnmarshalStruct(t *testing.T) {
	codec := &FormCodec{}
	type profile struct {
		Name string `form:"name"`
		Age  int    `form:"age"`
		Tag  string `form:"-"`
	}
	var p profile
	if err := codec.Unmarshal(strings.NewReader("name=alice&age=18"), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Name != "alice" || p.Age != 18 || p.Tag != "" {
		t.Fatalf("profile = %#v", p)
	}
}

func TestFormCodecUnmarshalStructWithoutTags(t *testing.T) {
	codec := &FormCodec{}
	type profile struct {
		Name string `json:"name"`
		Age  int
	}
	var p profile
	err := codec.Unmarshal(strings.NewReader("name=alice"), &p)
	if err == nil || !strings.Contains(err.Error(), "no form tags") {
		t.Fatalf("err = %v, want no-form-tags error", err)
	}
}

func TestPlainCodecUnmarshalRejectsUnsupportedTarget(t *testing.T) {
	codec := &PlainCodec{}
	var target struct {
		Name string
	}
	if err := codec.Unmarshal(strings.NewReader("name=alice"), &target); err == nil {
		t.Fatal("expected error for unsupported decode target")
	}
}

func TestCodecManagerNegotiateCacheHitAndInvalidate(t *testing.T) {
	mgr := NewCodecManager()

	// 预热缓存：application/custom 未知，回退到默认；unknown type falls back to default.
	if codec := mgr.Negotiate("application/custom"); codec.ContentTypes()[0] != "application/json" {
		t.Fatalf("expected default JSON fallback, got %s", codec.ContentTypes()[0])
	}
	// 缓存结果用于后续查询；cached result serves repeated lookups.
	if codec := mgr.Negotiate("application/custom"); codec.ContentTypes()[0] != "application/json" {
		t.Fatalf("expected cached JSON fallback, got %s", codec.ContentTypes()[0])
	}

	// 注册该类型 codec 后必须失效缓存回退；registering a codec invalidates the cached fallback.
	if err := mgr.Register(&testCodec{ct: "application/custom"}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if codec := mgr.Negotiate("application/custom"); codec.ContentTypes()[0] != "application/custom" {
		t.Fatalf("expected custom codec after invalidation, got %s", codec.ContentTypes()[0])
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
