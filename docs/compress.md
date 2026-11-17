# Compression

**English** | [简体中文](zh-CN/compress.md) | [Documentation](README.md)

[Source](../compress/) · `github.com/sofiworker/keel/compress`

Compression utilities for Gzip, Zlib, Flate, Zip, Tar, and Tgz.

## Usage

```go
import "github.com/sofiworker/keel/compress"

compressed, _ := compress.CompressString("data")
```

Byte and string helpers:

- `Compress` / `Decompress` (gzip)
- `ZlibCompress` / `ZlibDecompress`
- `FlateCompress` / `FlateDecompress`
- `TarGzCompress` / `TarGzDecompress`, `ZipCompress` / `ZipDecompress`
