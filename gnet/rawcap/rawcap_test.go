package rawcap

import (
	"testing"
)

func TestLinuxHandle(t *testing.T) {
	linuxHandle, err := NewHandle()
	if err != nil {
		t.Fatal(err)
	}
	defer linuxHandle.Close()
}
