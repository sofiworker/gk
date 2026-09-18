// Package context 构建并检查每次模型请求的上下文，不修改会话历史。
// Package context builds and checks per-request model context without modifying session history.
package context

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/model"
)

var (
	ErrConfig        = errors.New("invalid context configuration")
	ErrBudgetUnknown = errors.New("context budget unknown")
	ErrOverflow      = errors.New("model context exceeds input budget")
	ErrMeasurement   = errors.New("invalid context measurement")
)

// Counter 必须计入消息、工具定义、输出 Schema、媒体和协议输入开销。
// Counter must include messages, tool definitions, output schemas, media and protocol input overhead.
type Counter interface {
	Measure(context.Context, model.Request) (core.TokenMeasurement, error)
}
type CounterFunc func(context.Context, model.Request) (core.TokenMeasurement, error)

func (f CounterFunc) Measure(ctx context.Context, r model.Request) (core.TokenMeasurement, error) {
	return f(ctx, r)
}

// Limits 使用显式上限；SharedWindow 仅适用于输入输出共享窗口的模型。
// Limits are explicit bounds; SharedWindow applies only to models sharing input and output capacity.
type Limits struct {
	ModelID       string
	InputTokens   int64
	SharedWindow  int64
	OutputReserve int64
	SafetyMargin  int64
}

// BudgetResolver 按实际逻辑模型解析预算，不将未知模型套用另一模型的窗口。
// BudgetResolver resolves budgets per logical model instead of reusing another model's window.
type BudgetResolver interface {
	Resolve(context.Context, core.ModelSelection) (Limits, error)
}
type BudgetResolverFunc func(context.Context, core.ModelSelection) (Limits, error)

func (f BudgetResolverFunc) Resolve(ctx context.Context, m core.ModelSelection) (Limits, error) {
	return f(ctx, m)
}

type Input struct {
	Request model.Request
	Policy  core.ContextPolicy
}

// Group 是半开消息区间，工具请求及其结果不能跨组切断。
// Group is a half-open message range; tool calls and their results cannot be split across groups.
type Group struct {
	Start, End int
	Required   bool
}
type Plan struct {
	Request                                 model.Request
	Groups                                  []Group
	Digest                                  string
	Measurement                             core.TokenMeasurement
	InputLimit, OutputReserve, SafetyMargin int64
	NeedsCompaction                         bool
}

// Builder 首版保留原始消息、工具和参数，仅可补充未指定的输出预算。
// Builder currently preserves messages, tools and parameters, only defaulting an omitted output budget.
type Builder interface {
	Build(context.Context, Input) (Plan, error)
}
type builder struct {
	counter Counter
	budgets BudgetResolver
}

func New(counter Counter, budgets BudgetResolver) (Builder, error) {
	if counter == nil || budgets == nil {
		return nil, ErrConfig
	}
	return &builder{counter: counter, budgets: budgets}, nil
}

// FixedBudget 只绑定一个模型；模型切换必须提供对应预算。
// FixedBudget binds one model; model switches require matching budgets.
func FixedBudget(l Limits) BudgetResolver {
	return BudgetResolverFunc(func(ctx context.Context, m core.ModelSelection) (Limits, error) {
		if err := ctx.Err(); err != nil {
			return Limits{}, err
		}
		if l.ModelID == "" || m.ID != l.ModelID {
			return Limits{}, ErrBudgetUnknown
		}
		return l, nil
	})
}

