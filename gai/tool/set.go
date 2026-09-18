// Package tool 从 Agent 工具实例构建受控执行集合，不包含模型循环。
// Package tool builds controlled execution sets from Agent tool instances without a model loop.
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/sofiworker/gk/gai/core"
)

var (
	ErrDefinition = errors.New("invalid tool definition")
	ErrDuplicate  = errors.New("duplicate tool name")
	ErrNotFound   = errors.New("tool not found in bound set")
	ErrArguments  = errors.New("invalid tool arguments")
	ErrDenied     = errors.New("tool execution denied")
	ErrLimit      = errors.New("tool argument limit exceeded")
	ErrPanic      = errors.New("tool callback panicked")
)
var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type entry struct {
	ref        core.Binding
	definition core.ToolDefinition
	impl       core.Tool
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Interface, reflect.Slice, reflect.Chan:
		return rv.IsNil()
	}
	return false
}
func definitionOf(t core.Tool) (def core.ToolDefinition, err error) {
	defer func() {
		if recover() != nil {
			err = ErrPanic
		}
	}()
	return t.Definition(), nil
}
func validateDefinition(d core.ToolDefinition) error {
	if !validName.MatchString(d.Name) || d.Version == "" || len(d.Version) > 128 || len(d.Description) > 65536 || len(d.InputSchema) > 1<<20 {
		return ErrDefinition
	}
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(d.InputSchema, &schema); err != nil || schema == nil {
		return ErrDefinition
	}
	var kind string
	if json.Unmarshal(schema["type"], &kind) != nil || kind != "object" {
		return ErrDefinition
	}
	return nil
}
func copyDefinition(d core.ToolDefinition) core.ToolDefinition {
	d.InputSchema = append(json.RawMessage(nil), d.InputSchema...)
	return d
}

// Authorization 不包含能力接口；策略检查固定参数副本，不能改变实际执行参数。
// Authorization contains no capability interfaces; policy checks a copy and cannot rewrite execution arguments.
type Authorization struct {
	Tool        core.Binding
	Definition  core.ToolDefinition
	Call        core.ToolCall
	Attribution core.Call
}
type Authorizer func(context.Context, Authorization) error
type config struct {
	authorize     Authorizer
	argumentBytes int
	timeout       time.Duration
}
type Option func(*config) error

func WithAuthorizer(fn Authorizer) Option {
	return func(c *config) error {
		if fn == nil {
			return ErrDefinition
		}
		c.authorize = fn
		return nil
	}
}
func WithLimits(argumentBytes int, timeout time.Duration) Option {
	return func(c *config) error {
		if argumentBytes < 1 || timeout <= 0 {
			return ErrLimit
		}
		c.argumentBytes = argumentBytes
		c.timeout = timeout
		return nil
	}
}

// Set 固定工具描述与版本；修改输入切片不会改变这个工具集。
// Set freezes descriptions and versions; changes to the input slice cannot change this set.
type Set struct {
	entries map[string]entry
	order   []string
	cfg     config
}

func New(tools []core.Tool, options ...Option) (*Set, error) {
	cfg := config{argumentBytes: 1 << 20, timeout: 30 * time.Second}
	for _, o := range options {
		if o == nil {
			return nil, ErrDefinition
		}
		if err := o(&cfg); err != nil {
			return nil, err
		}
	}
	set := &Set{entries: make(map[string]entry), cfg: cfg}
	for _, impl := range tools {
		if nilValue(impl) {
			return nil, ErrDefinition
		}
		def, err := definitionOf(impl)
		if err != nil {
			return nil, err
		}
		if err = validateDefinition(def); err != nil {
			return nil, err
		}
		if _, ok := set.entries[def.Name]; ok {
			return nil, fmt.Errorf("tool %s: %w", def.Name, ErrDuplicate)
		}
		set.entries[def.Name] = entry{ref: core.Binding{ID: def.Name, Version: def.Version}, definition: copyDefinition(def), impl: impl}
		set.order = append(set.order, def.Name)
	}
	return set, nil
}
func (s *Set) Definitions() []core.ToolDefinition {
	out := make([]core.ToolDefinition, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, copyDefinition(s.entries[name].definition))
	}
	return out
}

// Execution.Started 表示已进入回调，错误或取消不能解释为没有副作用。
// Execution.Started means the callback was entered; errors or cancellation do not prove absence of side effects.
type Execution struct {
	Tool      core.Binding
	CallID    string
	Started   bool
	Status    core.CallStatus
	StartedAt *int64
	EndedAt   int64
	Output    core.ToolOutput
}

func (s *Set) Execute(ctx context.Context, tc core.ToolContext, call core.ToolCall) (execution Execution, err error) {
	execution.CallID = call.ID
	execution.Status = core.CallFailed
	defer func() {
		execution.EndedAt = time.Now().UnixMilli()
		if recover() != nil {
			err = ErrPanic
			if execution.Started {
				execution.Status = core.CallUnknown
			}
		}
	}()
	if err = ctx.Err(); err != nil {
		return execution, err
	}
	entry, ok := s.entries[call.Name]
	if !ok {
		return execution, ErrNotFound
	}
	execution.Tool = entry.ref
	if call.ID == "" || len(call.ID) > 256 {
		return execution, ErrArguments
	}
	if len(call.Arguments) > s.cfg.argumentBytes {
		return execution, ErrLimit
	}
	call = copyCall(call)
	if err = objectArguments(call.Arguments); err != nil {
		return execution, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.timeout)
	defer cancel()
	if err = entry.impl.Validate(ctx, append(json.RawMessage(nil), call.Arguments...)); err != nil {
		return execution, fmt.Errorf("validate %s: %w: %w", call.Name, ErrArguments, err)
	}
	if err = ctx.Err(); err != nil {
		return execution, err
	}
	if s.cfg.authorize == nil {
		return execution, ErrDenied
	}
	if tc.Call.ToolCallID != "" && tc.Call.ToolCallID != call.ID {
		return execution, ErrArguments
	}
	tc.Call.ToolCallID = call.ID
	authorization := Authorization{Tool: entry.ref, Definition: copyDefinition(entry.definition), Call: copyCall(call), Attribution: tc.Call}
	if err = s.cfg.authorize(ctx, authorization); err != nil {
		return execution, fmt.Errorf("authorize %s: %w: %w", call.Name, ErrDenied, err)
	}
	if err = ctx.Err(); err != nil {
		return execution, err
	}
	now := time.Now().UnixMilli()
	execution.StartedAt = &now
	execution.Started = true
	execution.Output, err = entry.impl.Execute(ctx, tc, call)
	if err != nil {
		execution.Status = core.CallUnknown
		return execution, err
	}
	if ctx.Err() != nil {
		execution.Status = core.CallUnknown
		return execution, ctx.Err()
	}
	execution.Status = core.CallCompleted
	if execution.Output.Error != nil {
		execution.Status = core.CallFailed
	}
	return execution, nil
}
func copyCall(c core.ToolCall) core.ToolCall {
	c.Arguments = append(json.RawMessage(nil), c.Arguments...)
	return c
}
