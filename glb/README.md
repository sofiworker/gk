# glb

Generic Load Balancer with multiple strategies (Random, RoundRobin, etc.).

## Usage

```go
import "github.com/sofiworker/gk/glb"

lb := glb.NewLoadBalancer(discovery, glb.NewRandomStrategy())
```
