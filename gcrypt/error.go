package gcrypt

import (
	"fmt"
)

var (
	ErrInvalidKey = fmt.Errorf("密钥长度必须是 16, 24 或 32 字节")
)
