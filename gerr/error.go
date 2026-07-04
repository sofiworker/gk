package gerr

import (
	"fmt"
	"strings"
)

type Kind string

const (
	KindUnknown     Kind = ""
	KindInvalid     Kind = "invalid"
	KindNotFound    Kind = "not_found"
	KindConflict    Kind = "conflict"
	KindPermission  Kind = "permission"
	KindUnavailable Kind = "unavailable"
	KindTimeout     Kind = "timeout"
	KindCanceled    Kind = "canceled"
	KindInternal    Kind = "internal"
)

type Error struct {
	Code    string
	Kind    Kind
	Op      string
	Message string
	Err     error
	Meta    map[string]interface{}
}

type Option func(*Error)

func New(message string, opts ...Option) *Error {
	e := &Error{Message: message}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

func Wrap(err error, message string, opts ...Option) error {
	if err == nil {
		return nil
	}
	e := New(message, opts...)
	e.Err = err
	return e
}

func WithCode(code string) Option {
	return func(e *Error) {
		e.Code = code
	}
}

func WithKind(kind Kind) Option {
	return func(e *Error) {
		e.Kind = kind
	}
}

func WithOp(op string) Option {
	return func(e *Error) {
		e.Op = op
	}
}

func WithMeta(key string, value interface{}) Option {
	return func(e *Error) {
		if e.Meta == nil {
			e.Meta = make(map[string]interface{})
		}
		e.Meta[key] = value
	}
}

func WithMetadata(meta map[string]interface{}) Option {
	return func(e *Error) {
		if len(meta) == 0 {
			return
		}
		if e.Meta == nil {
			e.Meta = make(map[string]interface{}, len(meta))
		}
		for key, value := range meta {
			e.Meta[key] = value
		}
	}
}

func WithCause(err error) Option {
	return func(e *Error) {
		e.Err = err
	}
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	if e.Message != "" {
		b.WriteString(e.Message)
	} else if e.Code != "" {
		b.WriteString(e.Code)
	} else if e.Kind != "" {
		b.WriteString(string(e.Kind))
	}
	if e.Err != nil {
		if b.Len() > 0 {
			b.WriteString(": ")
		}
		b.WriteString(e.Err.Error())
	}
	if b.Len() == 0 {
		return "error"
	}
	return b.String()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Error) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	if e == target {
		return true
	}

	t, ok := target.(*Error)
	if !ok || t == nil {
		return false
	}

	matched := false
	if t.Code != "" {
		matched = true
		if e.Code != t.Code {
			return false
		}
	}
	if t.Kind != "" {
		matched = true
		if e.Kind != t.Kind {
			return false
		}
	}
	if t.Op != "" {
		matched = true
		if e.Op != t.Op {
			return false
		}
	}
	if t.Message != "" {
		matched = true
		if e.Message != t.Message {
			return false
		}
	}
	return matched
}

func (e *Error) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			_, _ = fmt.Fprint(s, e.Error())
			return
		}
		fallthrough
	case 's':
		_, _ = fmt.Fprint(s, e.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", e.Error())
	}
}
