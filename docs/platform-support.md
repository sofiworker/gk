# Platform support

**English** | [简体中文](zh-CN/platform-support.md) | [Documentation](README.md)

Most utilities use portable Go APIs. Low-level networking selects implementations with
build tags. Compilation alone does not establish availability of every operation.

| Capability | Linux | macOS / BSD | Windows |
| --- | --- | --- | --- |
| HTTP and portable utilities | Go implementations | Go implementations | Go implementations |
| Reactor/readiness engine | epoll | kqueue | Returns `poller.ErrNotSupported` |
| Completion engine | io_uring, subject to kernel support | Unsupported | Unsupported |
| Live raw capture | AF_PACKET | BPF on supported systems | Npcap DLLs |
| pcap/pcapng file processing | Portable | Portable | Portable |
| Network administration | Capability-specific | Capability-specific | Capability-specific |

Check the source build tags before depending on an address, route, link or ethtool operation.
See [Networking](networking.md) and [Poller](poller.md).

## Capture privileges and Npcap

Linux raw sockets and BSD BPF devices require the appropriate privileges or device access.
Windows live capture requires a working Npcap installation and DLLs. On open failure,
the adapter returns the interface name and Npcap diagnostic.
File-based pcap/pcapng processing does not require a capture driver.

## Tests and external resources

Tests with an `integration` tag may need privileges or interfaces; read their prerequisites.
OTLP tests run local HTTP/gRPC receivers, including TLS. Prometheus tests scrape local handlers.
They do not require an external collector. See [Development](development.md) for module checks.
