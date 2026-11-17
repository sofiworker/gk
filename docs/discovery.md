# Service discovery

**English** | [简体中文](zh-CN/discovery.md) | [Documentation](README.md)

[Source](../sd/) · `github.com/sofiworker/keel/sd`

Service discovery, registration, health checking and load balancing.

## Usage

```go
import "github.com/sofiworker/keel/sd"
```

Provides etcd registration/discovery, HTTP health checks and multiple load-balancing strategies (round-robin, random, weighted, least connections); retry logic reuses `gretry`.

## Snapshot Listeners

Listeners are composable decorators (pattern borrowed from linkerd2's destination listeners) that filter or merge service snapshots before forwarding to the final callback:

- **`NewDedupListener(next)`** — skips snapshots whose address set equals the previous one. Watch jitter or event storms trigger full `Discover` calls and duplicate callbacks; this layer filters redundant notifications.
- **`NewFallbackListener(next)`** — merges a primary and a backup discovery source. Primary wins while it yields available instances; an empty primary falls back to the backup. Publishing starts only after both sources initialized, avoiding a premature empty snapshot.

```go
var instances []sd.ServiceInfo

next := sd.UpdateFunc(func(snapshot []sd.ServiceInfo) {
    instances = snapshot
})

// Primary/backup etcd, or etcd + local cache.
primary, backup := sd.NewFallbackListener(next)
dedup := sd.NewDedupListener(primary)

registry.Watch("api", dedup.Update)          // dedup → primary
fallbackRegistry.Watch("api", backup.Update) // backup
```
