package gcrypt

import (
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"fmt"
)

// GenerateDESKey 生成DES密钥（8字节）
func GenerateDESKey() ([]byte, error) {
	key := make([]byte, 8)
	_, err := rand.Read(key)
	if err != nil {
		return nil, fmt.Errorf("生成随机密钥失败: %v", err)
	}
	return key, nil
}

// DESEncrypt DES加密
func DESEncrypt(plaintext, key []byte) ([]byte, error) {
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// 添加填充
	plaintext = pkcs7Padding(plaintext, block.BlockSize())

	// 创建加密器
	ciphertext := make([]byte, des.BlockSize+len(plaintext))
	iv := ciphertext[:des.BlockSize]
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext[des.BlockSize:], plaintext)

	return ciphertext, nil
}

// DESDecrypt DES解密
func DESDecrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < des.BlockSize {
		return nil, fmt.Errorf("密文太短")
	}

	iv := ciphertext[:des.BlockSize]
	ciphertext = ciphertext[des.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	// 去除填充
	ciphertext, err = pkcs7Unpadding(ciphertext)
	if err != nil {
		return nil, err
	}

	return ciphertext, nil
}

// GenerateTripleDESKey 生成3DES密钥（24字节）
func GenerateTripleDESKey() ([]byte, error) {
	key := make([]byte, 24)
	_, err := rand.Read(key)
	if err != nil {
		return nil, fmt.Errorf("生成随机密钥失败: %v", err)
	}
	return key, nil
}

// TripleDESEncrypt 3DES加密
func TripleDESEncrypt(plaintext, key []byte) ([]byte, error) {
	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return nil, err
	}

	// 添加填充
	plaintext = pkcs7Padding(plaintext, block.BlockSize())

	// 创建加密器
	ciphertext := make([]byte, des.BlockSize+len(plaintext))
	iv := ciphertext[:des.BlockSize]
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext[des.BlockSize:], plaintext)

	return ciphertext, nil
}

// TripleDESDecrypt 3DES解密
func TripleDESDecrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < des.BlockSize {
		return nil, fmt.Errorf("密文太短")
	}

	iv := ciphertext[:des.BlockSize]
	ciphertext = ciphertext[des.BlockSize:]

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	// 去除填充
	ciphertext, err = pkcs7Unpadding(ciphertext)
	if err != nil {
		return nil, err
	}

	return ciphertext, nil
}
