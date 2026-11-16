//go:build windows

package rawcap

import (
	"strings"
	"testing"
)

func TestOpenLiveNpcapError(t *testing.T) {
	if err := loadWpcap(); err != nil {
		t.Skipf("Npcap unavailable: %v", err)
	}

	handle, err := OpenLive("invalid0", Config{})
	if handle != nil {
		if closeErr := handle.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("expected no handle for an invalid interface")
	}
	if err == nil {
		t.Fatal("expected an error for an invalid interface")
	}
	const prefix = "rawcap: pcap_open_live(invalid0) failed: "
	message, ok := strings.CutPrefix(err.Error(), prefix)
	if !ok || strings.TrimSpace(message) == "" {
		t.Fatalf("expected interface context and Npcap diagnostic, got %q", err)
	}
	if strings.ContainsRune(message, '\x00') {
		t.Fatalf("Npcap diagnostic contains a NUL byte: %q", message)
	}
}
