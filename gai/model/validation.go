package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/sofiworker/gk/gai/core"
)

var (
	ErrRequest   = errors.New("invalid model request")
	ErrResponse  = errors.New("invalid model response")
	ErrOutput    = errors.New("invalid structured model output")
	ErrValidator = errors.New("structured output schema validator required")
)

// ClientFunc 将应用函数适配为模型客户端；不会自动赋予函数厂商协议能力。
// ClientFunc adapts application functions without supplying a vendor protocol.
type ClientFunc func(context.Context, Request) (Response, error)

func (f ClientFunc) Generate(ctx context.Context, r Request) (Response, error) { return f(ctx, r) }

// SchemaValidator 由应用提供实际 Schema 引擎；仅声明 Schema 不代表已校验输出。
// SchemaValidator supplies a real schema engine; declaring a schema alone does not validate output.
type SchemaValidator func(context.Context, json.RawMessage, json.RawMessage) error

func validContent(c core.Content, role core.Role, depth int) bool {
	if depth > 16 {
		return false
	}
	switch c.Kind {
	case core.ContentText:
		return c.Artifact == nil && c.ToolCall == nil && c.ToolResult == nil
	case core.ContentImage, core.ContentAudio, core.ContentVideo, core.ContentFile:
		return c.Artifact != nil && c.Artifact.ID != "" && c.Text == "" && c.ToolCall == nil && c.ToolResult == nil
	case core.ContentToolCall:
		return role == core.RoleAssistant && c.ToolCall != nil && c.ToolCall.ID != "" && c.ToolCall.Name != "" && object(c.ToolCall.Arguments) && c.Artifact == nil && c.ToolResult == nil && c.Text == ""
	case core.ContentToolResult:
		if role != core.RoleTool || c.ToolResult == nil || c.ToolResult.CallID == "" || c.Artifact != nil || c.ToolCall != nil || c.Text != "" {
			return false
		}
		for _, nested := range c.ToolResult.Content {
			if !validContent(nested, core.RoleUser, depth+1) {
				return false
			}
		}
		return true
	}
	return false
}
func object(raw json.RawMessage) bool {
	b := bytes.TrimSpace(raw)
	return len(b) > 0 && b[0] == '{' && json.Valid(b)
}

// Validate 检查通用结构、工具选择与历史调用配对，不估算 token 或猜测厂商能力。
// Validate checks common structure, tool selection and history pairing without estimating tokens or vendor capabilities.
func (r Request) Validate() error {
	if r.Model.ID == "" || len(r.Messages) == 0 {
		return ErrRequest
	}
	if err := r.Parameters.Validate(); err != nil {
		return err
	}
	names := map[string]bool{}
	for _, t := range r.Tools {
		if t.Name == "" || names[t.Name] || !object(t.InputSchema) {
			return ErrRequest
		}
		names[t.Name] = true
	}
	if c := r.Parameters.ToolChoice; c != nil {
		if c.Mode == ToolNamed && !names[c.Name] || c.Mode == ToolRequired && len(names) == 0 {
			return ErrRequest
		}
	}
	pending := map[string]bool{}
	for _, m := range r.Messages {
		switch m.Role {
		case core.RoleUser, core.RoleSystem, core.RoleAssistant, core.RoleTool:
		default:
			return ErrRequest
		}
		if len(m.Content) == 0 {
			return ErrRequest
		}
		if len(pending) > 0 && m.Role != core.RoleTool {
			return ErrRequest
		}
		for _, c := range m.Content {
			if !validContent(c, m.Role, 0) {
				return ErrRequest
			}
			if m.Role == core.RoleTool && c.Kind != core.ContentToolResult {
				return ErrRequest
			}
			if c.ToolCall != nil {
				if pending[c.ToolCall.ID] {
					return ErrRequest
				}
				pending[c.ToolCall.ID] = true
			}
			if c.ToolResult != nil {
				if !pending[c.ToolResult.CallID] {
					return ErrRequest
				}
				delete(pending, c.ToolResult.CallID)
			}
		}
	}
	if len(pending) > 0 {
		return ErrRequest
	}
	return nil
}

// Validate 检查模型响应与本次请求的一致性；Schema 输出缺少验证器时明确拒绝。
// Validate checks response consistency with the request and rejects schema output without a validator.
func (r Response) Validate(ctx context.Context, request Request, validator SchemaValidator) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Message.Role != core.RoleAssistant {
		return ErrResponse
	}
	switch r.FinishReason {
	case FinishStop, FinishTools, FinishLength, FinishFiltered, FinishUnknown:
	default:
		return ErrResponse
	}
	names := map[string]bool{}
	for _, t := range request.Tools {
		names[t.Name] = true
	}
	seen := map[string]bool{}
	calls := 0
	var text strings.Builder
	for _, c := range r.Message.Content {
		if !validContent(c, core.RoleAssistant, 0) {
			return ErrResponse
		}
		if c.ToolCall != nil {
			call := c.ToolCall
			if !names[call.Name] || seen[call.ID] {
				return ErrResponse
			}
			seen[call.ID] = true
			calls++
			if choice := request.Parameters.ToolChoice; choice != nil && (choice.Mode == ToolNone || choice.Mode == ToolNamed && choice.Name != call.Name) {
				return ErrResponse
			}
		}
		if c.Kind == core.ContentText {
			text.WriteString(c.Text)
		}
	}
	if (calls > 0) != (r.FinishReason == FinishTools) {
		return ErrResponse
	}
	if r.FinishReason == FinishStop && len(r.Message.Content) == 0 {
		return ErrResponse
	}
	if choice := request.Parameters.ToolChoice; choice != nil && (choice.Mode == ToolRequired || choice.Mode == ToolNamed) && r.FinishReason == FinishStop {
		return ErrResponse
	}
	for _, metric := range r.Usage.Metrics {
		if metric.Tokens < 0 {
			return ErrResponse
		}
	}
	format := request.Parameters.OutputFormat
	if format == nil || format.Kind == FormatText || r.FinishReason != FinishStop {
		return nil
	}
	for _, c := range r.Message.Content {
		if c.Kind != core.ContentText {
			return ErrOutput
		}
	}
	raw := json.RawMessage(text.String())
	if !json.Valid(raw) {
		return ErrOutput
	}
	if format.Kind == FormatSchema {
		if validator == nil {
			return ErrValidator
		}
		if err := validator(ctx, format.Schema, raw); err != nil {
			return errors.Join(ErrOutput, err)
		}
	}
	return nil
}
