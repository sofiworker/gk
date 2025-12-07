# gcodec

Encoding and Decoding utilities for JSON, XML, YAML, and Plain text.

## Usage

```go
import "github.com/sofiworker/gk/gcodec"

codec := gcodec.NewJSONCodec()
data, _ := codec.EncodeBytes(myStruct)
```
