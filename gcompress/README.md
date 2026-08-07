# gcompress

压缩工具：Gzip、Zlib、Flate、Zip、Tar、Tgz。
Compression utilities for Gzip, Zlib, Flate, Zip, Tar, and Tgz.

## 用法 / Usage

```go
import "github.com/sofiworker/gk/gcompress"

compressed, _ := gcompress.CompressString("data")
```

字节/字符串助手 / byte and string helpers：

- `Compress` / `Decompress`（gzip）
- `ZlibCompress` / `ZlibDecompress`
- `FlateCompress` / `FlateDecompress`
- `TarGzCompress` / `TarGzDecompress`、`ZipCompress` / `ZipDecompress`
