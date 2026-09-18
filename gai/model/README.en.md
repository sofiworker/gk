# Model contracts

[中文](README.md)

Pre-v1, not for production. Info describes logical model capabilities; Parameters describes generation settings; Request/Response describes calls; Catalog optionally resolves metadata. Pointers distinguish omitted settings from explicit zero values.

Agent definitions do not own models. Executors own Client and receive logical ModelSelection per call. Vendor model names, endpoints, credentials, wire fields and vendor options belong to separate protocol implementations. Adapters must reject unsupported options rather than silently drop them.

Message excludes session message IDs and timestamps. Reused core.Content must be explicitly translated into wire content. Responses carry logical model identity, usage and finish reason. Streaming metadata does not imply a streaming API: only Generate is currently implemented.

See [modelhttp](../adapters/modelhttp/README.en.md).

Request.Validate checks roles, payloads, tool declarations, selection and result pairing. Response.Validate checks roles, finish reasons, tools, IDs, usage and structured output. Schema output requires a SchemaValidator; no full schema engine, token estimator or vendor capability inference is built in. Direct Client/modelhttp callers invoke these methods explicitly; Runner invokes them automatically. ClientFunc adapts application functions.
