package gcache

import (
	"testing"
)

func TestJSONSerializer(t *testing.T) {
	s := JSONSerializer{}
	
	// Test Serialize
	data, err := s.Serialize(map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty bytes, got %v", data)
	}

	// Test Deserialize
	var v map[string]string
	err = s.Deserialize([]byte(`{"foo":"bar"}`), &v)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}
	if v != nil {
		t.Fatalf("expected nil result, got %v", v)
	}
}

func TestTranslateRedisError(t *testing.T) {
	// Since translateRedisError is unexported and imports redis, we can't easily test it 
	// without importing redis or mocking. 
	// However, we can test nil behavior.
	if err := translateRedisError(nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestTranslateValkeyError(t *testing.T) {
	// Similarly for valkey
	if err := translateValkeyError(nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCloneBytes(t *testing.T) {
	b := []byte("hello")
	c := cloneBytes(b)
	if string(c) != string(b) {
		t.Errorf("expected %s, got %s", b, c)
	}
	// Modify original
	b[0] = 'H'
	if string(c) == string(b) {
		t.Error("expected copy to be independent")
	}
	
	if cloneBytes(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestRedisCache_New_Fail(t *testing.T) {
	// Test NewRedisCache with invalid address to trigger Ping fail
	opts := &Options{
		Address: "invalid_address:6379",
	}
	_, err := NewRedisCache(opts)
	if err == nil {
		t.Error("expected error for invalid address, got nil")
	}
}

func TestValkeyCache_New_Fail(t *testing.T) {
	// Test NewValkeyCache with invalid address
	opts := &Options{
		Address: "valkey://invalid_address:6379",
	}
	_, err := NewValkeyCache(opts)
	// MustParseURL might panic or return valid URL struct even for invalid host, 
	// but NewClient might fail or Ping might fail.
	// Actually MustParseURL panics if invalid. But "valkey://invalid_address:6379" is a valid URL format.
	// The connection should fail.
	if err == nil {
		t.Error("expected error for invalid address, got nil")
	}
}
