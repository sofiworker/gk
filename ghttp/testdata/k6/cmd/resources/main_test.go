package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunJSON(t *testing.T) {
	if run(nil) != 0 {
		t.Fatal()
	}
}
func TestRunInvalid(t *testing.T) {
	if run([]string{"-bad"}) != 2 {
		t.Fatal()
	}
}
func TestRunOutput(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x")
	if run([]string{"-output", p}) != 0 {
		t.Fatal()
	}
	if _, e := os.Stat(p); e != nil {
		t.Fatal(e)
	}
}
