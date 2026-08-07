# gcrypt

English | [中文](README.md)

Cryptographic utilities: AES (CBC/GCM), DES/3DES, RSA (encrypt/sign/OAEP), Ed25519 (sign/verify), hashing, HMAC and password hashing.

## Usage

```go
import "github.com/sofiworker/gk/gcrypt"

key, _ := gcrypt.GenerateAESKey(32)
encrypted, _ := gcrypt.AESEncrypt(data, key)
```
