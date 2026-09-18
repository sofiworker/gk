package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/sofiworker/gk/gai/core"
)

// Function 显式使用调用方提供的 Schema 和语义校验，不声称自动执行完整 JSON Schema 校验。
// Function uses explicit caller-provided schema and semantic validation, not full automatic JSON Schema validation.
type Function[T any] struct {
	definition core.ToolDefinition
	validate   func(context.Context, T) error
	run        func(context.Context, core.ToolContext, T) (core.ToolOutput, error)
}

func NewFunction[T any](definition core.ToolDefinition, validate func(context.Context, T) error, run func(context.Context, core.ToolContext, T) (core.ToolOutput, error)) (*Function[T], error) {
	if err := validateDefinition(definition); err != nil {
		return nil, err
	}
	if validate == nil || run == nil {
		return nil, ErrDefinition
	}
	return &Function[T]{definition: copyDefinition(definition), validate: validate, run: run}, nil
}
func (f *Function[T]) Definition() core.ToolDefinition { return copyDefinition(f.definition) }
func decode[T any](raw json.RawMessage) (value T, err error) {
	if err = objectArguments(raw); err != nil {
		return value, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err = decoder.Decode(&value); err != nil {
		return value, err
	}
	return value, nil
}
func (f *Function[T]) Validate(ctx context.Context, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := decode[T](raw)
	if err != nil {
		return err
	}
	return f.validate(ctx, value)
}

// Execute 供已校验的调度器调用；直接调用也解码参数，但不重复有状态的校验回调。
// Execute is called by a validated dispatcher; direct calls decode arguments but do not repeat validation callbacks.
func (f *Function[T]) Execute(ctx context.Context, tc core.ToolContext, call core.ToolCall) (core.ToolOutput, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolOutput{}, err
	}
	value, err := decode[T](call.Arguments)
	if err != nil {
		return core.ToolOutput{}, err
	}
	return f.run(ctx, tc, value)
}

// objectArguments 拒绝重复键与多 JSON 值，避免授权器和工具对同一参数产生不同解释。
// objectArguments rejects duplicate keys and multiple JSON values to avoid divergent policy and tool interpretations.
func objectArguments(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ErrArguments
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := jsonValue(decoder, 0); err != nil {
		return ErrArguments
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrArguments
	}
	return nil
}
func jsonValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return ErrArguments
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrArguments
			}
			seen[name] = true
			if err = jsonValue(d, depth+1); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim('}') {
			return ErrArguments
		}
	case '[':
		for d.More() {
			if err = jsonValue(d, depth+1); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim(']') {
			return ErrArguments
		}
	default:
		return ErrArguments
	}
	return nil
}
