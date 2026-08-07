package gcrypt

import (
	"bytes"
	"testing"
)

func TestEd25519SignVerify(t *testing.T) {
	pub, priv, err := GenerateEd25519Key()
	if err != nil {
		t.Fatalf("GenerateEd25519Key failed: %v", err)
	}
	data := []byte("signed payload")

	sig, err := SignWithEd25519(priv, data)
	if err != nil {
		t.Fatalf("SignWithEd25519 failed: %v", err)
	}
	if !VerifyWithEd25519(pub, data, sig) {
		t.Fatal("valid signature was rejected")
	}

	tampered := bytes.Clone(data)
	tampered[0] ^= 0xff
	if VerifyWithEd25519(pub, tampered, sig) {
		t.Fatal("tampered data was accepted")
	}
	if VerifyWithEd25519([]byte("short"), data, sig) {
		t.Fatal("invalid public key length was accepted")
	}
}
