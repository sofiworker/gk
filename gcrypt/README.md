# gcrypt

[English](README.en.md) | 中文

加密工具：AES（CBC/GCM）、DES/3DES、RSA（加密/签名/OAEP）、Ed25519（签名/验签）、哈希、HMAC 与密码哈希。

## 用法

```go
import "github.com/sofiworker/gk/gcrypt"

key, _ := gcrypt.GenerateAESKey(32)
encrypted, _ := gcrypt.AESEncrypt(data, key)
```
