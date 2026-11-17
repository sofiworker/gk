# Model HTTP adapter

**English** | [简体中文](zh-CN/agent-model-http.md) | [Documentation](README.md)

[Source](../agent/adapters/modelhttp/) · `github.com/sofiworker/keel/agent/adapters/modelhttp`

New(protocol, transport) combines independent model protocols and HTTP transport. Generate validates common parameters, then encodes, sends and decodes. Protocol implementations own logical-to-vendor model mapping, separate vendor configuration, authentication, content conversion, status errors, usage and finish reasons.

No default JSON gateway protocol or vendor implementation is provided. Transport does not retry and decode failures do not resend requests. Non-HTTP backends can implement model.Client directly.
