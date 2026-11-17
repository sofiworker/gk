# 加解密

[English](../crypt.md) | **简体中文** | [文档目录](README.md)

[源码](../../crypt/) · `github.com/sofiworker/keel/crypt`

加密工具：AES（CBC/GCM）、DES/3DES、RSA（加密/签名/OAEP）、Ed25519（签名/验签）、哈希、HMAC 与密码哈希。

## 用法

```go
import "github.com/sofiworker/keel/crypt"

key, _ := crypt.GenerateAESKey(32)
encrypted, _ := crypt.AESEncrypt(data, key)
```
