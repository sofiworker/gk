# Model context

[中文](README.md)

Pre-v1, not for production. This first implementation validates request structure, groups complete tool interactions, checks budgets and records input digests. It never deletes history; summaries are separate derived records.

Construct New(counter, resolver), inject it with agent.WithContextBuilder, and pass agent options through runtime.WithAgentOptions. Agent.ContextPolicy configures hard input limits and soft thresholds. See the [new API examples](../examples/README.en.md); these interfaces await implementation.

Counter measures the complete model.Request, including instructions, tools, output schemas, media and protocol overhead. It must report exact or estimated counts and a counter ID/version. No vendor tokenizer is included. The example's JSON byte counter is only a demonstration, not a real model window estimator.

BudgetResolver resolves limits per logical model; FixedBudget rejects model switches. SharedWindow describes combined input/output capacity; InputTokens is a separate input limit. Safety margins reduce known model capacity; the Agent limit may further reduce it. Explicit MaxOutputTokens overrides OutputReserve; otherwise the reserve is materialized in the request. Unknown input or output budgets return ErrBudgetUnknown.

Build returns a copied request, balanced half-open message groups, digest, measurement and effective limits. System instructions, the latest user message and the last complete group are required. Soft limits set NeedsCompaction; hard overflow returns ErrOverflow before model dispatch. Estimated counts do not guarantee upstream acceptance. Material resolution and explicit compaction APIs remain unimplemented. Builders currently cannot rewrite messages, tools or existing parameters.

Runner rebuilds before every model call, including calls following tools. Runtime atomically commits ContextSnapshot with the running ModelCall. Stored snapshots cannot be rewritten. Snapshots contain message IDs, agent/model identity, a SHA-256 digest of the SDK request JSON, measurement and budgets. They are neither full request backups nor vendor wire digests.

Compatibility: without a Builder and with zero ContextPolicy, Inspect only checks structure and records unknown measurement without window guarantees. A nonzero policy requires a Builder. Strict budgets are not yet the default.

## Automatic summaries

NewCompactor accepts a separate summary client, budgeted Builder, logical model, output cap and timeout. Inject agent.WithCompactor and set Agent.ContextPolicy.MaxCompactions per Run. Summary calls have no tools and do not invoke Agent recursively.

Before generation, soft pressure or hard overflow selects the oldest contiguous non-required complete groups. The summary request must fit its own budget. Only a nonempty, normally completed summary producing a smaller request within the hard budget can activate. At most one attempt is made per model step, subject to the per-Run cap. Soft failures may use the original legal request; hard failures stop generation.

CompactionRecord belongs to the initiating Turn but can cover earlier turns. It records exact message:/summary: sources, summary text, model call and usage, input digest and measurements. Runtime saves intent before summarizing and saves activation before using the new view. Subsequent turns replay applied records without changing source messages. Final records are immutable.

This supports synchronous automatic compaction during Run only. Idle Compact, chunked summarization, cross-process recovery and long-term memory are not provided. Oversized summary inputs fail rather than silently truncate. Preparation failures do not yet create separate attempt records. Direct Runner calls without Checkpoint only return records in Result; durable boundaries require Runtime/Store.
