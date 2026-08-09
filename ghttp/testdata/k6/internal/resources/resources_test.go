package resources

import "testing"

func TestSample(t *testing.T) {
	s := Sample()
	if s.MemoryBytes == 0 || s.Goroutines < 1 || s.HeapBytes == 0 {
		t.Fatalf("invalid snapshot: %+v", s)
	}
}

func TestCPUPercentRange(t *testing.T) {
	s := Sample()
	if s.CPUPercent < 0 || s.CPUPercent > 100 {
		t.Fatalf("cpu=%v", s.CPUPercent)
	}
}
