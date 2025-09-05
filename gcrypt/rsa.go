package gcrypt

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// GenerateRSAKeyPair 生成RSA密钥对
func GenerateRSAKeyPair(bits int) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, nil, err
	}
	return privateKey, &privateKey.PublicKey, nil
}

// RSAEncrypt RSA加密 (使用PKCS1v15)
func RSAEncrypt(plaintext []byte, publicKey *rsa.PublicKey) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, publicKey, plaintext)
}

// RSADecrypt RSA解密 (使用PKCS1v15)
func RSADecrypt(ciphertext []byte, privateKey *rsa.PrivateKey) ([]byte, error) {
	return rsa.DecryptPKCS1v15(rand.Reader, privateKey, ciphertext)
}

// RSAEncryptOAEP RSA加密 (使用OAEP)
func RSAEncryptOAEP(plaintext []byte, publicKey *rsa.PublicKey) ([]byte, error) {
	return rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, plaintext, nil)
}

// RSADecryptOAEP RSA解密 (使用OAEP)
func RSADecryptOAEP(ciphertext []byte, privateKey *rsa.PrivateKey) ([]byte, error) {
	return rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, ciphertext, nil)
}

// SignWithRSA RSA签名 (使用PKCS1v15)
func SignWithRSA(data []byte, privateKey *rsa.PrivateKey) ([]byte, error) {
	hashed := sha256.Sum256(data)
	return rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
}

// VerifyWithRSA RSA验证签名 (使用PKCS1v15)
func VerifyWithRSA(data, signature []byte, publicKey *rsa.PublicKey) error {
	hashed := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hashed[:], signature)
}

// SignWithRSAPSS RSA签名 (使用PSS)
func SignWithRSAPSS(data []byte, privateKey *rsa.PrivateKey) ([]byte, error) {
	hashed := sha256.Sum256(data)
	return rsa.SignPSS(rand.Reader, privateKey, crypto.SHA256, hashed[:], nil)
}

// VerifyWithRSAPSS RSA验证签名 (使用PSS)
func VerifyWithRSAPSS(data, signature []byte, publicKey *rsa.PublicKey) error {
	hashed := sha256.Sum256(data)
	return rsa.VerifyPSS(publicKey, crypto.SHA256, hashed[:], signature, nil)
}

// EncodePrivateKeyToPEM 将私钥编码为PEM格式
func EncodePrivateKeyToPEM(privateKey *rsa.PrivateKey) []byte {
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})
	return privateKeyPEM
}

// EncodePublicKeyToPEM 将公钥编码为PEM格式
func EncodePublicKeyToPEM(publicKey *rsa.PublicKey) ([]byte, error) {
	if publicKey == nil {
		return nil, errors.New("publicKey is nil")
	}

	publicKeyBytes := x509.MarshalPKCS1PublicKey(publicKey)

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	return publicKeyPEM, nil
}

// DecodePrivateKeyFromPEM 从PEM格式解码私钥
func DecodePrivateKeyFromPEM(privateKeyPEM []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("无法解码PEM块")
	}

	// 尝试解析PKCS8格式
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("PEM块不是RSA私钥")
	}

	// 回退到PKCS1格式
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// DecodePublicKeyFromPEM 从PEM格式解码公钥
func DecodePublicKeyFromPEM(publicKeyPEM []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("无法解码PEM块")
	}

	// 尝试解析PKIX格式
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("PEM块不是RSA公钥")
	}

	// 回退到PKCS1格式
	return x509.ParsePKCS1PublicKey(block.Bytes)
}
