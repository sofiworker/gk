package ghttp

import "time"

// RouteDoc 保存路由的文档元数据。
// RouteDoc holds documentation metadata for a route.
type RouteDoc struct {
	Summary          string
	Description      string
	OperationID      string
	Tags             []string
	Deprecated       bool
	DeprecatedReason string
	Sunset           string
	ExternalDocs     *ExternalDocsDoc
	Success          *DocMessage
	Errors           []DocMessage
}

// DocOption 配置路由文档元数据。
// DocOption configures route documentation metadata.
type DocOption func(*RouteDoc)

// Summary 设置 OpenAPI 操作摘要。
// Summary sets the OpenAPI operation summary.
func Summary(summary string) DocOption {
	return func(d *RouteDoc) {
		d.Summary = summary
	}
}

// Description 设置 OpenAPI 操作描述。
// Description sets the OpenAPI operation description.
func Description(description string) DocOption {
	return func(d *RouteDoc) {
		d.Description = description
	}
}

// OperationID 设置 OpenAPI 操作 ID。
// OperationID sets the OpenAPI operation ID.
func OperationID(id string) DocOption {
	return func(d *RouteDoc) {
		d.OperationID = id
	}
}

// Tags 设置 OpenAPI 操作标签。
// Tags sets OpenAPI operation tags.
func Tags(tags ...string) DocOption {
	return func(d *RouteDoc) {
		d.Tags = append(d.Tags[:0], tags...)
	}
}

// Deprecated 标记路由废弃并记录可选的迁移提示。
// Deprecated marks the route deprecated with an optional migration hint.
func Deprecated(reason string) DocOption {
	return func(d *RouteDoc) {
		d.Deprecated = true
		d.DeprecatedReason = reason
	}
}

// Sunset 记录废弃路由预计停止可用的时间。
// Sunset documents when a deprecated route is expected to disappear.
func Sunset(at time.Time) DocOption {
	return func(d *RouteDoc) {
		if at.IsZero() {
			d.Sunset = ""
			return
		}
		d.Sunset = at.UTC().Format(time.RFC3339)
	}
}

// ExternalDocsDoc 描述路由的外部文档。
// ExternalDocsDoc describes external documentation for a route.
type ExternalDocsDoc struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// ExternalDocs 为路由关联外部文档。
// ExternalDocs links a route to external documentation.
func ExternalDocs(description, url string) DocOption {
	return func(d *RouteDoc) {
		d.ExternalDocs = &ExternalDocsDoc{
			Description: description,
			URL:         url,
		}
	}
}

// DocMessage 描述业务级响应 code 与 message。
// DocMessage describes a business-level response code and message.
type DocMessage struct {
	Code    *int   `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// DocMessageOption 配置 DocMessage。
// DocMessageOption configures a DocMessage.
type DocMessageOption func(*DocMessage)

// Code 设置文档元数据的业务级 code。
// Code sets a business-level code for documentation metadata.
func Code(code int) DocMessageOption {
	return func(m *DocMessage) {
		m.Code = &code
	}
}

// Message 设置文档元数据的业务级 message。
// Message sets a business-level message for documentation metadata.
func Message(message string) DocMessageOption {
	return func(m *DocMessage) {
		m.Message = message
	}
}

// Success 记录成功的业务响应。
// Success documents the successful business response.
func Success(opts ...DocMessageOption) DocOption {
	return func(d *RouteDoc) {
		msg := &DocMessage{}
		for _, opt := range opts {
			if opt != nil {
				opt(msg)
			}
		}
		d.Success = msg
	}
}

// Errors 记录路由可能返回的业务级错误。
// Errors documents business-level errors a route can return.
func Errors(errs ...error) DocOption {
	return func(d *RouteDoc) {
		for _, err := range errs {
			if err == nil {
				continue
			}
			d.Errors = append(d.Errors, docMessageFromError(err))
		}
	}
}

func docMessageFromError(err error) DocMessage {
	if he := AsError(err); he != nil {
		code := he.Code
		return DocMessage{
			Code:    &code,
			Message: he.Message,
		}
	}
	return DocMessage{Message: err.Error()}
}

func (d RouteDoc) clone() RouteDoc {
	out := d
	if d.Tags != nil {
		out.Tags = append([]string(nil), d.Tags...)
	}
	if d.Errors != nil {
		out.Errors = append([]DocMessage(nil), d.Errors...)
		for index := range out.Errors {
			if d.Errors[index].Code == nil {
				continue
			}
			code := *d.Errors[index].Code
			out.Errors[index].Code = &code
		}
	}
	if d.Success != nil {
		success := *d.Success
		if d.Success.Code != nil {
			code := *d.Success.Code
			success.Code = &code
		}
		out.Success = &success
	}
	if d.ExternalDocs != nil {
		externalDocs := *d.ExternalDocs
		out.ExternalDocs = &externalDocs
	}
	return out
}
