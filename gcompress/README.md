# gcompress

压缩工具：Gzip、Zlib、Flate、Zip、Tar、Tgz。

## 用法

```go
import "github.com/sofiworker/gk/gcompress"

compressed, _ := gcompress.CompressString("data")
```

字节/字符串助手：

- `Compress` / `Decompress`（gzip）
- `ZlibCompress` / `ZlibDecompress`
- `FlateCompress` / `FlateDecompress`
- `TarGzCompress` / `TarGzDecompress`、`ZipCompress` / `ZipDecompress`
