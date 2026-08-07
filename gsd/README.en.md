# gsd

English | [中文](README.md)

Service discovery, registration, health checking and load balancing.

## Usage

```go
import "github.com/sofiworker/gk/gsd"
```

Provides etcd registration/discovery, HTTP health checks and multiple load-balancing strategies (round-robin, random, weighted, least connections); retry logic reuses `gretry`.
