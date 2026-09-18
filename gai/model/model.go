// Package model 定义厂商无关的模型信息、生成参数与调用契约。
// Package model defines vendor-neutral model information, generation parameters and invocation contracts.
package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"

	"github.com/sofiworker/gk/gai/core"
)

var ErrParameters = errors.New("invalid model parameters")

// Info 描述逻辑模型的能力，不包含厂商名称、部署地址、认证或协议配置。
// Info describes logical model capabilities without vendor names, endpoints, authentication or protocol settings.
type Info struct {
	ID               string
	Version          string
	Name             string
	ContextTokens    *int
	MaxOutputTokens  *int
	InputModalities  []Modality
	OutputModalities []Modality
	Capabilities     Capabilities
}
type Modality string

const (
	Text  Modality = "text"
	Image Modality = "image"
	Audio Modality = "audio"
	Video Modality = "video"
)

// Support 的零值表示未知，不将缺失的能力信息当作支持或明确拒绝。
// Support's zero value means unknown rather than supported or explicitly rejected.
type Support string

const (
	Supported   Support = "supported"
	Unsupported Support = "unsupported"
)

type Capabilities struct {
	Tools            Support
	Streaming        Support
	StructuredOutput Support
	Reasoning        Support
}

// Parameters 的指针区分未指定与显式零值；厂商专属参数由独立适配器配置。
// Parameter pointers distinguish omission from explicit zero; vendor-specific options belong to separate adapters.
type Parameters struct {
	Temperature     *float64
	TopP            *float64
	MaxOutputTokens *int
	Stop            *[]string
	ToolChoice      *ToolChoice
	OutputFormat    *OutputFormat
}
type ToolChoice struct {
	Mode ToolChoiceMode
	Name string
}
type ToolChoiceMode string

const (
	ToolAuto     ToolChoiceMode = "auto"
	ToolNone     ToolChoiceMode = "none"
	ToolRequired ToolChoiceMode = "required"
	ToolNamed    ToolChoiceMode = "named"
)

type OutputFormat struct {
	Kind   FormatKind
	Name   string
	Schema json.RawMessage
}
type FormatKind string

const (
	FormatText   FormatKind = "text"
	FormatJSON   FormatKind = "json"
	FormatSchema FormatKind = "json_schema"
)

// Message 是模型输入输出内容，不携带 Session 消息身份或持久化时间。
// Message is model input/output content without Session message identity or persistence timestamps.
type Message struct {
	Role    core.Role
	Content []core.Content
}
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}
type Request struct {
	Model      core.ModelSelection
	Messages   []Message
	Tools      []Tool
	Parameters Parameters
}
type FinishReason = core.FinishReason

const (
	FinishStop     = core.FinishStop
	FinishTools    = core.FinishTools
	FinishLength   = core.FinishLength
	FinishFiltered = core.FinishFiltered
	FinishUnknown  = core.FinishUnknown
)

type Response struct {
	Message      Message
	ActualModel  core.Binding
	RequestID    string
	Usage        core.Usage
	FinishReason FinishReason
}

// Client 由运行时持有；厂商适配器负责解析逻辑模型身份及协议转换。
// Client belongs to the runtime; vendor adapters resolve logical model identity and translate protocols.
type Client interface {
	Generate(context.Context, Request) (Response, error)
}
type Catalog interface {
	Lookup(context.Context, core.ModelSelection) (Info, error)
}

// Validate 只检查通用语义，具体模型的范围和支持情况须由适配器进一步校验。
// Validate checks common semantics; adapters must additionally validate model-specific ranges and support.
func (p Parameters) Validate() error {
	if p.Temperature != nil && (math.IsNaN(*p.Temperature) || math.IsInf(*p.Temperature, 0) || *p.Temperature < 0) {
		return ErrParameters
	}
	if p.TopP != nil && (math.IsNaN(*p.TopP) || *p.TopP < 0 || *p.TopP > 1) {
		return ErrParameters
	}
	if p.MaxOutputTokens != nil && *p.MaxOutputTokens <= 0 {
		return ErrParameters
	}
	if p.Stop != nil {
		for _, v := range *p.Stop {
			if v == "" {
				return ErrParameters
			}
		}
	}
	if p.ToolChoice != nil {
		switch p.ToolChoice.Mode {
		case ToolAuto, ToolNone, ToolRequired:
			if p.ToolChoice.Name != "" {
				return ErrParameters
			}
		case ToolNamed:
			if p.ToolChoice.Name == "" {
				return ErrParameters
			}
		default:
			return ErrParameters
		}
	}
	if p.OutputFormat != nil {
		switch p.OutputFormat.Kind {
		case FormatText, FormatJSON:
			if (len(p.OutputFormat.Schema) != 0 && !bytes.Equal(bytes.TrimSpace(p.OutputFormat.Schema), []byte("null"))) || p.OutputFormat.Name != "" {
				return ErrParameters
			}
		case FormatSchema:
			if p.OutputFormat.Name == "" || !json.Valid(p.OutputFormat.Schema) {
				return ErrParameters
			}
		default:
			return ErrParameters
		}
	}
	return nil
}
