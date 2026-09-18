// Package agent 将行为定义、模型客户端和每次调用的工具能力组合为执行循环。
// Package agent combines behavior, a model client and per-call tool capabilities into an execution loop.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	gcontext "github.com/sofiworker/gk/gai/context"
	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/id"
	"github.com/sofiworker/gk/gai/model"
	"github.com/sofiworker/gk/gai/tool"
)

var (
	ErrConfig     = errors.New("invalid agent configuration")
	ErrBudget     = errors.New("agent execution budget exceeded")
	ErrResponse   = model.ErrResponse
	ErrCheckpoint = errors.New("agent checkpoint failed")
)

type config struct {
	compactor      *gcontext.Compactor
	contextBuilder gcontext.Builder
	validator      model.SchemaValidator
	rounds, tools  int
	toolOptions    []tool.Option
}
type Option func(*config) error

func WithCompactor(compactor *gcontext.Compactor) Option {
	return func(c *config) error {
		if compactor == nil {
			return ErrConfig
		}
		c.compactor = compactor
		return nil
	}
}

func WithContextBuilder(builder gcontext.Builder) Option {
	return func(c *config) error {
		if builder == nil {
			return ErrConfig
		}
		c.contextBuilder = builder
		return nil
	}
}

func WithOutputValidator(validator model.SchemaValidator) Option {
	return func(c *config) error {
		if validator == nil {
			return ErrConfig
		}
		c.validator = validator
		return nil
	}
}

func WithLimits(rounds, tools int) Option {
	return func(c *config) error {
		if rounds < 1 || tools < 0 {
			return ErrConfig
		}
		c.rounds = rounds
		c.tools = tools
		return nil
	}
}
func WithToolOptions(options ...tool.Option) Option {
	return func(c *config) error { c.toolOptions = append([]tool.Option(nil), options...); return nil }
}

// Runner 可并发复用，不保存会话或 Sandbox；模型客户端须支持并发。
// Runner is reusable concurrently and stores no session or Sandbox; the model client must support concurrency.
type Runner struct {
	definition core.Agent
	client     model.Client
	tools      *tool.Set
	cfg        config
}

func New(definition core.Agent, client model.Client, options ...Option) (*Runner, error) {
	if definition.ID == "" || client == nil {
		return nil, ErrConfig
	}
	if len(definition.Prompt.Sources) > 0 {
		return nil, fmt.Errorf("prompt sources need a resolver: %w", core.ErrUnsupported)
	}
	cfg := config{rounds: 8, tools: 32}
	for _, o := range options {
		if o == nil {
			return nil, ErrConfig
		}
		if err := o(&cfg); err != nil {
			return nil, err
		}
	}
	tools, err := tool.New(definition.Tools, cfg.toolOptions...)
	if err != nil {
		return nil, err
	}
	if definition.ContextPolicy.MaxInputTokens < 0 || definition.ContextPolicy.SoftInputTokens < 0 {
		return nil, ErrConfig
	}
	if definition.ContextPolicy != (core.ContextPolicy{}) && cfg.contextBuilder == nil {
		return nil, gcontext.ErrBudgetUnknown
	}
	if definition.ContextPolicy.MaxCompactions < 0 || (definition.ContextPolicy.MaxCompactions > 0 && cfg.compactor == nil) {
		return nil, ErrConfig
	}
	definition.Tools = nil
	return &Runner{definition: definition, client: client, tools: tools, cfg: cfg}, nil
}

type Input struct {
	Compactions []core.CompactionRecord
	Model       core.ModelSelection
	Parameters  model.Parameters
	Messages    []core.Message
	Tools       core.ToolContext
	// Capabilities 按调用绑定审计身份；nil 时使用 Tools。
	// Capabilities binds audit identity per call; nil uses Tools.
	Capabilities func(context.Context, core.Call) (core.ToolContext, error)
	// Checkpoint 在外部调用前及结果产生后执行；错误立即中止，不重试副作用。
	// Checkpoint runs before external calls and after results; errors stop execution without replaying effects.
	Checkpoint func(context.Context, Result) error
}

// Result 保留部分执行结果；失败后不应盲目重跑已有副作用的调用。
// Result retains partial execution results; failures must not blindly replay calls with effects.
type Result struct {
	Compactions  []core.CompactionRecord
	Contexts     []core.ContextSnapshot
	Messages     []core.Message
	ModelCalls   []model.Response
	ToolCalls    []tool.Execution
	ModelRecords []core.ModelCall
	ToolRecords  []core.ToolExecution
}

func (r *Runner) Binding() core.Binding {
	return core.Binding{ID: r.definition.ID, Version: r.definition.Version}
}

func checkpoint(ctx context.Context, in Input, result Result) error {
	if in.Checkpoint == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return errors.Join(ErrCheckpoint, err)
	}
	var snapshot Result
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return errors.Join(ErrCheckpoint, err)
	}
	if err = in.Checkpoint(ctx, snapshot); err != nil {
		return errors.Join(ErrCheckpoint, err)
	}
	return nil
}

