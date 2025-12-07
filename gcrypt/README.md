# gcrypt

Cryptographic utilities for AES, DES, RSA, and Hashing.

## Usage

```go
import "github.com/sofiworker/gk/gcrypt"

key, _ := gcrypt.GenerateAESKey(32)
encrypted, _ := gcrypt.AESEncrypt(data, key)
```
