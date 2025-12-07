package gclient

import (
	"bytes"
	"testing"
)

func TestMultiSizeBufferPool(t *testing.T) {
	pool := NewMultiSizeBufferPool(10, 100, 1000)

	// Get exact match
	b1 := pool.Get(10)
	if b1.Cap() < 10 {
		t.Error("cap < 10")
	}
	pool.Put(b1)

	// Get approximate
	b2 := pool.Get(50)
	if b2.Cap() < 100 {
		t.Error("cap < 100")
	}
	pool.Put(b2)

	// Get larger than max
	b3 := pool.Get(2000)
	if b3.Cap() < 2000 {
		t.Error("cap < 2000")
	}
	// Put back (should be discarded or put in largest?)
	// Code: "if capacity > 1024*1024 ... return". 2000 is small.
	// "if len(mbp.pools) > 0 { lastPool ... put }"
	pool.Put(b3)

	// Stats
	stats := pool.Stats()
	if stats["pools"] == nil {
		t.Error("stats missing pools")
	}
}

func TestMultiSizeBufferPoolDefault(t *testing.T) {
	pool := NewMultiSizeBufferPool()
	b := pool.Get(100)
	if b.Cap() < 1024 {
		t.Errorf("expected 1024 (smallest default), got %d", b.Cap())
	}
}

func TestPutNil(t *testing.T) {
	pool := NewMultiSizeBufferPool()
	pool.Put(nil) // Should not panic
}

func TestPutHuge(t *testing.T) {
	pool := NewMultiSizeBufferPool()
	b := bytes.NewBuffer(make([]byte, 2*1024*1024))
	pool.Put(b) // Should be discarded
}
