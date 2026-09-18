# Model HTTP composition

[中文](README.md)

New(protocol, transport) combines independent model protocols and HTTP transport. Generate validates common parameters, then encodes, sends and decodes. Protocol implementations own logical-to-vendor model mapping, separate vendor configuration, authentication, content conversion, status errors, usage and finish reasons.

No default JSON gateway protocol or vendor implementation is provided. Transport does not retry and decode failures do not resend requests. Non-HTTP backends can implement model.Client directly.
