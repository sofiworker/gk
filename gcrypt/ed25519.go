package gcrypt

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
)

// GenerateEd25519Key 生成 Ed25519 密钥对，返回公钥与私钥（种子+公钥）。
func GenerateEd25519Key() (publicKey, privateKey []byte, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	return []byte(pub), []byte(priv), nil
}

// SignWithEd25519 使用 Ed25519 私钥签名。
func SignWithEd25519(privateKey, data []byte) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key length %d", len(privateKey))
	}
	return ed25519.Sign(ed25519.PrivateKey(privateKey), data), nil
}

// VerifyWithEd25519 验证 Ed25519 签名。
func VerifyWithEd25519(publicKey, data, signature []byte) bool {
	if len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(publicKey), data, signature)
}
