package codec

import (
	"testing"
)

// MockCodec for testing
type MockCodec struct {
	encodeFunc func(v interface{}) ([]byte, error)
	decodeFunc func(data []byte, v interface{}) error
}

func (m *MockCodec) Encode(v interface{}) ([]byte, error) {
	if m.encodeFunc != nil {
		return m.encodeFunc(v)
	}
	return nil, nil
}

func (m *MockCodec) Decode(data []byte, v interface{}) error {
	if m.decodeFunc != nil {
		return m.decodeFunc(data, v)
	}
	return nil
}

func TestManager(t *testing.T) {
	m := NewManager()
	
	// Test Register and Get
	mock := &MockCodec{}
	m.Register("application/json", mock)
	
	if got := m.GetCodec("application/json"); got != mock {
		t.Error("GetCodec failed")
	}
	
	// Test Default
	if got := m.DefaultCodec(); got != mock {
		t.Error("DefaultCodec failed (should be first registered)")
	}

	// Test Case Insensitive and Parameters
	if got := m.GetCodec("APPLICATION/JSON; charset=utf-8"); got != mock {
		t.Error("GetCodec normalization failed")
	}

	// Test SetDefault
	mock2 := &MockCodec{}
	m.SetDefault(mock2)
	if got := m.DefaultCodec(); got != mock2 {
		t.Error("SetDefault failed")
	}

	// Test Register nil
	m.Register("bad", nil)
	// Shouldn't panic, logic checks for nil

	// Test Register empty content type
	m.Register("", mock)
	// Shouldn't register

	// Test Clone
	clone := m.Clone()
	if clone.DefaultCodec() != mock2 {
		t.Error("Clone default codec failed")
	}
	if got := clone.GetCodec("application/json"); got != mock {
		t.Error("Clone map failed")
	}
}

func TestDefaultManager(t *testing.T) {
	dm := DefaultManager()
	if dm == nil {
		t.Fatal("DefaultManager is nil")
	}
	if dm.GetCodec("application/json") == nil {
		t.Error("DefaultManager should have json")
	}
}

func TestWrapBytesCodec(t *testing.T) {
	// gcodec.BytesCodec adapter
	// We can use gcodec.PlainCodec or mock
	// But we need to import gcodec which might cause cycle if gcodec imports codec? 
	// No, codec imports gcodec.
	// We can test wrapBytesCodec behavior via RegisterDefaults which uses it.
	
	m := NewManagerWithDefaults()
	c := m.GetCodec("text/plain")
	if c == nil {
		t.Fatal("text/plain not registered")
	}
	
	// Test Encode
	res, err := c.Encode("test")
	if err != nil {
		t.Fatal(err)
	}
	if string(res) != "test" {
		t.Errorf("Encode failed: %s", res)
	}
	
	// Test Decode
	var out string
	if err := c.Decode([]byte("test"), &out); err != nil {
		t.Fatal(err)
	}
	if out != "test" {
		t.Errorf("Decode failed: %s", out)
	}
}

func TestNormalizeContentType(t *testing.T) {
	tests := []struct {
		in  string
		out string
	}{
		{"application/json", "application/json"},
		{" APPLICATION/JSON ", "application/json"},
		{"application/json; charset=utf-8", "application/json"},
		{"text/html; boundary=something", "text/html"},
	}
	
	for _, tt := range tests {
		if got := normalizeContentType(tt.in); got != tt.out {
			t.Errorf("normalizeContentType(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}
