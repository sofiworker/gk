# 压缩

[English](../compress.md) | **简体中文** | [文档目录](README.md)

[源码](../../compress/) · `github.com/sofiworker/keel/compress`

压缩工具：Gzip、Zlib、Flate、Zip、Tar、Tgz。

## 用法

```go
import "github.com/sofiworker/keel/compress"

compressed, _ := compress.CompressString("data")
```

字节/字符串助手：

- `Compress` / `Decompress`（gzip）
- `ZlibCompress` / `ZlibDecompress`
- `FlateCompress` / `FlateDecompress`
- `TarGzCompress` / `TarGzDecompress`、`ZipCompress` / `ZipDecompress`
