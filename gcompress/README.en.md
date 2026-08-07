# gcompress

English | [中文](README.md)

Compression utilities for Gzip, Zlib, Flate, Zip, Tar, and Tgz.

## Usage

```go
import "github.com/sofiworker/gk/gcompress"

compressed, _ := gcompress.CompressString("data")
```

Byte and string helpers:

- `Compress` / `Decompress` (gzip)
- `ZlibCompress` / `ZlibDecompress`
- `FlateCompress` / `FlateDecompress`
- `TarGzCompress` / `TarGzDecompress`, `ZipCompress` / `ZipDecompress`
