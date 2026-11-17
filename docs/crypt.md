# Cryptography

**English** | [简体中文](zh-CN/crypt.md) | [Documentation](README.md)

[Source](../crypt/) · `github.com/sofiworker/keel/crypt`

Cryptographic utilities: AES (CBC/GCM), DES/3DES, RSA (encrypt/sign/OAEP), Ed25519 (sign/verify), hashing, HMAC and password hashing.

## Usage

```go
import "github.com/sofiworker/keel/crypt"

key, _ := crypt.GenerateAESKey(32)
encrypted, _ := crypt.AESEncrypt(data, key)
```
