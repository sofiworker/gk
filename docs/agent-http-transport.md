# Agent HTTP transport

**English** | [简体中文](zh-CN/agent-http-transport.md) | [Documentation](README.md)

[Source](../agent/transport/httptransport/) · `github.com/sofiworker/keel/agent/transport/httptransport`

Handles HTTP methods, URLs, headers, bodies and status codes without model or vendor knowledge. Protocol implementations receive all status codes. Defaults: 60-second timeout, 8 MiB limits on each request and response, no retries, redirects or environment proxy.

WithTransport accepts a standard http.RoundTripper, including http/client Client.RoundTripper() from application composition code. This reuses transport configuration, not ghttp middleware, status classification or retry orchestration. agent does not import ghttp. Injected transports are trusted and may customize proxy behavior.

Responses are buffered; streaming and SSE are not implemented. Close closes idle connections where supported. Callers coordinate shared transport ownership.