func failure(err error) *core.Failure {
	if err == nil {
		return nil
	}
	return &core.Failure{Code: "execution_failed", Message: "execution failed"}
}

func (r *Runner) Run(ctx context.Context, in Input) (result Result, err error) {
	if in.Model.ID == "" {
		return result, ErrConfig
	}
	encoded, err := json.Marshal(in.Messages)
	if err != nil {
		return result, err
	}
	var history []core.Message
	if err = json.Unmarshal(encoded, &history); err != nil {
		return result, err
	}
	if err = in.Parameters.Validate(); err != nil {
		return result, err
	}
	if f := in.Parameters.OutputFormat; f != nil && f.Kind == model.FormatSchema && r.cfg.validator == nil {
		return result, model.ErrValidator
	}
	parameterData, err := json.Marshal(in.Parameters)
	if err != nil {
		return result, err
	}
	var parameters model.Parameters
	if err = json.Unmarshal(parameterData, &parameters); err != nil {
		return result, err
	}
	messages := make([]model.Message, 0, len(history)+1)
	sources := make([]string, 0, len(history)+1)
	for _, m := range history {
		messages = append(messages, model.Message{Role: m.Role, Content: m.Content})
		key := m.ID
		if key == "" {
			key = id.NewV7()
		}
		sources = append(sources, "message:"+key)
	}
	definitions := r.tools.Definitions()
	tools := make([]model.Tool, 0, len(definitions))
	for _, d := range definitions {
		tools = append(tools, model.Tool{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema})
	}
	if r.definition.Prompt.System != "" {
		messages = append([]model.Message{{Role: core.RoleSystem, Content: []core.Content{{Kind: core.ContentText, Text: r.definition.Prompt.System}}}}, messages...)
		sources = append([]string{"agent:" + r.definition.ID + ":" + r.definition.Version}, sources...)
	}
	for _, record := range in.Compactions {
		if record.Applied {
			messages, sources = applySummary(messages, sources, record)
		}
	}
	seen := map[string]bool{}
	used := 0
	for round := 0; round < r.cfg.rounds; round++ {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		requestData, e := json.Marshal(model.Request{Model: in.Model, Messages: messages, Tools: tools, Parameters: parameters})
		if e != nil {
			return result, e
		}
		var request model.Request
		if e = json.Unmarshal(requestData, &request); e != nil {
			return result, e
		}
		if e = request.Validate(); e != nil {
			return result, e
		}
		plan, e := gcontext.Inspect(ctx, request)
		if r.cfg.contextBuilder != nil {
			plan, e = r.cfg.contextBuilder.Build(ctx, gcontext.Input{Request: request, Policy: r.definition.ContextPolicy})
		}
		if (e == nil || errors.Is(e, gcontext.ErrOverflow)) && (plan.NeedsCompaction || errors.Is(e, gcontext.ErrOverflow)) && r.cfg.compactor != nil && len(result.Compactions) < r.definition.ContextPolicy.MaxCompactions {
			var nextSources []string
			plan, nextSources, e = r.compact(ctx, in, &result, plan, sources, e)
			if e != nil {
				return result, e
			}
			sources = nextSources
			request = plan.Request
			messages = request.Messages
		}
		if e != nil {
			return result, e
		}
		// 首版 Builder 只检查预算，不允许在缺少覆盖记录时改写输入来源。
		// The initial Builder only checks budgets; rewriting sources requires coverage records.
		expected := request
		if expected.Parameters.MaxOutputTokens == nil {
			expected.Parameters.MaxOutputTokens = plan.Request.Parameters.MaxOutputTokens
		}
		if !reflect.DeepEqual(expected, plan.Request) {
			return result, gcontext.ErrConfig
		}
		verified, e := gcontext.Inspect(ctx, plan.Request)
		if e != nil {
			return result, e
		}
		if plan.Digest != verified.Digest {
			return result, gcontext.ErrConfig
		}
		request = plan.Request
		if e = request.Validate(); e != nil {
			return result, e
		}
		snapshot := core.ContextSnapshot{ID: id.NewV7(), Agent: r.Binding(), Model: request.Model, RequestDigest: plan.Digest, Measurement: plan.Measurement, InputLimit: plan.InputLimit, OutputReserve: plan.OutputReserve, SafetyMargin: plan.SafetyMargin, NeedsCompaction: plan.NeedsCompaction, CreatedAt: time.Now().UnixMilli()}
		snapshot.InputSourceIDs = append([]string(nil), sources...)
		for _, source := range sources {
			if strings.HasPrefix(source, "message:") {
				snapshot.SourceMessageIDs = append(snapshot.SourceMessageIDs, strings.TrimPrefix(source, "message:"))
			}
		}
		result.Contexts = append(result.Contexts, snapshot)
		record := core.ModelCall{ContextSnapshotID: snapshot.ID, ID: id.NewV7(), RequestedModel: in.Model, Status: core.CallRunning, StartedAt: time.Now().UnixMilli()}
		result.ModelRecords = append(result.ModelRecords, record)
		if err = checkpoint(ctx, in, result); err != nil {
			return result, err
		}
		response, e := r.client.Generate(ctx, request)
		if e == nil {
			e = response.Validate(ctx, request, r.cfg.validator)
		}
		ended := time.Now().UnixMilli()
		record.EndedAt = &ended
		record.ActualModel, record.RequestID, record.Usage = response.ActualModel, response.RequestID, response.Usage
		record.FinishReason = response.FinishReason
		record.Status = core.CallCompleted
		if e == nil {
			e = validateResponse(response, seen)
		}
		if e != nil {
			record.Status, record.Error = core.CallFailed, failure(e)
			if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
				record.Status = core.CallCancelled
			}
			result.ModelRecords[len(result.ModelRecords)-1] = record
			return result, e
		}
		result.ModelCalls = append(result.ModelCalls, response)
		message := core.Message{ID: id.NewV7(), CreatedAt: ended, Role: response.Message.Role, Content: response.Message.Content}
		result.Messages = append(result.Messages, message)
		record.OutputMessageIDs = []string{message.ID}
		result.ModelRecords[len(result.ModelRecords)-1] = record
		if err = checkpoint(ctx, in, result); err != nil {
			return result, err
		}
		messages = append(messages, response.Message)
		sources = append(sources, "message:"+message.ID)
		calls := []core.ToolCall{}
		for _, part := range response.Message.Content {
			if part.Kind == core.ContentToolCall {
				if part.ToolCall == nil || part.ToolCall.ID == "" || seen[part.ToolCall.ID] {
					return result, ErrResponse
				}
				seen[part.ToolCall.ID] = true
				calls = append(calls, *part.ToolCall)
			}
		}
		if len(calls) == 0 {
			switch response.FinishReason {
			case model.FinishStop:
				return result, nil
			case model.FinishLength:
				return result, ErrBudget
			default:
				return result, ErrResponse
			}
		}
		if response.FinishReason != model.FinishTools {
			return result, ErrResponse
		}
		if used+len(calls) > r.cfg.tools {
			return result, ErrBudget
		}
		for _, call := range calls {
			tc := in.Tools
			tc.Call.ToolCallID = call.ID
			tc.Call.ID = ""
			tr := core.ToolExecution{ID: id.NewV7(), CallID: call.ID, Tool: core.Binding{ID: call.Name}, Status: core.CallRunning}
			result.ToolRecords = append(result.ToolRecords, tr)
			if err = checkpoint(ctx, in, result); err != nil {
				return result, err
			}
			if in.Capabilities != nil {
				resolved, e := in.Capabilities(ctx, tc.Call)
				if e != nil {
					ended := time.Now().UnixMilli()
					tr.Status, tr.Error, tr.EndedAt = core.CallFailed, failure(e), &ended
					result.ToolRecords[len(result.ToolRecords)-1] = tr
					return result, e
				}
				resolved.Call = tc.Call
				tc = resolved
			}
			execution, e := r.tools.Execute(ctx, tc, call)
			tr.Tool, tr.Status, tr.StartedAt, tr.EndedAt = execution.Tool, execution.Status, execution.StartedAt, &execution.EndedAt
			tr.Error = execution.Output.Error
			if e != nil {
				tr.Error = failure(e)
			}
			result.ToolCalls = append(result.ToolCalls, execution)
			used++
			message := core.Message{ID: id.NewV7(), CreatedAt: execution.EndedAt, Role: core.RoleTool, Content: []core.Content{{Kind: core.ContentToolResult, ToolResult: &core.ToolResult{CallID: call.ID, ExecutionID: tr.ID, Content: execution.Output.Content, Error: tr.Error}}}}
			tr.ResultMessageID = message.ID
			result.ToolRecords[len(result.ToolRecords)-1] = tr
			result.Messages = append(result.Messages, message)
			messages = append(messages, model.Message{Role: message.Role, Content: message.Content})
			sources = append(sources, "message:"+message.ID)
			if e != nil {
				return result, e
			}
			if err = checkpoint(ctx, in, result); err != nil {
				return result, err
			}
		}
	}
	return result, ErrBudget
}

func validateResponse(response model.Response, seen map[string]bool) error {
	if response.Message.Role != core.RoleAssistant {
		return ErrResponse
	}
	calls := map[string]bool{}
	for _, part := range response.Message.Content {
		if part.Kind == core.ContentToolResult || part.ToolResult != nil {
			return ErrResponse
		}
		if part.Kind == core.ContentToolCall {
			if part.ToolCall == nil || part.ToolCall.ID == "" || part.ToolCall.Name == "" || seen[part.ToolCall.ID] || calls[part.ToolCall.ID] {
				return ErrResponse
			}
			calls[part.ToolCall.ID] = true
		} else if part.ToolCall != nil {
			return ErrResponse
		}
	}
	if (len(calls) > 0) != (response.FinishReason == model.FinishTools) {
		return ErrResponse
	}
	return nil
}
