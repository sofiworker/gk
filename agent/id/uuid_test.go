package id

import (
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewV7(t *testing.T) {
	before := time.Now().UnixMilli()
	value := NewV7()
	after := time.Now().UnixMilli()
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value != strings.ToLower(value) {
		t.Fatal(value)
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	if raw[6]>>4 != 7 || raw[8]>>6 != 2 {
		t.Fatal(value)
	}
	var millis int64
	for _, b := range raw[:6] {
		millis = millis<<8 | int64(b)
	}
	if millis < before || millis > after {
		t.Fatalf("timestamp %d outside [%d, %d]", millis, before, after)
	}
	previous := value
	for range 1000 {
		next := NewV7()
		if next <= previous {
			t.Fatalf("non-increasing IDs: %s %s", previous, next)
		}
		previous = next
	}
}

func TestNewV7ConcurrentUnique(t *testing.T) {
	const workers, count = 16, 1000
	ids := make(chan string, workers*count)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range count {
				ids <- NewV7()
			}
		}()
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]bool)
	for value := range ids {
		if seen[value] {
			t.Fatalf("duplicate UUID %s", value)
		}
		seen[value] = true
	}
}

func TestNextV7Time(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		nano, previous, want int64
	}{
		{"clock", 1_000_000, 0, 4096},
		{"fraction", 1_000_256, 0, 4097},
		{"tie", 1_000_000, 4096, 4097},
		{"rollback", 1_000_000, 8192, 8193},
		{"overflow", 1_000_000, 8191, 8192},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextV7Time(tc.nano, tc.previous); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
