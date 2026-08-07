package gcrypt

import (
	"bytes"
	"testing"
)

func TestRandomBytes(t *testing.T) {
	b, err := RandomBytes(32)
	if err != nil {
		t.Fatalf("RandomBytes failed: %v", err)
	}
	if len(b) != 32 {
		t.Fatalf("len = %d, want 32", len(b))
	}
	if _, err := RandomBytes(-1); err == nil {
		t.Fatal("RandomBytes(-1) should fail")
	}
}

func TestAESGCMRoundTrip(t *testing.T) {
	key, err := GenerateAESKey(32)
	if err != nil {
		t.Fatalf("GenerateAESKey failed: %v", err)
	}
	aad := []byte("additional data")
	plaintext := []byte("secret message")

	ciphertext, err := AESGCMEncrypt(plaintext, key, aad)
	if err != nil {
		t.Fatalf("AESGCMEncrypt failed: %v", err)
	}
	decrypted, err := AESGCMDecrypt(ciphertext, key, aad)
	if err != nil {
		t.Fatalf("AESGCMDecrypt failed: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted = %q, want %q", decrypted, plaintext)
	}

	if _, err := AESGCMDecrypt(ciphertext, key, []byte("wrong aad")); err == nil {
		t.Fatal("AESGCMDecrypt with wrong AAD should fail")
	}
}
