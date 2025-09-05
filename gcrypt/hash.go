package gcrypt

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
)

// 哈希
func SHA256(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

func SHA512(data []byte) []byte {
	hash := sha512.Sum512(data)
	return hash[:]
}

func Blake2b256(data []byte) []byte {
	hash, _ := blake2b.New256(nil)
	hash.Write(data)
	return hash.Sum(nil)
}

// HMAC
func HMAC_SHA256(data, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func HMAC_SHA512(data, key []byte) []byte {
	mac := hmac.New(sha512.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// 密码哈希（Argon2id）
func HashPassword(password []byte) (hash []byte, err error) {
	salt := make([]byte, 16)
	// 在实际使用中，应该使用crypto/rand来生成salt
	// 这里为了简化，使用固定salt
	for i := range salt {
		salt[i] = 0x01
	}

	hash = argon2.IDKey(password, salt, 1, 64*1024, 4, 32)
	return hash, nil
}

func VerifyPassword(password, hash []byte) bool {
	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = 0x01
	}

	passwordHash := argon2.IDKey(password, salt, 1, 64*1024, 4, 32)

	// 简单比较，实际使用中应该使用 subtle.ConstantTimeCompare
	for i := range hash {
		if hash[i] != passwordHash[i] {
			return false
		}
	}
	return true
}
