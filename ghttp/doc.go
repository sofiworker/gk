package ghttp

import "time"

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

// DocOption configures route documentation metadata.
type DocOption func(*RouteDoc)

// Summary sets the OpenAPI operation summary.
func Summary(summary string) DocOption {
	return func(d *RouteDoc) {
		d.Summary = summary
	}
}

// Description sets the OpenAPI operation description.
func Description(description string) DocOption {
	return func(d *RouteDoc) {
		d.Description = description
	}
}

// OperationID sets the OpenAPI operation ID.
func OperationID(id string) DocOption {
	return func(d *RouteDoc) {
		d.OperationID = id
	}
}

// Tags sets OpenAPI operation tags.
func Tags(tags ...string) DocOption {
	return func(d *RouteDoc) {
		d.Tags = append(d.Tags[:0], tags...)
	}
}

// Deprecated marks the route as deprecated and records an optional migration hint.
func Deprecated(reason string) DocOption {
	return func(d *RouteDoc) {
		d.Deprecated = true
		d.DeprecatedReason = reason
	}
}

// Sunset documents when a deprecated route is expected to stop being available.
func Sunset(at time.Time) DocOption {
	return func(d *RouteDoc) {
		if at.IsZero() {
			d.Sunset = ""
			return
		}
		d.Sunset = at.UTC().Format(time.RFC3339)
	}
}

// ExternalDocsDoc describes external documentation for a route.
type ExternalDocsDoc struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// ExternalDocs links a route to external documentation.
func ExternalDocs(description, url string) DocOption {
	return func(d *RouteDoc) {
		d.ExternalDocs = &ExternalDocsDoc{
			Description: description,
			URL:         url,
		}
	}
}

// DocMessage describes a business-level response code and message.
type DocMessage struct {
	Code    *int   `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// DocMessageOption configures a DocMessage.
type DocMessageOption func(*DocMessage)

// Code sets a business-level code for documentation metadata.
func Code(code int) DocMessageOption {
	return func(m *DocMessage) {
		m.Code = &code
	}
}

// Message sets a business-level message for documentation metadata.
func Message(message string) DocMessageOption {
	return func(m *DocMessage) {
		m.Message = message
	}
}

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