func cloneRequest(r model.Request) (model.Request, error) {
	var out model.Request
	b, err := json.Marshal(r)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

// Inspect 仅进行结构检查与分组；预算未知明确标记，不提供窗口保证。
// Inspect checks structure and grouping only; unknown budgets provide no window guarantee.
func Inspect(ctx context.Context, r model.Request) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	copy, err := cloneRequest(r)
	if err != nil {
		return Plan{}, err
	}
	if err = copy.Validate(); err != nil {
		return Plan{}, err
	}
	data, err := json.Marshal(copy)
	if err != nil {
		return Plan{}, err
	}
	digest := sha256.Sum256(data)
	plan := Plan{Request: copy, Digest: hex.EncodeToString(digest[:]), Measurement: core.TokenMeasurement{Kind: core.MeasurementUnknown}}
	lastUser := -1
	for i, m := range copy.Messages {
		if m.Role == core.RoleUser {
			lastUser = i
		}
	}
	pending := map[string]bool{}
	start := 0
	required := false
	for i, m := range copy.Messages {
		required = required || m.Role == core.RoleSystem || i == lastUser
		for _, c := range m.Content {
			if c.ToolCall != nil {
				pending[c.ToolCall.ID] = true
			}
			if c.ToolResult != nil {
				delete(pending, c.ToolResult.CallID)
			}
		}
		if len(pending) == 0 {
			plan.Groups = append(plan.Groups, Group{Start: start, End: i + 1, Required: required})
			start = i + 1
			required = false
		}
	}
	if len(plan.Groups) > 0 {
		plan.Groups[len(plan.Groups)-1].Required = true
	}
	return plan, nil
}

func (b *builder) Build(ctx context.Context, in Input) (Plan, error) {
	plan, err := Inspect(ctx, in.Request)
	if err != nil {
		return plan, err
	}
	l, err := b.budgets.Resolve(ctx, in.Request.Model)
	if err != nil {
		return plan, err
	}
	if l.ModelID != in.Request.Model.ID {
		return plan, ErrBudgetUnknown
	}
	if l.InputTokens < 0 || l.SharedWindow < 0 || l.OutputReserve < 0 || l.SafetyMargin < 0 || in.Policy.MaxInputTokens < 0 || in.Policy.SoftInputTokens < 0 {
		return plan, ErrConfig
	}
	reserve := l.OutputReserve
	if n := in.Request.Parameters.MaxOutputTokens; n != nil {
		reserve = int64(*n)
	}
	if reserve <= 0 {
		return plan, ErrBudgetUnknown
	}
	limit := l.InputTokens
	if limit > 0 {
		if l.SafetyMargin >= limit {
			return plan, ErrOverflow
		}
		limit -= l.SafetyMargin
	}
	if l.SharedWindow > 0 {
		if reserve >= l.SharedWindow || l.SafetyMargin >= l.SharedWindow-reserve {
			return plan, ErrOverflow
		}
		shared := l.SharedWindow - reserve - l.SafetyMargin
		if limit == 0 || shared < limit {
			limit = shared
		}
	}
	if in.Policy.MaxInputTokens > 0 && (limit == 0 || in.Policy.MaxInputTokens < limit) {
		limit = in.Policy.MaxInputTokens
	}
	if limit <= 0 {
		return plan, ErrBudgetUnknown
	}
	if in.Policy.SoftInputTokens > limit {
		return plan, ErrConfig
	}
	if plan.Request.Parameters.MaxOutputTokens == nil {
		n := int(reserve)
		if int64(n) != reserve {
			return plan, ErrConfig
		}
		plan.Request.Parameters.MaxOutputTokens = &n
		plan, err = Inspect(ctx, plan.Request)
		if err != nil {
			return plan, err
		}
	}
	plan.InputLimit, plan.OutputReserve, plan.SafetyMargin = limit, reserve, l.SafetyMargin
	copy, err := cloneRequest(plan.Request)
	if err != nil {
		return plan, err
	}
	measurement, err := b.counter.Measure(ctx, copy)
	if err != nil {
		return plan, err
	}
	if err = ctx.Err(); err != nil {
		return plan, err
	}
	if measurement.Kind == core.MeasurementUnknown || measurement.Kind == "" {
		return plan, ErrBudgetUnknown
	}
	if measurement.Tokens < 0 || measurement.Counter.ID == "" || measurement.Counter.Version == "" || (measurement.Kind != core.MeasurementExact && measurement.Kind != core.MeasurementEstimated) {
		return plan, ErrMeasurement
	}
	plan.Measurement = measurement
	plan.NeedsCompaction = in.Policy.SoftInputTokens > 0 && measurement.Tokens >= in.Policy.SoftInputTokens
	if measurement.Tokens > limit {
		return plan, ErrOverflow
	}
	return plan, nil
}
